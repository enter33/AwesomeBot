package subagent

import (
	"context"

	"github.com/enter33/AwesomeBot/internal/agent"
)

// SubagentType 子代理类型
type SubagentType string

const (
	SubagentTypeGeneral    SubagentType = "general-purpose"
	SubagentTypeExplore    SubagentType = "explore"
	SubagentTypePlan       SubagentType = "plan"
	SubagentTypeClaudeCode SubagentType = "claude-code-guide"
)

// SubagentStatus 子代理状态
type SubagentStatus string

const (
	StatusCreated   SubagentStatus = "created"
	StatusRunning   SubagentStatus = "running"
	StatusCompleted SubagentStatus = "completed"
	StatusFailed    SubagentStatus = "failed"
	StatusStopped   SubagentStatus = "stopped"
)

// StatusCallback 状态变化回调函数
type StatusCallback func(subagentID string, status SubagentStatus, result string, err error)

// CompletionNotification 完成通知结构体
type CompletionNotification struct {
	SubagentID string
	Status     SubagentStatus
	Result     string
	Err        error
}

// SubagentManager 子代理管理器接口（供 tool 包使用）
type SubagentManager interface {
	CreateSubagent(name string, subagentType SubagentType) (string, error)
	GetSubagent(id string) (Subagent, bool)
}

// SubagentSender 子代理消息发送者接口
type SubagentSender interface {
	GetSubagent(id string) (SubagentInfo, bool)
}

// SubagentInfo 子代理信息接口
type SubagentInfo interface {
	SendMessage(ctx context.Context, msg string) error
	Status() SubagentStatus
}

// Subagent 子代理接口
type Subagent interface {
	ID() string
	Name() string
	Type() SubagentType
	Status() SubagentStatus
	Task() string
	Result() string
	Run(ctx context.Context, query string, viewCh chan agent.MessageVO, confirmCh chan agent.ConfirmationAction) error
	SendMessage(ctx context.Context, msg string) error
	Stop() error
}
