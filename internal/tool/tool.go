package tool

import (
	"context"

	"github.com/openai/openai-go/v3"
)

// AgentTool 工具类型
type AgentTool = string

const (
	AgentToolRead      AgentTool = "read"
	AgentToolWrite     AgentTool = "write"
	AgentToolEdit      AgentTool = "edit"
	AgentToolList      AgentTool = "list"
	AgentToolBash      AgentTool = "bash"
	AgentToolLoadSkill AgentTool = "load_skill"
	AgentToolWebSearch AgentTool = "web_search"
	AgentToolWebFetch  AgentTool = "web_fetch"
)

// Tool 工具接口
type Tool interface {
	ToolName() AgentTool
	Info() openai.ChatCompletionToolUnionParam
	Execute(ctx context.Context, argumentsInJSON string) (string, error)
}
