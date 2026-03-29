package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"

	"github.com/enter33/AwesomeBot/internal/logging"
)

// GetResultTool 获取子代理结果的工具
type GetResultTool struct {
	getter func(subagentID string) string
}

// NewGetResultTool 创建 GetResultTool 实例
func NewGetResultTool(getter func(subagentID string) string) *GetResultTool {
	return &GetResultTool{
		getter: getter,
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
		Description: openai.String("获取子代理的完整执行结果摘要。如果返回结果中包含 [WAITING] 标记，表示子代理仍在运行中，此时不要生成任何其他输出，只需等待一段时间后再次调用此工具。"),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"subagent_id": map[string]any{
					"type":        "string",
					"description": "子代理 ID",
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

	// 轮询直到 subagent 完成
	for {
		result := t.getter(args.SubagentID)

		// 解析结果判断状态
		if strings.Contains(result, "[WAITING]") {
			// subagent 仍在运行，等待后继续轮询
			logging.Info("[subagent polling] waiting for subagent to complete...")

			select {
			case <-ctx.Done():
				// Context 取消时，立即重新检查状态
				// Stop() 已先设置 status = StatusStopped，所以 getter 会返回最新状态
				result = t.getter(args.SubagentID)
				if strings.Contains(result, "[STOPPED]") {
					return fmt.Sprintf(`{"result": "", "status": "stopped", "error": "子代理已被终止"}`), nil
				}
				if strings.Contains(result, "[FAILED]") {
					return fmt.Sprintf(`{"result": "", "status": "failed", "error": "子代理执行失败"}`), nil
				}
				return result, nil
			case <-time.After(2 * time.Second):
				continue
			}
		}

		// 子代理不存在、已失败、已终止或有实际结果，直接返回
		// 根据状态返回不同格式
		if strings.Contains(result, "[FAILED]") {
			return fmt.Sprintf(`{"result": "", "status": "failed", "error": "子代理执行失败"}`), nil
		}
		if strings.Contains(result, "[NOT_FOUND]") {
			return fmt.Sprintf(`{"result": "", "status": "error", "error": "子代理不存在"}`), nil
		}
		if strings.Contains(result, "[STOPPED]") {
			return fmt.Sprintf(`{"result": "", "status": "stopped", "error": "子代理已被终止"}`), nil
		}
		return fmt.Sprintf(`{"result": "%s"}`, result), nil
	}
}
