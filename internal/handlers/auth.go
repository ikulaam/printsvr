package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"printsvr/internal/auth"
	"printsvr/internal/database"
	"printsvr/internal/models"
)

type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type RegisterRequest struct {
	Username    string `json:"username" binding:"required,min=3,max=30"`
	Password    string `json:"password" binding:"required,min=6"`
	DisplayName string `json:"display_name" binding:"required"`
	Department  string `json:"department"`
}

type ChangePasswordRequest struct {
	OldPassword     string `json:"old_password" binding:"required"`
	NewPassword     string `json:"new_password" binding:"required,min=6"`
	ConfirmPassword string `json:"confirm_password"`
}

// Login 用户登录
func Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请输入有效的用户名和密码"})
		return
	}

	var user models.User
	if err := database.DB.Where("username = ?", req.Username).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码不正确"})
		return
	}

	if !user.IsActive {
		c.JSON(http.StatusForbidden, gin.H{"error": "该账号已被停用，请联系管理员"})
		return
	}

	if !auth.CheckPasswordHash(req.Password, user.Password) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码不正确"})
		return
	}

	token, err := auth.GenerateToken(&user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成会话凭据失败"})
		return
	}

	// 同时写入安全 Cookie
	c.SetCookie("auth_token", token, 3600*72, "/", "", false, true)

	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user": gin.H{
			"id":           user.ID,
			"username":     user.Username,
			"display_name": user.DisplayName,
			"role":         user.Role,
			"department":   user.Department,
			"quota":        user.Quota,
			"used_quota":   user.UsedQuota,
		},
	})
}

// Register 用户自助注册
func Register(c *gin.Context) {
	// 检查系统是否开放注册
	var allowReg models.SystemSetting
	if err := database.DB.Where("key = ?", "allow_register").First(&allowReg).Error; err == nil {
		if allowReg.Value != "true" {
			c.JSON(http.StatusForbidden, gin.H{"error": "当前系统已关闭自主注册功能，请联系管理员分配账号"})
			return
		}
	}

	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "注册数据格式不正确: " + err.Error()})
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	var existing models.User
	if err := database.DB.Where("username = ?", req.Username).First(&existing).Error; err == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "用户名已存在，请换一个"})
		return
	}

	hashedPwd, err := auth.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "密码加密异常"})
		return
	}

	// 读取默认配额
	defaultQuota := 200
	var quotaSetting models.SystemSetting
	if err := database.DB.Where("key = ?", "default_quota").First(&quotaSetting).Error; err == nil {
		if q, err := strconv.Atoi(quotaSetting.Value); err == nil {
			defaultQuota = q
		}
	}

	newUser := models.User{
		Username:    req.Username,
		Password:    hashedPwd,
		DisplayName: req.DisplayName,
		Role:        models.RoleUser,
		Quota:       defaultQuota,
		UsedQuota:   0,
		IsActive:    true,
		Department:  req.Department,
	}

	if err := database.DB.Create(&newUser).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建用户失败: " + err.Error()})
		return
	}

	token, _ := auth.GenerateToken(&newUser)
	c.SetCookie("auth_token", token, 3600*72, "/", "", false, true)

	c.JSON(http.StatusOK, gin.H{
		"message": "注册成功！",
		"token":   token,
		"user": gin.H{
			"id":           newUser.ID,
			"username":     newUser.Username,
			"display_name": newUser.DisplayName,
			"role":         newUser.Role,
			"department":   newUser.Department,
			"quota":        newUser.Quota,
			"used_quota":   newUser.UsedQuota,
		},
	})
}

// GetCurrentUserInfo 获取当前用户信息
func GetCurrentUserInfo(c *gin.Context) {
	user := GetCurrentUser(c)
	// 刷新数据库获取最新已用配额与锁定状态
	database.DB.First(user, user.ID)

	c.JSON(http.StatusOK, gin.H{
		"id":                 user.ID,
		"username":           user.Username,
		"display_name":       user.DisplayName,
		"role":               user.Role,
		"department":         user.Department,
		"quota":              user.Quota,
		"used_quota":         user.UsedQuota,
		"is_password_locked": user.IsPasswordLocked,
		"created_at":         user.CreatedAt,
	})
}

// Logout 退出登录
func Logout(c *gin.Context) {
	c.SetCookie("auth_token", "", -1, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"message": "已安全退出"})
}

// ChangePassword 修改密码
func ChangePassword(c *gin.Context) {
	user := GetCurrentUser(c)

	// 密码锁定校验: 公用或被锁账号不允许修改密码
	if user.IsPasswordLocked {
		c.JSON(http.StatusForbidden, gin.H{"error": "该账号已被管理员锁定密码，禁止修改！如有疑问请联系系统管理员。"})
		return
	}

	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "新密码至少需要6位字符"})
		return
	}

	if req.ConfirmPassword != "" && req.ConfirmPassword != req.NewPassword {
		c.JSON(http.StatusBadRequest, gin.H{"error": "两次输入的新密码不一致，请重新输入"})
		return
	}

	if !auth.CheckPasswordHash(req.OldPassword, user.Password) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "原密码输入不正确"})
		return
	}

	if req.OldPassword == req.NewPassword {
		c.JSON(http.StatusBadRequest, gin.H{"error": "新密码不能与原密码相同"})
		return
	}

	newHashed, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "密码加密失败"})
		return
	}

	database.DB.Model(user).Update("password", newHashed)
	c.JSON(http.StatusOK, gin.H{"message": "密码修改成功，请重新登录"})
}

// GetPublicInfo 获取系统公开信息（公告、名称等）
func GetPublicInfo(c *gin.Context) {
	var settings []models.SystemSetting
	database.DB.Find(&settings)

	info := make(map[string]string)
	for _, s := range settings {
		info[s.Key] = s.Value
	}

	c.JSON(http.StatusOK, gin.H{
		"site_name":          info["site_name"],
		"announcement":       info["announcement"],
		"allow_register":     info["allow_register"] == "true",
		"show_demo_accounts": info["show_demo_accounts"] != "false",
	})
}
