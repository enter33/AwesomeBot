package logging

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/awesome/awesomebot/pkg/config"
)

const maxLogFiles = 20

var (
	logger *Logger
)

// Logger 日志记录器
type Logger struct {
	file *os.File
	path string
}

// Init 初始化日志系统
func Init() error {
	dir := config.GetAwesomeDir()
	if dir == "" {
		// 无法获取用户目录，使用临时目录
		dir = os.TempDir()
	}

	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("创建日志目录失败: %w", err)
	}

	// 生成唯一日志文件名: awesome_YYYYMMDD_HHMMSS_ffffff.log
	now := time.Now()
	timestamp := now.Format("20060102_150405")
	randStr := fmt.Sprintf("%06d", rand.Intn(1000000))
	logName := fmt.Sprintf("awesome_%s_%s.log", timestamp, randStr)
	logPath := filepath.Join(logDir, logName)

	file, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("创建日志文件失败: %w", err)
	}

	logger = &Logger{
		file: file,
		path: logPath,
	}

	// 清理旧日志文件
	if err := cleanupOldLogs(logDir); err != nil {
		// 日志清理失败不影响主流程
		fmt.Fprintf(os.Stderr, "清理旧日志失败: %v\n", err)
	}

	Info("日志系统初始化完成")
	Info("日志文件: %s", logPath)

	return nil
}

// cleanupOldLogs 清理超过最大数量的旧日志文件
func cleanupOldLogs(logDir string) error {
	pattern := filepath.Join(logDir, "awesome_*.log")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}

	if len(matches) <= maxLogFiles {
		return nil
	}

	// 按修改时间排序，删除最旧的
	var files []fileInfo
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		files = append(files, fileInfo{path: path, time: info.ModTime()})
	}

	// 冒泡排序找出最旧的文件
	for i := 0; i < len(files)-1; i++ {
		for j := 0; j < len(files)-i-1; j++ {
			if files[j].time.After(files[j+1].time) {
				files[j], files[j+1] = files[j+1], files[j]
			}
		}
	}

	// 删除最旧的文件直到只剩 maxLogFiles 个
	deleteCount := len(files) - maxLogFiles
	for i := 0; i < deleteCount; i++ {
		os.Remove(files[i].path)
	}

	return nil
}

type fileInfo struct {
	path string
	time time.Time
}

// GetLogPath 获取当前日志文件路径
func GetLogPath() string {
	if logger == nil {
		return ""
	}
	return logger.path
}

// Info 输出 Info 级别日志
func Info(format string, args ...interface{}) {
	if logger == nil {
		return
	}
	log("INFO", format, args...)
}

// Error 输出 Error 级别日志
func Error(format string, args ...interface{}) {
	if logger == nil {
		return
	}
	log("ERROR", format, args...)
}

// Debug 输出 Debug 级别日志
func Debug(format string, args ...interface{}) {
	if logger == nil {
		return
	}
	log("DEBUG", format, args...)
}

// Warn 输出 Warn 级别日志
func Warn(format string, args ...interface{}) {
	if logger == nil {
		return
	}
	log("WARN", format, args...)
}

func log(level, format string, args ...interface{}) {
	if logger == nil {
		return
	}
	msg := fmt.Sprintf(format, args...)
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	line := fmt.Sprintf("[%s] [%s] %s\n", timestamp, level, msg)
	logger.file.WriteString(line)
}

// Close 关闭日志系统
func Close() {
	if logger != nil && logger.file != nil {
		logger.file.Close()
	}
}

// SetOutput 设置日志输出目标（用于测试）
func SetOutput(file *os.File) {
	if logger != nil {
		logger.file = file
	}
}

// GetWorkspaceDir 获取工作目录
func GetWorkspaceDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	return dir
}

// GetHomeDir 获取用户主目录
func GetHomeDir() string {
	dir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return dir
}

// SanitizePath 清理路径中的敏感信息
func SanitizePath(path string) string {
	home := GetHomeDir()
	if home != "" {
		path = strings.ReplaceAll(path, home, "~")
	}
	return path
}
