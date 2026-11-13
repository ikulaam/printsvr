package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"printsvr/internal/config"
	"printsvr/internal/database"
	"printsvr/internal/handlers"
	"printsvr/internal/queue"
	"printsvr/internal/web"
)

func main() {
	// 1. 加载配置
	cfg := config.Load()

	// 2. 初始化数据库
	db, err := database.InitDB()
	if err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}

	// 3. 启动打印队列调度中心
	queueMgr := queue.InitQueue(db)
	defer queueMgr.Stop()

	// 4. 设置 Gin Web 路由
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// 静态文件服务 (基于内嵌 embed.FS)
	r.StaticFS("/static", web.GetStaticFS())

	// 首页
	r.GET("/", func(c *gin.Context) {
		htmlBytes, err := web.GetIndexHTML()
		if err != nil {
			c.String(http.StatusInternalServerError, "加载首页失败")
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", htmlBytes)
	})

	// 公开接口
	r.GET("/api/public/info", handlers.GetPublicInfo)
	authGroup := r.Group("/api/auth")
	{
		authGroup.POST("/login", handlers.Login)
		authGroup.POST("/register", handlers.Register)
	}

	// 登录用户通用接口 (需要 AuthMiddleware)
	userGroup := r.Group("/api")
	userGroup.Use(handlers.AuthMiddleware())
	{
		userGroup.GET("/auth/me", handlers.GetCurrentUserInfo)
		userGroup.POST("/auth/logout", handlers.Logout)
		userGroup.POST("/auth/password", handlers.ChangePassword)

		// 打印机列表
		userGroup.GET("/printers", handlers.GetPrinters)

		// 打印任务与队列
		userGroup.POST("/jobs", handlers.CreatePrintJob)
		userGroup.GET("/jobs", handlers.GetUserJobs)
		userGroup.GET("/jobs/:id", handlers.GetJobDetail)
		userGroup.POST("/jobs/:id/cancel", handlers.CancelUserJob)
		userGroup.POST("/jobs/:id/retry", handlers.RetryUserJob)
		userGroup.GET("/jobs/:id/download", handlers.DownloadJobFile)
	}

	// 管理员专属接口 (需要 AuthMiddleware + AdminRequired)
	adminGroup := r.Group("/api")
	adminGroup.Use(handlers.AuthMiddleware(), handlers.AdminRequired())
	{
		// 仪表盘大盘
		adminGroup.GET("/admin/dashboard", handlers.GetAdminDashboard)

		// 全局打印队列调度
		adminGroup.GET("/admin/jobs", handlers.GetAdminJobs)
		adminGroup.POST("/admin/jobs/:id/prioritize", handlers.PrioritizeJob)
		adminGroup.POST("/admin/jobs/:id/cancel", handlers.AdminCancelJob)
		adminGroup.DELETE("/admin/jobs/history", handlers.ClearHistoryJobs)

		// 打印机设备管理与发现
		adminGroup.GET("/printers/discover", handlers.DiscoverPrinters)
		adminGroup.POST("/printers/import", handlers.ImportPrinter)
		adminGroup.PUT("/printers/:id", handlers.UpdatePrinter)
		adminGroup.DELETE("/printers/:id", handlers.DeletePrinter)
		adminGroup.POST("/printers/:id/test", handlers.TestPrintPage)

		// 用户管理
		adminGroup.GET("/admin/users", handlers.GetAdminUsers)
		adminGroup.POST("/admin/users", handlers.CreateAdminUser)
		adminGroup.PUT("/admin/users/:id", handlers.UpdateAdminUser)
		adminGroup.POST("/admin/users/:id/reset-password", handlers.ResetUserPassword)
		adminGroup.POST("/admin/users/:id/toggle-active", handlers.ToggleUserActive)
		adminGroup.POST("/admin/users/:id/toggle-password-lock", handlers.ToggleUserPasswordLock)

		// 系统设置
		adminGroup.GET("/admin/settings", handlers.GetAdminSettings)
		adminGroup.POST("/admin/settings", handlers.SaveAdminSettings)
	}

	// 5. 启动 HTTP 服务
	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("启动服务异常: %v", err)
		}
	}()

	printBanner(cfg.Port)

	// 优雅关闭监听
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("[PrintSvr] 正在平稳停止共享打印服务...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("[PrintSvr] 强制关闭服务: %v", err)
	}
	log.Println("[PrintSvr] 共享打印服务已退出")
}

func printBanner(port string) {
	fmt.Println("==================================================================")
	fmt.Println("   🖨️  PrintSvr - 内网局域网共享打印应用平台已成功启动！")
	fmt.Println("==================================================================")
	fmt.Printf("   本地访问地址 : http://localhost:%s\n", port)

	// 打印所有局域网可访问的 IP 地址
	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, addr := range addrs {
			if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
				if ipNet.IP.To4() != nil {
					fmt.Printf("   内网共享地址 : http://%s:%s\n", ipNet.IP.String(), port)
				}
			}
		}
	}

	fmt.Println("------------------------------------------------------------------")
	fmt.Println("   默认管理员账号: admin   默认管理员密码: admin123")
	fmt.Println("   默认测试用户:   demo    默认测试密码:   123456")
	fmt.Println("==================================================================")
}
