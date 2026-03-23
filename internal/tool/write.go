package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

type WriteTool struct {
	pathResolver *PathResolver
}

func NewWriteTool() *WriteTool {
	return &WriteTool{
		pathResolver: NewPathResolver("", ""),
	}
}

// NewWriteToolWithResolver 创建带 PathResolver 的 WriteTool
func NewWriteToolWithResolver(resolver *PathResolver) *WriteTool {
	return &WriteTool{
		pathResolver: resolver,
	}
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

	// 解析路径
	resolvedPath, err := t.pathResolver.Resolve(p.Path)
	if err != nil {
		return "", err
	}

	// 确保父目录存在
	parentDir := filepath.Dir(resolvedPath)
	if parentDir != "" {
		if err := os.MkdirAll(parentDir, 0755); err != nil {
			return "", err
		}
	}

	err = os.WriteFile(resolvedPath, []byte(p.Content), 0644)
	if err != nil {
		return "", err
	}

	return "File written successfully: " + resolvedPath, nil
}
