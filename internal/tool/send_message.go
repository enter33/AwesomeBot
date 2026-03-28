package tool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

// SendMessageResult 发送消息结果
type SendMessageResult struct {
	Status string
	Error  string
}

// SendMessageFunc 发送消息的函数类型
type SendMessageFunc func(subagentID string, message string) (SendMessageResult, error)

// SendMessageTool 发送消息到子代理
type SendMessageTool struct {
	sender SendMessageFunc
}

// NewSendMessageTool 创建 SendMessageTool 实例
func NewSendMessageTool(sender SendMessageFunc) *SendMessageTool {
	return &SendMessageTool{
		sender: sender,
	}
}

// ToolName 返回工具名称
func (t *SendMessageTool) ToolName() AgentTool {
	return "send_message"
}

// Info 返回工具定义
func (t *SendMessageTool) Info() openai.ChatCompletionToolUnionParam {
	return openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
		Name:        "send_message",
		Description: openai.String("向指定的子代理发送消息。"),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"subagent_id": map[string]any{
					"type":        "string",
					"description": "子代理 ID",
				},
				"message": map[string]any{
					"type":        "string",
					"description": "要发送的消息内容",
				},
			},
			"required": []any{"subagent_id", "message"},
		},
	})
}

// Execute 执行工具
func (t *SendMessageTool) Execute(ctx context.Context, argumentsInJSON string) (string, error) {
	var args struct {
		SubagentID string `json:"subagent_id"`
		Message    string `json:"message"`
	}

	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("解析参数失败: %v", err)
	}

	if args.SubagentID == "" {
		return "", fmt.Errorf("subagent_id 不能为空")
	}

	result, err := t.sender(args.SubagentID, args.Message)
	if err != nil {
		return "", fmt.Errorf("发送消息失败: %v", err)
	}

	if result.Error != "" {
		return "", fmt.Errorf("%s", result.Error)
	}

	return fmt.Sprintf(`{"subagent_id": "%s", "status": "%s", "message": "消息已发送"}`,
		args.SubagentID, result.Status), nil
}
