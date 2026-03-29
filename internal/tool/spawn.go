package tool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"

	"github.com/enter33/AwesomeBot/pkg/prompt"
)

// SpawnTool 创建子代理工具
type SpawnTool struct {
	creator CreateSubagentFunc
}

// CreateSubagentFunc 创建子代理的函数类型
type CreateSubagentFunc func(name string, subagentType string, systemPrompt string, task string) (string, error)

// NewSpawnTool 创建 SpawnTool 实例
func NewSpawnTool(creator CreateSubagentFunc) *SpawnTool {
	return &SpawnTool{
		creator: creator,
	}
}

// ToolName 返回工具名称
func (t *SpawnTool) ToolName() AgentTool {
	return "spawn"
}

// Info 返回工具定义
func (t *SpawnTool) Info() openai.ChatCompletionToolUnionParam {
	return openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
		Name:        "spawn",
		Description: openai.String("创建一个子代理执行任务。返回子代理 ID，需要调用 get_subagent_result 获取结果。"),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "子代理名称（可选）",
				},
				"type": map[string]any{
					"type":        "string",
					"description": "子代理类型: general-purpose, explore, plan",
					"enum":        []any{"general-purpose", "explore", "plan"},
				},
				"task": map[string]any{
					"type":        "string",
					"description": "分配给子代理的任务",
				},
			},
			"required": []any{"type", "task"},
		},
	})
}

// Execute 执行工具
func (t *SpawnTool) Execute(ctx context.Context, argumentsInJSON string) (string, error) {
	var args struct {
		Name string `json:"name"`
		Type string `json:"type"`
		Task string `json:"task"`
	}

	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("解析参数失败: %v", err)
	}

	if args.Task == "" {
		return "", fmt.Errorf("task 参数不能为空")
	}

	resultInstruction := fmt.Sprintf(
		"\n\n【重要】任务完成后，你必须用普通文本输出执行结果摘要（不要只使用工具调用）。"+
			"摘要格式：\n## 执行结果\n1. 发现了什么\n2. 关键结论\n3. 相关文件路径\n\n"+
			"必须输出文本内容，不能只调用工具。",
	)
	enhancedTask := args.Task + resultInstruction

	subagentType := args.Type
	if subagentType == "" {
		subagentType = "general-purpose"
	}

	systemPrompt := getPromptForType(subagentType)

	id, err := t.creator(args.Name, subagentType, systemPrompt, enhancedTask)
	if err != nil {
		return "", fmt.Errorf("创建 subagent 失败: %v", err)
	}

	return fmt.Sprintf(`{"subagent_id": "%s", "type": "%s", "status": "created", "task": "%s"}`,
		id, subagentType, escapeJSON(args.Task)), nil
}

func escapeJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1])
}

func getPromptForType(subagentType string) string {
	switch subagentType {
	case "explore":
		return prompt.ExploreAgentSystemPrompt
	case "plan":
		return prompt.PlanAgentSystemPrompt
	default:
		return prompt.CodingSubagentSystemPrompt
	}
}
