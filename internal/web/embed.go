package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static/* templates/*
var embeddedFiles embed.FS

// GetStaticFS 获取内嵌的 static 静态资源子文件系统
func GetStaticFS() http.FileSystem {
	sub, err := fs.Sub(embeddedFiles, "static")
	if err != nil {
		panic(err)
	}
	return http.FS(sub)
}

// GetIndexHTML 获取首页模板内容
func GetIndexHTML() ([]byte, error) {
	return embeddedFiles.ReadFile("templates/index.html")
}
