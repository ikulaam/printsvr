package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"printsvr/internal/auth"
	"printsvr/internal/database"
	"printsvr/internal/models"
)

const (
	CtxUserKey = "currentUser"
)

// AuthMiddleware JWT 认证中间件
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		var tokenStr string

		// 1. 尝试从 Authorization Header 获取
		authHeader := c.GetHeader("Authorization")
		if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
			tokenStr = strings.TrimPrefix(authHeader, "Bearer ")
		}

		// 2. 尝试从 Cookie 中获取 (支持浏览器原生页面访问与下载)
		if tokenStr == "" {
			if cookieVal, err := c.Cookie("auth_token"); err == nil && cookieVal != "" {
				tokenStr = cookieVal
			}
		}

		// 3. 尝试从 query parameter 获取 (方便在新标签打开文件预览/下载)
		if tokenStr == "" {
			tokenStr = c.Query("token")
		}

		if tokenStr == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "请先登录系统"})
			c.Abort()
			return
		}

		claims, err := auth.ParseToken(tokenStr)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "登录凭证无效或已过期，请重新登录"})
			c.Abort()
			return
		}

		// 校验用户是否存在并且启用
		var user models.User
		if err := database.DB.First(&user, claims.UserID).Error; err != nil || !user.IsActive {
			c.JSON(http.StatusForbidden, gin.H{"error": "用户账号不存在或已被停用"})
			c.Abort()
			return
		}

		c.Set(CtxUserKey, &user)
		c.Next()
	}
}

// AdminRequired 管理员权限保护
func AdminRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		userVal, exists := c.Get(CtxUserKey)
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
			c.Abort()
			return
		}

		user := userVal.(*models.User)
		if user.Role != models.RoleAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足，仅管理员可执行该操作"})
			c.Abort()
			return
		}

		c.Next()
	}
}

// GetCurrentUser 从上下文快捷获取当前用户
func GetCurrentUser(c *gin.Context) *models.User {
	if val, ok := c.Get(CtxUserKey); ok {
		return val.(*models.User)
	}
	return nil
}
