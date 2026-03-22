package tool

import (
	"context"
	"encoding/json"
	"os/exec"
	"runtime"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

type BashTool struct{}

func NewBashTool() *BashTool {
	return &BashTool{}
}

type BashToolParam struct {
	Command string `json:"command"`
}

func (t *BashTool) ToolName() AgentTool {
	return AgentToolBash
}

func (t *BashTool) Info() openai.ChatCompletionToolUnionParam {
	return openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
		Name:        string(AgentToolBash),
		Description: openai.String("execute bash command"),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "the bash command to execute",
				},
			},
			"required": []string{"command"},
		},
	})
}

func (t *BashTool) Execute(ctx context.Context, argumentsInJSON string) (string, error) {
	p := BashToolParam{}
	err := json.Unmarshal([]byte(argumentsInJSON), &p)
	if err != nil {
		return "", err
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		// Windows: use powershell to interpret the command line (more compatible)
		cmd = exec.CommandContext(ctx, "powershell", "-Command", p.Command)
	} else {
		// Linux/macOS: use POSIX sh
		cmd = exec.CommandContext(ctx, "sh", "-c", p.Command)
	}

	output, err := cmd.CombinedOutput()
	result := string(output)

	// 即使有错误也返回输出内容，错误信息附加在结果后面
	if err != nil {
		if result != "" {
			return result + "\n[Error: " + err.Error() + "]", nil
		}
		return "", err
	}
	return result, nil
}
