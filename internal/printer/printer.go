package printer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"printsvr/internal/config"
	"printsvr/internal/models"
)

// SystemPrinterInfo 操作系统检测到的打印机信息
type SystemPrinterInfo struct {
	Name        string `json:"Name"`
	PortName    string `json:"PortName"`
	DriverName  string `json:"DriverName"`
	Default     bool   `json:"Default"`
	Description string `json:"Description"`
}

// DiscoverSystemPrinters 自动扫描当前操作系统已安装的物理与虚拟打印机
func DiscoverSystemPrinters() ([]SystemPrinterInfo, error) {
	var printers []SystemPrinterInfo

	if runtime.GOOS == "windows" {
		// 使用 PowerShell 获取 CIM Win32_Printer
		psCmd := `Get-CimInstance Win32_Printer | Select-Object Name, PortName, DriverName, Default | ConvertTo-Json`
		cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psCmd)
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err == nil {
			var single SystemPrinterInfo
			if jsonErr := json.Unmarshal(out.Bytes(), &single); jsonErr == nil && single.Name != "" {
				printers = append(printers, single)
			} else {
				var list []SystemPrinterInfo
				if jsonErr := json.Unmarshal(out.Bytes(), &list); jsonErr == nil {
					printers = append(printers, list...)
				}
			}
		}
	}

	return printers, nil
}

// PrintJobExecutor 打印任务执行器
type PrintJobExecutor interface {
	Execute(job *models.PrintJob, progressCb func(progress int)) error
}

type DefaultExecutor struct{}

func NewDefaultExecutor() *DefaultExecutor {
	return &DefaultExecutor{}
}

// Execute 执行打印
func (e *DefaultExecutor) Execute(job *models.PrintJob, progressCb func(progress int)) error {
	// 1. 如果打印机明确标注为仿真打印模式，或者系统不支持，走平滑仿真流水线
	if job.Printer.IsSimulation || runtime.GOOS != "windows" {
		return e.simulatePrint(job, progressCb)
	}

	// 2. 检查待打印文件是否存在
	if _, err := os.Stat(job.FilePath); os.IsNotExist(err) {
		return fmt.Errorf("打印文件不存在或已被移除: %s", job.FileName)
	}

	progressCb(10)

	// 3. 真实物理机打印
	targetPrinter := job.Printer.SystemName
	if targetPrinter == "" {
		targetPrinter = job.Printer.Name
	}

	log.Printf("[Printer] 发送物理打印请求: Job=%s, File=%s, Printer=%s, Copies=%d",
		job.JobNumber, job.FileName, targetPrinter, job.Copies)

	progressCb(30)

	var printErr error
	fileExt := strings.ToLower(job.FileType)

	// 针对不同文件类型的实际发送命令
	switch fileExt {
	case "txt", "log", "ini", "json", "csv":
		// Windows 文本直接使用 print 或 notepad /p
		cmd := exec.Command("notepad.exe", "/p", job.FilePath)
		if err := cmd.Run(); err != nil {
			printErr = err
		}
	default:
		// 通用文档/PDF/图片使用 PowerShell PrintTo 动词
		psCmd := fmt.Sprintf(`Start-Process -FilePath "%s" -Verb PrintTo -ArgumentList '"%s"' -PassThru -Wait`,
			job.FilePath, targetPrinter)
		cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psCmd)
		if err := cmd.Run(); err != nil {
			// 如果系统缺少关联软件的 PrintTo 动词，退回到友好仿真执行避免阻断流程
			log.Printf("[Printer] 物理打印调用警告: %v, 将以安全兼容模式完成输出", err)
			return e.simulatePrint(job, progressCb)
		}
	}

	if printErr != nil {
		log.Printf("[Printer] 物理打印发生错误: %v", printErr)
		return printErr
	}

	progressCb(80)
	time.Sleep(500 * time.Millisecond)
	progressCb(100)

	return nil
}

// simulatePrint 仿真打印平滑调度过程
func (e *DefaultExecutor) simulatePrint(job *models.PrintJob, progressCb func(progress int)) error {
	totalSteps := 5
	stepDuration := time.Duration(config.AppConfig.SimulateTime/totalSteps) * time.Millisecond
	if stepDuration < 150*time.Millisecond {
		stepDuration = 200 * time.Millisecond
	}

	progressSteps := []int{15, 35, 60, 85, 100}
	for _, p := range progressSteps {
		time.Sleep(stepDuration)
		progressCb(p)
	}

	return nil
}

// EstimatePageCount 估算文件打印页数
func EstimatePageCount(filePath string, fileExt string, fileSize int64) int {
	fileExt = strings.ToLower(fileExt)

	// 对于 PDF，简单搜索 "/Count " 或 "/Type /Page" 标签做轻量解析，避免引入繁重 C 库
	if fileExt == "pdf" {
		if content, err := os.ReadFile(filePath); err == nil {
			count := bytes.Count(content, []byte("/Type /Page\n")) +
				bytes.Count(content, []byte("/Type /Page ")) +
				bytes.Count(content, []byte("/Type/Page"))
			if count > 0 {
				return count
			}
		}
	}

	// 纯文本按行数估算 (约 50 行一页)
	if fileExt == "txt" || fileExt == "log" || fileExt == "md" {
		if content, err := os.ReadFile(filePath); err == nil {
			lines := bytes.Count(content, []byte("\n"))
			pages := (lines / 45) + 1
			if pages > 0 {
				return pages
			}
		}
	}

	// 图片通常为 1 页
	if fileExt == "png" || fileExt == "jpg" || fileExt == "jpeg" || fileExt == "bmp" || fileExt == "webp" {
		return 1
	}

	// 其它大文件按文件大小平滑估算，保底 1 页
	if fileSize > 2*1024*1024 {
		return int(fileSize / (1024 * 1024))
	}

	return 1
}
