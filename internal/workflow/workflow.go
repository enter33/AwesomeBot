package workflow

import (
	"github.com/enter33/AwesomeBot/internal/subagent"
)

// Workflow 工作流定义
type Workflow struct {
	ID                 string
	Name               string
	Description        string
	EntryNode          string
	Nodes              []string
	DefaultTransitions map[string]string
	Router             RouterConfig
}

// RouterConfig 路由配置
type RouterConfig struct {
	Enabled      bool     `yaml:"enabled"`
	TriggerAfter []string `yaml:"trigger_after"`
}

// Node 工作流节点定义
type Node struct {
	ID            string
	Name          string
	Description   string
	ExecutionMode string // "subagent" | "main_agent"
	SubagentType  subagent.SubagentType
	Prompt        string
	InputTemplate string
	OutputFormat  string
	RetryLimit    int
}

// ExecutionMode 常量
const (
	ExecutionModeSubagent  = "subagent"
	ExecutionModeMainAgent = "main_agent"
)
