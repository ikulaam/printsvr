package config

import (
	"flag"
	"os"
	"path/filepath"
)

type Config struct {
	Port         string
	DataDir      string
	UploadDir    string
	DBPath       string
	JWTSecret    string
	MaxUploadMB  int64
	SimulateTime int // 仿真打印时每页耗时(毫秒)
}

var AppConfig *Config

func Load() *Config {
	port := flag.String("port", "8080", "Web server listening port")
	dataDir := flag.String("data", "./data", "Directory for data storage and SQLite DB")
	jwtSecret := flag.String("secret", "PrintSvr-Secret-Key-2026-Secure", "JWT signing secret")
	flag.Parse()

	// 环境变量优先覆盖
	if envPort := os.Getenv("PRINTSVR_PORT"); envPort != "" {
		*port = envPort
	}
	if envData := os.Getenv("PRINTSVR_DATA"); envData != "" {
		*dataDir = envData
	}
	if envSecret := os.Getenv("PRINTSVR_SECRET"); envSecret != "" {
		*jwtSecret = envSecret
	}

	uploadDir := filepath.Join(*dataDir, "uploads")
	dbPath := filepath.Join(*dataDir, "printsvr.db")

	_ = os.MkdirAll(uploadDir, 0755)

	AppConfig = &Config{
		Port:         *port,
		DataDir:      *dataDir,
		UploadDir:    uploadDir,
		DBPath:       dbPath,
		JWTSecret:    *jwtSecret,
		MaxUploadMB:  50,
		SimulateTime: 1200, // 仿真打印每份/页耗时约 1.2 秒
	}

	return AppConfig
}
