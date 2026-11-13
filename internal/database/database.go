package database

import (
	"log"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"printsvr/internal/auth"
	"printsvr/internal/config"
	"printsvr/internal/models"
)

var DB *gorm.DB

// InitDB 初始化数据库连接、迁移表结构及初始数据
func InitDB() (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(config.AppConfig.DBPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, err
	}

	// 自动迁移模型
	err = db.AutoMigrate(
		&models.User{},
		&models.Printer{},
		&models.PrintJob{},
		&models.SystemSetting{},
		&models.ActivityLog{},
	)
	if err != nil {
		return nil, err
	}

	DB = db

	// 种子数据初始化
	seedData(db)

	return db, nil
}

func seedData(db *gorm.DB) {
	// 1. 初始化系统配置
	settings := []models.SystemSetting{
		{Key: "site_name", Value: "内网共享打印服务平台 (PrintSvr)", Description: "系统名称"},
		{Key: "announcement", Value: "欢迎使用局域网共享打印系统！请节约纸张，双面打印优先。", Description: "系统公告"},
		{Key: "allow_register", Value: "true", Description: "是否开放用户自主注册"},
		{Key: "show_demo_accounts", Value: "true", Description: "登录页是否显示演示测试账号提示卡片"},
		{Key: "default_quota", Value: "200", Description: "新用户默认打印配额(页)"},
		{Key: "auto_cleanup_days", Value: "30", Description: "已完成文件自动清理天数"},
	}

	for _, s := range settings {
		var existing models.SystemSetting
		if err := db.Where("key = ?", s.Key).First(&existing).Error; err != nil {
			s.UpdatedAt = time.Now()
			db.Create(&s)
		}
	}

	// 2. 初始化管理员账号
	var adminCount int64
	db.Model(&models.User{}).Where("role = ?", models.RoleAdmin).Count(&adminCount)
	if adminCount == 0 {
		hashedPwd, _ := auth.HashPassword("admin123")
		admin := models.User{
			Username:    "admin",
			Password:    hashedPwd,
			DisplayName: "系统管理员",
			Role:        models.RoleAdmin,
			Quota:       -1, // 无限制
			IsActive:    true,
			Department:  "信息化运维中心",
		}
		if err := db.Create(&admin).Error; err == nil {
			log.Println("[Init] 初始管理员账号已创建: 用户名 admin, 初始密码 admin123")
		}
	}

	// 3. 初始化测试演示普通用户
	var userCount int64
	db.Model(&models.User{}).Where("username = ?", "demo").Count(&userCount)
	if userCount == 0 {
		hashedPwd, _ := auth.HashPassword("123456")
		demoUser := models.User{
			Username:    "demo",
			Password:    hashedPwd,
			DisplayName: "演示用户 (张工)",
			Role:        models.RoleUser,
			Quota:       100,
			UsedQuota:   12,
			IsActive:    true,
			Department:  "研发一部",
		}
		_ = db.Create(&demoUser).Error
	}

	// 4. 默认内置虚拟/演示打印机（保证即使未连物理机也能体验全部打印队列流程）
	var printerCount int64
	db.Model(&models.Printer{}).Count(&printerCount)
	if printerCount == 0 {
		defaultPrinters := []models.Printer{
			{
				Name:            "办公区激光打印机 (虚拟仿真)",
				SystemName:      "Virtual-Office-Laser",
				Location:        "总部 3F 开放办公区东侧",
				Status:          models.PrinterStatusIdle,
				IsDefault:       true,
				ColorSupported:  true,
				DuplexSupported: true,
				PaperSizes:      "A4,A3,B5",
				IsSimulation:    true,
				Description:     "全功能高速双面彩色激光机，适用于日常文件打印",
			},
			{
				Name:            "财务室黑白高速机 (虚拟仿真)",
				SystemName:      "Virtual-Finance-Mono",
				Location:        "财务部 208 室",
				Status:          models.PrinterStatusIdle,
				IsDefault:       false,
				ColorSupported:  false,
				DuplexSupported: true,
				PaperSizes:      "A4,B5",
				IsSimulation:    true,
				Description:     "票据与报表专用黑白打印机",
			},
		}
		for _, p := range defaultPrinters {
			db.Create(&p)
		}
	}
}
