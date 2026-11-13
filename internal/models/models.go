package models

import (
	"time"
)

type UserRole string

const (
	RoleAdmin UserRole = "admin"
	RoleUser  UserRole = "user"
)

type User struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Username    string    `gorm:"uniqueIndex;size:64;not null" json:"username"`
	Password    string    `gorm:"size:255;not null" json:"-"`
	DisplayName string    `gorm:"size:64" json:"display_name"`
	Role        UserRole  `gorm:"size:20;default:'user'" json:"role"`
	Quota       int       `gorm:"default:-1" json:"quota"`           // -1 表示无限制
	UsedQuota   int       `gorm:"default:0" json:"used_quota"`       // 已消耗打印页数
	IsActive         bool      `gorm:"default:true" json:"is_active"`             // 是否允许使用
	IsPasswordLocked bool      `gorm:"default:false" json:"is_password_locked"`   // 是否锁定密码(禁止自行修改)
	Department       string    `gorm:"size:64" json:"department"`                // 部门/科室/班级
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type PrinterStatus string

const (
	PrinterStatusIdle    PrinterStatus = "idle"
	PrinterStatusBusy    PrinterStatus = "busy"
	PrinterStatusOffline PrinterStatus = "offline"
	PrinterStatusError   PrinterStatus = "error"
)

type Printer struct {
	ID              uint          `gorm:"primaryKey" json:"id"`
	Name            string        `gorm:"size:100;not null" json:"name"`            // 显示名称，如 "研发中心HP激光机"
	SystemName      string        `gorm:"size:255" json:"system_name"`             // 操作系统实际打印机名称
	Location        string        `gorm:"size:120" json:"location"`                // 物理位置
	Status          PrinterStatus `gorm:"size:20;default:'idle'" json:"status"`    // 状态
	IsDefault       bool          `gorm:"default:false" json:"is_default"`         // 是否默认
	ColorSupported  bool          `gorm:"default:false" json:"color_supported"`    // 是否支持彩色
	DuplexSupported bool          `gorm:"default:true" json:"duplex_supported"`    // 是否支持双面
	PaperSizes      string        `gorm:"size:120;default:'A4,A3,B5'" json:"paper_sizes"` // 逗号隔开
	IsSimulation    bool          `gorm:"default:false" json:"is_simulation"`      // 是否虚拟仿真打印模式
	Description     string        `gorm:"size:255" json:"description"`             // 备注
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
}

type JobStatus string

const (
	JobStatusPending    JobStatus = "pending"
	JobStatusProcessing JobStatus = "processing"
	JobStatusCompleted  JobStatus = "completed"
	JobStatusFailed     JobStatus = "failed"
	JobStatusCancelled  JobStatus = "cancelled"
)

type PrintJob struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	JobNumber    string     `gorm:"uniqueIndex;size:32;not null" json:"job_number"` // 例如 "JOB-20261113-0001"
	UserID       uint       `gorm:"not null;index" json:"user_id"`
	User         User       `gorm:"foreignKey:UserID" json:"user,omitempty"`
	PrinterID    uint       `gorm:"not null;index" json:"printer_id"`
	Printer      Printer    `gorm:"foreignKey:PrinterID" json:"printer,omitempty"`
	FileName     string     `gorm:"size:255;not null" json:"file_name"`             // 原始文件名
	StoredName   string     `gorm:"size:255;not null" json:"-"`                     // 磁盘存储文件名
	FilePath     string     `gorm:"size:512;not null" json:"-"`                     // 完整物理路径
	FileSize     int64      `json:"file_size"`                                      // 字节大小
	FileType     string     `gorm:"size:30" json:"file_type"`                       // pdf, docx, png, txt 等
	Copies       int        `gorm:"default:1" json:"copies"`                        // 打印份数
	PageCount    int        `gorm:"default:1" json:"page_count"`                    // 估算页数
	IsColor      bool       `gorm:"default:false" json:"is_color"`                  // 彩色打印
	Duplex       string     `gorm:"size:20;default:'simplex'" json:"duplex"`        // simplex, duplex_long, duplex_short
	PaperSize    string     `gorm:"size:20;default:'A4'" json:"paper_size"`         // A4, A3 等
	Orientation  string     `gorm:"size:20;default:'portrait'" json:"orientation"`  // portrait, landscape
	Priority     int        `gorm:"default:0;index" json:"priority"`                // 优先级，越大越优先(加急=100)
	Status       JobStatus  `gorm:"size:20;default:'pending';index" json:"status"`  // 状态
	Progress     int        `gorm:"default:0" json:"progress"`                      // 0 - 100
	ErrorMessage string     `gorm:"type:text" json:"error_message,omitempty"`
	Remark       string     `gorm:"size:255" json:"remark,omitempty"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	PrintedAt    *time.Time `json:"printed_at,omitempty"`
	CreatedAt    time.Time  `gorm:"index" json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type SystemSetting struct {
	Key         string    `gorm:"primaryKey;size:64" json:"key"`
	Value       string    `gorm:"type:text" json:"value"`
	Description string    `gorm:"size:255" json:"description"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ActivityLog struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	UserID    uint      `gorm:"index" json:"user_id"`
	Username  string    `gorm:"size:64" json:"username"`
	Action    string    `gorm:"size:64" json:"action"`
	Detail    string    `gorm:"type:text" json:"detail"`
	IP        string    `gorm:"size:64" json:"ip"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
}
