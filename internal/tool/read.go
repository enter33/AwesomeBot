package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

const (
	// MAX_CHARS 最大字符数限制
	MAX_CHARS = 128000
	// DEFAULT_LIMIT 默认最大行数
	DEFAULT_LIMIT = 2000
)

type ReadTool struct {
	pathResolver *PathResolver
}

func NewReadTool() *ReadTool {
	return &ReadTool{
		pathResolver: NewPathResolver("", ""),
	}
}

// NewReadToolWithResolver 创建带 PathResolver 的 ReadTool
func NewReadToolWithResolver(resolver *PathResolver) *ReadTool {
	return &ReadTool{
		pathResolver: resolver,
	}
}

type ReadToolParam struct {
	Path   string `json:"path"`
	Offset int    `json:"offset"` // 起始行号（1-indexed，默认1）
	Limit  int    `json:"limit"`  // 最大行数（默认2000）
}

func (t *ReadTool) ToolName() AgentTool {
	return AgentToolRead
}

func (t *ReadTool) Info() openai.ChatCompletionToolUnionParam {
	return openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
		Name:        string(AgentToolRead),
		Description: openai.String("read file content with optional line-based pagination. Returns numbered lines."),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "the file path to read",
				},
				"offset": map[string]any{
					"type":        "integer",
					"description": "line number to start reading from (1-indexed, default 1)",
					"minimum":     1,
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "maximum number of lines to read (default 2000)",
					"minimum":     1,
				},
			},
			"required": []string{"path"},
		},
	})
}

func (t *ReadTool) Execute(ctx context.Context, argumentsInJSON string) (string, error) {
	p := ReadToolParam{}
	err := json.Unmarshal([]byte(argumentsInJSON), &p)
	if err != nil {
		return "", err
	}

	// 设置默认值
	if p.Offset < 1 {
		p.Offset = 1
	}
	if p.Limit <= 0 {
		p.Limit = DEFAULT_LIMIT
	}

	// 解析路径
	resolvedPath, err := t.pathResolver.Resolve(p.Path)
	if err != nil {
		return "", err
	}

	fileInfo, err := os.Stat(resolvedPath)
	if err != nil {
		return "", err
	}
	if fileInfo.IsDir() {
		return "", fmt.Errorf("path is a directory")
	}

	raw, err := os.ReadFile(resolvedPath)
	if err != nil {
		return "", err
	}

	if len(raw) == 0 {
		return fmt.Sprintf("(Empty file: %s)", p.Path), nil
	}

	// 检测图片 MIME 类型
	mime := detectImageMime(raw)
	if mime != "" && strings.HasPrefix(mime, "image/") {
		return buildImageContentBlock(raw, mime, resolvedPath, p.Path), nil
	}

	// 尝试解码为 UTF-8 文本
	textContent, err := decodeUTF8(raw)
	if err != nil {
		return "", fmt.Errorf("cannot read binary file %s (MIME: %s). Only UTF-8 text and images are supported.", p.Path, mime)
	}

	allLines := strings.Split(textContent, "\n")
	total := len(allLines)

	if p.Offset > total {
		return "", fmt.Errorf("offset %d is beyond end of file (%d lines)", p.Offset, total)
	}

	start := p.Offset - 1
	end := min(start+p.Limit, total)

	// 构建带行号的内容
	var sb strings.Builder
	for i := start; i < end; i++ {
		sb.WriteString(fmt.Sprintf("%d| %s\n", i+1, allLines[i]))
	}

	result := sb.String()

	// 检查 MAX_CHARS 限制
	if len(result) > MAX_CHARS {
		result, end = trimToMaxChars(result, start, end, allLines)
	}

	// 添加分页提示
	if end < total {
		result += fmt.Sprintf("\n(Showing lines %d-%d of %d. Use offset=%d to continue.)", p.Offset, end, total, end+1)
	} else {
		result += fmt.Sprintf("\n(End of file — %d lines total)", total)
	}

	return result, nil
}

// trimToMaxChars trim result to MAX_CHARS and return adjusted end line
func trimToMaxChars(result string, start, end int, allLines []string) (string, int) {
	var sb strings.Builder
	chars := 0
	newEnd := start
	for i := start; i < len(allLines); i++ {
		line := fmt.Sprintf("%d| %s\n", i+1, allLines[i])
		chars += len(line)
		if chars > MAX_CHARS {
			break
		}
		sb.WriteString(line)
		newEnd = i + 1
	}
	return sb.String(), newEnd
}

// detectImageMime 检测图片 MIME 类型（基于文件头）
func detectImageMime(data []byte) string {
	if len(data) < 4 {
		return ""
	}
	// PNG
	if data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
		return "image/png"
	}
	// JPEG
	if data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "image/jpeg"
	}
	// GIF
	if data[0] == 0x47 && data[1] == 0x49 && data[2] == 0x46 {
		return "image/gif"
	}
	// WebP
	if len(data) >= 12 && data[0] == 0x52 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x46 &&
		data[8] == 0x57 && data[9] == 0x45 && data[10] == 0x42 && data[11] == 0x50 {
		return "image/webp"
	}
	// BMP
	if data[0] == 0x42 && data[1] == 0x4D {
		return "image/bmp"
	}
	return ""
}

// buildImageContentBlock 构建图片内容块
func buildImageContentBlock(data []byte, mime, absPath, displayPath string) string {
	// 返回图片信息，让上层处理图片显示
	return fmt.Sprintf("(Image file: %s, MIME: %s, Size: %d bytes)", displayPath, mime, len(data))
}

// decodeUTF8 尝试解码为 UTF-8 文本
func decodeUTF8(data []byte) (string, error) {
	// 验证是否为有效 UTF-8
	if !isValidUTF8(data) {
		return "", fmt.Errorf("not valid UTF-8")
	}
	return string(data), nil
}

// isValidUTF8 检查数据是否为有效的 UTF-8 编码
func isValidUTF8(data []byte) bool {
	return true // 简化的实现，实际应该做完整的 UTF-8 验证
}
