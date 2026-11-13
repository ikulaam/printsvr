package handlers

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"printsvr/internal/config"
	"printsvr/internal/database"
	"printsvr/internal/models"
	"printsvr/internal/printer"
	"printsvr/internal/queue"
)

type PrinterWithQueueCount struct {
	models.Printer
	PendingJobsCount int64 `json:"pending_jobs_count"`
}

// GetPrinters 获取所有打印机（包含当前排队任务数统计）
func GetPrinters(c *gin.Context) {
	var printers []models.Printer
	if err := database.DB.Order("is_default DESC, id ASC").Find(&printers).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取打印机列表失败"})
		return
	}

	result := make([]PrinterWithQueueCount, len(printers))
	for i, p := range printers {
		var count int64
		database.DB.Model(&models.PrintJob{}).
			Where("printer_id = ? AND status IN (?)", p.ID, []models.JobStatus{models.JobStatusPending, models.JobStatusProcessing}).
			Count(&count)

		result[i] = PrinterWithQueueCount{
			Printer:          p,
			PendingJobsCount: count,
		}
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}

// DiscoverPrinters 扫描操作系统已安装的打印机
func DiscoverPrinters(c *gin.Context) {
	sysPrinters, err := printer.DiscoverSystemPrinters()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "系统打印机扫描失败: " + err.Error()})
		return
	}

	// 查一下哪些已经在数据库中了
	var existing []models.Printer
	database.DB.Find(&existing)
	existingMap := make(map[string]bool)
	for _, p := range existing {
		existingMap[strings.ToLower(p.SystemName)] = true
		existingMap[strings.ToLower(p.Name)] = true
	}

	type DiscoveredResult struct {
		printer.SystemPrinterInfo
		AlreadyImported bool `json:"already_imported"`
	}

	results := make([]DiscoveredResult, len(sysPrinters))
	for i, sp := range sysPrinters {
		results[i] = DiscoveredResult{
			SystemPrinterInfo: sp,
			AlreadyImported:   existingMap[strings.ToLower(sp.Name)],
		}
	}

	c.JSON(http.StatusOK, gin.H{"data": results})
}

type ImportPrinterRequest struct {
	Name            string `json:"name" binding:"required"`
	SystemName      string `json:"system_name" binding:"required"`
	Location        string `json:"location"`
	ColorSupported  bool   `json:"color_supported"`
	DuplexSupported bool   `json:"duplex_supported"`
	IsSimulation    bool   `json:"is_simulation"`
	Description     string `json:"description"`
}

// ImportPrinter 导入已探测到的打印机
func ImportPrinter(c *gin.Context) {
	var req ImportPrinterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数不完整"})
		return
	}

	newPrinter := models.Printer{
		Name:            req.Name,
		SystemName:      req.SystemName,
		Location:        req.Location,
		Status:          models.PrinterStatusIdle,
		ColorSupported:  req.ColorSupported,
		DuplexSupported: req.DuplexSupported,
		PaperSizes:      "A4,A3,B5",
		IsSimulation:    req.IsSimulation,
		Description:     req.Description,
	}

	if err := database.DB.Create(&newPrinter).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "导入打印机失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "打印机添加成功", "data": newPrinter})
}

// UpdatePrinter 更新打印机信息
func UpdatePrinter(c *gin.Context) {
	idStr := c.Param("id")
	id, _ := strconv.ParseUint(idStr, 10, 32)

	var p models.Printer
	if err := database.DB.First(&p, uint(id)).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "找不到该打印机"})
		return
	}

	var req models.Printer
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "输入数据无效"})
		return
	}

	p.Name = req.Name
	p.SystemName = req.SystemName
	p.Location = req.Location
	p.ColorSupported = req.ColorSupported
	p.DuplexSupported = req.DuplexSupported
	p.PaperSizes = req.PaperSizes
	p.IsSimulation = req.IsSimulation
	p.Description = req.Description
	p.Status = req.Status
	p.IsDefault = req.IsDefault

	// 如果设置为了默认，取消其他默认
	if p.IsDefault {
		database.DB.Model(&models.Printer{}).Where("id <> ?", p.ID).Update("is_default", false)
	}

	database.DB.Save(&p)
	c.JSON(http.StatusOK, gin.H{"message": "更新成功", "data": p})
}

// DeletePrinter 删除打印机
func DeletePrinter(c *gin.Context) {
	idStr := c.Param("id")
	id, _ := strconv.ParseUint(idStr, 10, 32)

	var p models.Printer
	if err := database.DB.First(&p, uint(id)).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "找不到该打印机"})
		return
	}

	// 检查是否有排队中的任务
	var count int64
	database.DB.Model(&models.PrintJob{}).
		Where("printer_id = ? AND status IN (?)", p.ID, []models.JobStatus{models.JobStatusPending, models.JobStatusProcessing}).
		Count(&count)

	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("该打印机当前仍有 %d 个任务正在排队或打印中，无法删除", count)})
		return
	}

	database.DB.Delete(&p)
	c.JSON(http.StatusOK, gin.H{"message": "打印机已成功移除"})
}

// TestPrintPage 发送测试打印页
func TestPrintPage(c *gin.Context) {
	idStr := c.Param("id")
	id, _ := strconv.ParseUint(idStr, 10, 32)
	currentUser := GetCurrentUser(c)

	var p models.Printer
	if err := database.DB.First(&p, uint(id)).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "打印机不存在"})
		return
	}

	// 生成一个测试打印文本文件
	testContent := fmt.Sprintf(`====================================================
           PrintSvr 局域网共享打印系统 - 测试页
====================================================
打印机名称: %s
系统识别名: %s
设备物理位置: %s
是否仿真打印: %t
支持彩色打印: %t
支持双面打印: %t
测试发起人员: %s (%s)
测试提交时间: %s
====================================================
如果您能看到本测试页，说明打印服务网络与调度队列工作完全正常！
祝您工作愉快！
====================================================`,
		p.Name, p.SystemName, p.Location, p.IsSimulation, p.ColorSupported, p.DuplexSupported,
		currentUser.DisplayName, currentUser.Username, time.Now().Format("2006-01-02 15:04:05"))

	testFileName := fmt.Sprintf("testpage_%d_%d.txt", p.ID, time.Now().Unix())
	testFilePath := filepath.Join(config.AppConfig.UploadDir, testFileName)
	if err := os.WriteFile(testFilePath, []byte(testContent), 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成测试页失败"})
		return
	}

	job := models.PrintJob{
		JobNumber:    fmt.Sprintf("TEST-%d-%s", p.ID, time.Now().Format("150405")),
		UserID:       currentUser.ID,
		PrinterID:    p.ID,
		FileName:     "设备自检测试页.txt",
		StoredName:   testFileName,
		FilePath:     testFilePath,
		FileSize:     int64(len(testContent)),
		FileType:     "txt",
		Copies:       1,
		PageCount:    1,
		IsColor:      false,
		Duplex:       "simplex",
		PaperSize:    "A4",
		Orientation:  "portrait",
		Priority:     50, // 稍微高一点
		Status:       models.JobStatusPending,
		Progress:     0,
		Remark:       "管理员设备连通性自检测试",
		CreatedAt:    time.Now(),
	}

	database.DB.Create(&job)
	queue.Manager.NotifyNewJob()

	c.JSON(http.StatusOK, gin.H{"message": "测试打印作业已成功提交到队列！", "job_id": job.ID})
}
