package handlers

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"printsvr/internal/config"
	"printsvr/internal/database"
	"printsvr/internal/models"
	"printsvr/internal/printer"
	"printsvr/internal/queue"
)

// CreatePrintJob 提交打印作业
func CreatePrintJob(c *gin.Context) {
	currentUser := GetCurrentUser(c)

	// 解析表单参数
	printerIDStr := c.PostForm("printer_id")
	copiesStr := c.DefaultPostForm("copies", "1")
	isColorStr := c.DefaultPostForm("is_color", "false")
	duplex := c.DefaultPostForm("duplex", "simplex")
	paperSize := c.DefaultPostForm("paper_size", "A4")
	orientation := c.DefaultPostForm("orientation", "portrait")
	remark := c.PostForm("remark")

	printerIDUint, err := strconv.ParseUint(printerIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择有效的打印机"})
		return
	}

	copies, _ := strconv.Atoi(copiesStr)
	if copies < 1 {
		copies = 1
	}
	if copies > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "单次打印份数不可超过 100 份"})
		return
	}

	isColor := isColorStr == "true" || isColorStr == "1"

	// 验证打印机是否存在且状态正常
	var targetPrinter models.Printer
	if err := database.DB.First(&targetPrinter, uint(printerIDUint)).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "指定的打印机不存在"})
		return
	}

	// 接收上传的文件
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择待打印的文件"})
		return
	}
	defer file.Close()

	if header.Size > config.AppConfig.MaxUploadMB*1024*1024 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("文件大小不能超过 %dMB", config.AppConfig.MaxUploadMB),
		})
		return
	}

	origFileName := filepath.Base(header.Filename)
	ext := strings.TrimPrefix(filepath.Ext(origFileName), ".")
	extLower := strings.ToLower(ext)

	// 生成安全存储文件名
	uniqueID := uuid.New().String()
	storedFileName := fmt.Sprintf("%s_%s.%s", time.Now().Format("20060102150405"), uniqueID[:8], extLower)
	storedFilePath := filepath.Join(config.AppConfig.UploadDir, storedFileName)

	destFile, err := os.Create(storedFilePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存上传文件失败: " + err.Error()})
		return
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, file); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "写入文件存储失败"})
		return
	}

	// 估算页数
	estimatedPages := printer.EstimatePageCount(storedFilePath, extLower, header.Size)

	// 检查配额
	totalDemandPages := estimatedPages * copies
	if currentUser.Quota != -1 && (currentUser.UsedQuota+totalDemandPages > currentUser.Quota) {
		_ = os.Remove(storedFilePath)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("打印配额不足！当前可用剩余: %d 页，本次预计消耗: %d 页",
				currentUser.Quota-currentUser.UsedQuota, totalDemandPages),
		})
		return
	}

	// 生成任务流水号
	jobNumber := fmt.Sprintf("PJ-%s-%s", time.Now().Format("20060102"), strings.ToUpper(uniqueID[:6]))

	job := models.PrintJob{
		JobNumber:    jobNumber,
		UserID:       currentUser.ID,
		PrinterID:    targetPrinter.ID,
		FileName:     origFileName,
		StoredName:   storedFileName,
		FilePath:     storedFilePath,
		FileSize:     header.Size,
		FileType:     extLower,
		Copies:       copies,
		PageCount:    estimatedPages,
		IsColor:      isColor,
		Duplex:       duplex,
		PaperSize:    paperSize,
		Orientation:  orientation,
		Priority:     0,
		Status:       models.JobStatusPending,
		Progress:     0,
		ErrorMessage: "",
		Remark:       remark,
		CreatedAt:    time.Now(),
	}

	if err := database.DB.Create(&job).Error; err != nil {
		_ = os.Remove(storedFilePath)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建打印任务失败: " + err.Error()})
		return
	}

	// 唤醒打印队列工作调度器
	queue.Manager.NotifyNewJob()

	c.JSON(http.StatusOK, gin.H{
		"message":    "打印作业已成功进入排队队列！",
		"job_number": jobNumber,
		"job_id":     job.ID,
		"pages":      estimatedPages,
		"copies":     copies,
	})
}

// GetUserJobs 获取当前用户的打印作业列表
func GetUserJobs(c *gin.Context) {
	currentUser := GetCurrentUser(c)
	statusFilter := c.Query("status")

	query := database.DB.Model(&models.PrintJob{}).
		Preload("Printer").
		Where("user_id = ?", currentUser.ID)

	if statusFilter != "" && statusFilter != "all" {
		query = query.Where("status = ?", statusFilter)
	}

	var jobs []models.PrintJob
	if err := query.Order("created_at DESC").Limit(100).Find(&jobs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取队列列表失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": jobs})
}

// GetJobDetail 获取单个打印任务详情
func GetJobDetail(c *gin.Context) {
	currentUser := GetCurrentUser(c)
	idStr := c.Param("id")
	id, _ := strconv.ParseUint(idStr, 10, 32)

	var job models.PrintJob
	if err := database.DB.Preload("Printer").Preload("User").First(&job, uint(id)).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "找不到该打印任务"})
		return
	}

	// 权限检查
	if currentUser.Role != models.RoleAdmin && job.UserID != currentUser.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权查看该任务"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": job})
}

// CancelUserJob 取消打印任务
func CancelUserJob(c *gin.Context) {
	currentUser := GetCurrentUser(c)
	idStr := c.Param("id")
	id, _ := strconv.ParseUint(idStr, 10, 32)

	isAdmin := currentUser.Role == models.RoleAdmin
	if err := queue.Manager.CancelJob(uint(id), currentUser.ID, isAdmin); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "打印任务已成功取消"})
}

// RetryUserJob 重新排队打印
func RetryUserJob(c *gin.Context) {
	currentUser := GetCurrentUser(c)
	idStr := c.Param("id")
	id, _ := strconv.ParseUint(idStr, 10, 32)

	isAdmin := currentUser.Role == models.RoleAdmin
	if err := queue.Manager.RetryJob(uint(id), currentUser.ID, isAdmin); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "任务已重新进入排队队列"})
}

// DownloadJobFile 下载或在线预览原待打印文件
func DownloadJobFile(c *gin.Context) {
	currentUser := GetCurrentUser(c)
	idStr := c.Param("id")
	id, _ := strconv.ParseUint(idStr, 10, 32)

	var job models.PrintJob
	if err := database.DB.First(&job, uint(id)).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "打印任务不存在"})
		return
	}

	if currentUser.Role != models.RoleAdmin && job.UserID != currentUser.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权访问此文件"})
		return
	}

	if _, err := os.Stat(job.FilePath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "服务器物理存储文件已不存在"})
		return
	}

	// 针对 PDF 或图片支持 inline 在线预览，其它附件下载
	c.Header("Content-Disposition", fmt.Sprintf("inline; filename=\"%s\"", job.FileName))
	c.File(job.FilePath)
}
