package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"printsvr/internal/auth"
	"printsvr/internal/database"
	"printsvr/internal/models"
	"printsvr/internal/queue"
)

// AdminDashboardStats 仪表盘统计数据
type AdminDashboardStats struct {
	TotalJobsToday    int64            `json:"total_jobs_today"`
	PendingJobs       int64            `json:"pending_jobs"`
	ProcessingJobs    int64            `json:"processing_jobs"`
	CompletedJobs     int64            `json:"completed_jobs"`
	FailedJobs        int64            `json:"failed_jobs"`
	TotalUsers        int64            `json:"total_users"`
	ActivePrinters    int64            `json:"active_printers"`
	TotalPagesPrinted int64            `json:"total_pages_printed"`
	RecentTrend       []DailyTrendItem `json:"recent_trend"`
	PrinterLoads      []PrinterLoad    `json:"printer_loads"`
}

type DailyTrendItem struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

type PrinterLoad struct {
	PrinterName string `json:"printer_name"`
	JobCount    int64  `json:"job_count"`
}

// GetAdminDashboard 获取管理员仪表盘数据
func GetAdminDashboard(c *gin.Context) {
	var stats AdminDashboardStats

	todayStart := time.Now().Truncate(24 * time.Hour)

	database.DB.Model(&models.PrintJob{}).Where("created_at >= ?", todayStart).Count(&stats.TotalJobsToday)
	database.DB.Model(&models.PrintJob{}).Where("status = ?", models.JobStatusPending).Count(&stats.PendingJobs)
	database.DB.Model(&models.PrintJob{}).Where("status = ?", models.JobStatusProcessing).Count(&stats.ProcessingJobs)
	database.DB.Model(&models.PrintJob{}).Where("status = ?", models.JobStatusCompleted).Count(&stats.CompletedJobs)
	database.DB.Model(&models.PrintJob{}).Where("status = ?", models.JobStatusFailed).Count(&stats.FailedJobs)
	database.DB.Model(&models.User{}).Count(&stats.TotalUsers)
	database.DB.Model(&models.Printer{}).Where("status IN (?)", []models.PrinterStatus{models.PrinterStatusIdle, models.PrinterStatusBusy}).Count(&stats.ActivePrinters)

	// 计算已消耗总页数
	var totalPages struct {
		Total int64
	}
	database.DB.Model(&models.User{}).Select("SUM(used_quota) as total").Scan(&totalPages)
	stats.TotalPagesPrinted = totalPages.Total

	// 统计近 7 天趋势
	stats.RecentTrend = make([]DailyTrendItem, 7)
	for i := 6; i >= 0; i-- {
		day := time.Now().AddDate(0, 0, -i)
		start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
		end := start.Add(24 * time.Hour)

		var dayCount int64
		database.DB.Model(&models.PrintJob{}).Where("created_at >= ? AND created_at < ?", start, end).Count(&dayCount)

		stats.RecentTrend[6-i] = DailyTrendItem{
			Date:  day.Format("01-02"),
			Count: dayCount,
		}
	}

	// 统计各打印机任务负载
	var printers []models.Printer
	database.DB.Find(&printers)
	for _, p := range printers {
		var jCount int64
		database.DB.Model(&models.PrintJob{}).Where("printer_id = ?", p.ID).Count(&jCount)
		stats.PrinterLoads = append(stats.PrinterLoads, PrinterLoad{
			PrinterName: p.Name,
			JobCount:    jCount,
		})
	}

	c.JSON(http.StatusOK, gin.H{"data": stats})
}

// GetAdminJobs 查看全局所有作业列表
func GetAdminJobs(c *gin.Context) {
	statusFilter := c.Query("status")
	searchKeyword := strings.TrimSpace(c.Query("search"))

	query := database.DB.Model(&models.PrintJob{}).
		Preload("Printer").
		Preload("User")

	if statusFilter != "" && statusFilter != "all" {
		query = query.Where("status = ?", statusFilter)
	}

	if searchKeyword != "" {
		pattern := "%" + searchKeyword + "%"
		query = query.Where("file_name LIKE ? OR job_number LIKE ?", pattern, pattern)
	}

	var jobs []models.PrintJob
	if err := query.Order("priority DESC, created_at DESC").Limit(150).Find(&jobs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取队列任务失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": jobs})
}

// PrioritizeJob 管理员加急置顶
func PrioritizeJob(c *gin.Context) {
	idStr := c.Param("id")
	id, _ := strconv.ParseUint(idStr, 10, 32)

	if err := queue.Manager.PrioritizeJob(uint(id)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "任务已加急置顶并调整至队列前列！"})
}

// AdminCancelJob 管理员强制取消任务
func AdminCancelJob(c *gin.Context) {
	idStr := c.Param("id")
	id, _ := strconv.ParseUint(idStr, 10, 32)
	currentUser := GetCurrentUser(c)

	if err := queue.Manager.CancelJob(uint(id), currentUser.ID, true); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "该作业已被管理员终止"})
}

// ClearHistoryJobs 清理历史归档
func ClearHistoryJobs(c *gin.Context) {
	// 删除已完成、失败、或已取消的历史任务记录
	res := database.DB.Where("status IN (?)", []models.JobStatus{
		models.JobStatusCompleted,
		models.JobStatusFailed,
		models.JobStatusCancelled,
	}).Delete(&models.PrintJob{})

	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("已成功清理 %d 条历史打印记录", res.RowsAffected),
	})
}

// GetAdminUsers 用户列表
func GetAdminUsers(c *gin.Context) {
	var users []models.User
	database.DB.Order("id ASC").Find(&users)
	c.JSON(http.StatusOK, gin.H{"data": users})
}

type CreateUserRequest struct {
	Username    string          `json:"username" binding:"required,min=3"`
	Password    string          `json:"password" binding:"required,min=6"`
	DisplayName string          `json:"display_name" binding:"required"`
	Role        models.UserRole `json:"role"`
	Quota       int             `json:"quota"`
	Department  string          `json:"department"`
}

// CreateAdminUser 管理员添加用户
func CreateAdminUser(c *gin.Context) {
	var req CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数不合法: " + err.Error()})
		return
	}

	var existing models.User
	if err := database.DB.Where("username = ?", req.Username).First(&existing).Error; err == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该用户名已存在"})
		return
	}

	hashed, _ := auth.HashPassword(req.Password)
	if req.Role == "" {
		req.Role = models.RoleUser
	}

	newUser := models.User{
		Username:    req.Username,
		Password:    hashed,
		DisplayName: req.DisplayName,
		Role:        req.Role,
		Quota:       req.Quota,
		UsedQuota:   0,
		IsActive:    true,
		Department:  req.Department,
	}

	if err := database.DB.Create(&newUser).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建用户失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "用户创建成功", "data": newUser})
}

type UpdateUserRequest struct {
	DisplayName      string          `json:"display_name"`
	Role             models.UserRole `json:"role"`
	Quota            int             `json:"quota"`
	IsActive         bool            `json:"is_active"`
	IsPasswordLocked bool            `json:"is_password_locked"`
	Department       string          `json:"department"`
}

// UpdateAdminUser 更新用户信息
func UpdateAdminUser(c *gin.Context) {
	idStr := c.Param("id")
	id, _ := strconv.ParseUint(idStr, 10, 32)

	var u models.User
	if err := database.DB.First(&u, uint(id)).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}

	var req UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "数据无效"})
		return
	}

	u.DisplayName = req.DisplayName
	u.Role = req.Role
	u.Quota = req.Quota
	u.IsActive = req.IsActive
	u.IsPasswordLocked = req.IsPasswordLocked
	u.Department = req.Department

	database.DB.Save(&u)
	c.JSON(http.StatusOK, gin.H{"message": "用户信息已更新", "data": u})
}

// ToggleUserActive 快捷启用/禁用用户
func ToggleUserActive(c *gin.Context) {
	idStr := c.Param("id")
	id, _ := strconv.ParseUint(idStr, 10, 32)
	currentUser := GetCurrentUser(c)

	if currentUser.ID == uint(id) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "不可禁用您当前正在登录的管理员账号"})
		return
	}

	var u models.User
	if err := database.DB.First(&u, uint(id)).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}

	u.IsActive = !u.IsActive
	database.DB.Model(&u).Update("is_active", u.IsActive)

	statusStr := "启用"
	if !u.IsActive {
		statusStr = "停用"
	}
	c.JSON(http.StatusOK, gin.H{
		"message":   fmt.Sprintf("用户 [%s] 已被成功%s", u.Username, statusStr),
		"is_active": u.IsActive,
	})
}

// ToggleUserPasswordLock 快捷锁定/解锁用户密码修改权限
func ToggleUserPasswordLock(c *gin.Context) {
	idStr := c.Param("id")
	id, _ := strconv.ParseUint(idStr, 10, 32)

	var u models.User
	if err := database.DB.First(&u, uint(id)).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}

	u.IsPasswordLocked = !u.IsPasswordLocked
	database.DB.Model(&u).Update("is_password_locked", u.IsPasswordLocked)

	stateStr := "锁定(禁止自行修改密码)"
	if !u.IsPasswordLocked {
		stateStr = "解锁(允许自行修改密码)"
	}
	c.JSON(http.StatusOK, gin.H{
		"message":            fmt.Sprintf("用户 [%s] 的密码状态已设置为: %s", u.Username, stateStr),
		"is_password_locked": u.IsPasswordLocked,
	})
}

type ResetPasswordRequest struct {
	NewPassword string `json:"new_password" binding:"required,min=6"`
}

// ResetUserPassword 重置用户密码
func ResetUserPassword(c *gin.Context) {
	idStr := c.Param("id")
	id, _ := strconv.ParseUint(idStr, 10, 32)

	var u models.User
	if err := database.DB.First(&u, uint(id)).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}

	var req ResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "新密码至少需要6位字符"})
		return
	}

	hashed, _ := auth.HashPassword(req.NewPassword)
	database.DB.Model(&u).Update("password", hashed)

	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("已成功将用户 [%s] 的密码重置", u.Username)})
}

// GetAdminSettings 获取系统配置
func GetAdminSettings(c *gin.Context) {
	var settings []models.SystemSetting
	database.DB.Find(&settings)
	c.JSON(http.StatusOK, gin.H{"data": settings})
}

// SaveAdminSettings 保存系统配置
func SaveAdminSettings(c *gin.Context) {
	var body map[string]string
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "配置格式错误"})
		return
	}

	for k, v := range body {
		database.DB.Model(&models.SystemSetting{}).
			Where("key = ?", k).
			Updates(map[string]interface{}{
				"value":      v,
				"updated_at": time.Now(),
			})
	}

	c.JSON(http.StatusOK, gin.H{"message": "系统设置已更新并生效"})
}
