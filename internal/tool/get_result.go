package tool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

// GetResultTool 获取子代理结果的工具
type GetResultTool struct {
	getResultCh func(subagentID string) (<-chan ResultNotification, error)
}

// ResultNotification 结果通知
type ResultNotification struct {
	Status string // "completed", "failed", "stopped"
	Result string
	Err    error
}

// NewGetResultTool 创建 GetResultTool 实例
func NewGetResultTool(getResultCh func(subagentID string) (<-chan ResultNotification, error)) *GetResultTool {
	return &GetResultTool{
		getResultCh: getResultCh,
	}
}

// ToolName 返回工具名称
func (t *GetResultTool) ToolName() AgentTool {
	return "get_subagent_result"
}

// Info 返回工具定义
func (t *GetResultTool) Info() openai.ChatCompletionToolUnionParam {
	return openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
		Name:        "get_subagent_result",
		Description: openai.String("等待子代理完成并获取执行结果。此工具会阻塞直到子代理完成。"),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"subagent_id": map[string]any{
					"type":        "string",
					"description": "子代理 ID（由 spawn 工具返回）",
				},
			},
			"required": []any{"subagent_id"},
		},
	})
}

// Execute 执行工具
func (t *GetResultTool) Execute(ctx context.Context, argumentsInJSON string) (string, error) {
	var args struct {
		SubagentID string `json:"subagent_id"`
	}

	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("解析参数失败: %v", err)
	}

	if args.SubagentID == "" {
		return "", fmt.Errorf("subagent_id 不能为空")
	}

	resultCh, err := t.getResultCh(args.SubagentID)
	if err != nil {
		return fmt.Sprintf(`{"status": "error", "error": "%s"}`, err.Error()), nil
	}

	select {
	case <-ctx.Done():
		return `{"status": "cancelled", "error": "等待被取消"}`, nil
	case notification, ok := <-resultCh:
		if !ok {
			return `{"status": "error", "error": "channel closed"}`, nil
		}

		var errMsg string
		if notification.Err != nil {
			errMsg = notification.Err.Error()
		}

		return fmt.Sprintf(`{"status": "%s", "error": %s, "result": %s}`,
			notification.Status, mustMarshalJSON(errMsg), mustMarshalJSON(notification.Result)), nil
	}
}

func mustMarshalJSON(s string) string {
	if s == "" {
		return `""`
	}
	b, _ := json.Marshal(s)
	return string(b)
}
