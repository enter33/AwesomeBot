package orchestrator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/enter33/AwesomeBot/pkg/config"
)

// ErrorType 错误类型
type ErrorType string

const (
	ErrorTypeToolFailed      ErrorType = "tool_execution_failed"
	ErrorTypeLLMFailed       ErrorType = "llm_response_failed"
	ErrorTypeContextOverflow ErrorType = "context_overflow"
	ErrorTypeUnknown         ErrorType = "unknown"
)

// ErrorLog 错误日志
type ErrorLog struct {
	Phase         string    `json:"phase"`
	ErrorType     ErrorType `json:"error_type"`
	ErrorDetail   string    `json:"error_detail"`
	ContextSnap   string    `json:"context_snapshot"`
	LearnedLesson string    `json:"learned_lesson"`
	Timestamp     time.Time `json:"timestamp"`
	RetryCount    int       `json:"retry_count"`
}

// ErrorLogger 错误记录器
type ErrorLogger struct {
	mu      sync.RWMutex
	logs    []ErrorLog
	logPath string
}

func NewErrorLogger() *ErrorLogger {
	path := filepath.Join(config.GetAwesomeDir(), "logs", "orchestrator_errors.json")
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	return &ErrorLogger{logPath: path}
}

func (e *ErrorLogger) Log(err ErrorLog) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.logs = append(e.logs, err)
	e.persist()
}

func (e *ErrorLogger) persist() {
	content, _ := json.MarshalIndent(e.logs, "", "  ")
	_ = os.WriteFile(e.logPath, content, 0644)
}

func (e *ErrorLogger) GetLessons(phase string) []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var lessons []string
	for _, log := range e.logs {
		if log.Phase == phase && log.LearnedLesson != "" {
			lessons = append(lessons, log.LearnedLesson)
		}
	}
	return lessons
}
