package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

type WriteTool struct{}

func NewWriteTool() *WriteTool {
	return &WriteTool{}
}

type WriteToolParam struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (t *WriteTool) ToolName() AgentTool {
	return AgentToolWrite
}

func (t *WriteTool) Info() openai.ChatCompletionToolUnionParam {
	return openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
		Name:        string(AgentToolWrite),
		Description: openai.String("write content to a file, creating or overwriting the file"),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "the file path to write to",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "the content to write to the file",
				},
			},
			"required": []string{"path", "content"},
		},
	})
}

func (t *WriteTool) Execute(ctx context.Context, argumentsInJSON string) (string, error) {
	p := WriteToolParam{}
	err := json.Unmarshal([]byte(argumentsInJSON), &p)
	if err != nil {
		return "", err
	}

	// 确保目录存在
	dir := os.Getenv("HOME")
	if dir == "" {
		// Windows
		dir = os.Getenv("USERPROFILE")
	}
	if dir == "" {
		// 如果都获取不到，使用当前目录
		dir = "."
	}

	// 检查是否是绝对路径，如果不是则拼接到工作目录
	if !filepath.IsAbs(p.Path) {
		cwd, _ := os.Getwd()
		p.Path = filepath.Join(cwd, p.Path)
	}

	// 确保父目录存在
	parentDir := filepath.Dir(p.Path)
	if parentDir != "" {
		if err := os.MkdirAll(parentDir, 0755); err != nil {
			return "", err
		}
	}

	err = os.WriteFile(p.Path, []byte(p.Content), 0644)
	if err != nil {
		return "", err
	}

	return "File written successfully: " + p.Path, nil
}
