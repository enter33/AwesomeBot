package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

type EditTool struct {
	pathResolver *PathResolver
}

func NewEditTool() *EditTool {
	return &EditTool{
		pathResolver: NewPathResolver("", ""),
	}
}

// NewEditToolWithResolver 创建带 PathResolver 的 EditTool
func NewEditToolWithResolver(resolver *PathResolver) *EditTool {
	return &EditTool{
		pathResolver: resolver,
	}
}

type EditToolParam struct {
	Path       string `json:"path"`
	OldText    string `json:"old_text"`
	NewText    string `json:"new_text"`
	ReplaceAll bool   `json:"replace_all"`
}

func (t *EditTool) ToolName() AgentTool {
	return AgentToolEdit
}

func (t *EditTool) Info() openai.ChatCompletionToolUnionParam {
	return openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
		Name:        string(AgentToolEdit),
		Description: openai.String("Edit a file by replacing old_text with new_text.\n\nFeatures:\n- Smart matching (handles minor whitespace/line-ending differences)\n- Fuzzy matching when exact match fails\n- Shows best match suggestion if old_text not found\n- replace_all=true replaces every occurrence\n\nImportant:\n- old_text must uniquely identify the location (if multiple matches exist without replace_all, you'll be prompted)\n- For multiple identical changes, use replace_all=true\n- Preserves original line endings (LF/CRLF)"),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "the file path to edit",
				},
				"old_text": map[string]any{
					"type":        "string",
					"description": "the text to find and replace (must be unique within file, or use replace_all)",
				},
				"new_text": map[string]any{
					"type":        "string",
					"description": "the text to replace with",
				},
				"replace_all": map[string]any{
					"type":        "boolean",
					"description": "replace all occurrences of old_text (default false)",
				},
			},
			"required": []string{"path", "old_text", "new_text"},
		},
	})
}

func (t *EditTool) Execute(ctx context.Context, argumentsInJSON string) (string, error) {
	p := EditToolParam{}
	err := json.Unmarshal([]byte(argumentsInJSON), &p)
	if err != nil {
		return "", err
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

	// 检测 CRLF 并转换为 LF 处理
	usesCRLF := bytes.Contains(raw, []byte("\r\n"))
	content := strings.ReplaceAll(string(raw), "\r\n", "\n")
	oldText := strings.ReplaceAll(p.OldText, "\r\n", "\n")

	// 查找匹配
	match, count := findMatch(content, oldText)
	if match == "" {
		return t.notFoundMsg(oldText, content, p.Path, resolvedPath), nil
	}

	if count > 1 && !p.ReplaceAll {
		return fmt.Sprintf("Warning: old_text appears %d times. Provide more context to make it unique, or set replace_all=true.", count), nil
	}

	// 替换文本
	newText := strings.ReplaceAll(p.NewText, "\r\n", "\n")
	if p.ReplaceAll {
		content = strings.Replace(content, match, newText, -1)
	} else {
		content = strings.Replace(content, match, newText, 1)
	}

	// 恢复原始行结束符
	if usesCRLF {
		content = strings.ReplaceAll(content, "\n", "\r\n")
	}

	err = os.WriteFile(resolvedPath, []byte(content), 0644)
	if err != nil {
		return "", err
	}

	return "Successfully edited " + resolvedPath, nil
}

// findMatch 查找 oldText 在 content 中的匹配位置
// 首先尝试精确匹配，失败时使用 trimmed sliding window 匹配
// 返回 (matched_fragment, count)
func findMatch(content, oldText string) (string, int) {
	// 精确匹配
	if strings.Contains(content, oldText) {
		return oldText, strings.Count(content, oldText)
	}

	oldLines := strings.Split(oldText, "\n")
	if len(oldLines) == 0 {
		return "", 0
	}

	// 去除首尾空白后逐行比较
	strippedOld := make([]string, len(oldLines))
	for i, l := range oldLines {
		strippedOld[i] = strings.TrimSpace(l)
	}

	contentLines := strings.Split(content, "\n")

	// sliding window 匹配
	var candidates []string
	for i := 0; i <= len(contentLines)-len(strippedOld); i++ {
		window := contentLines[i : i+len(strippedOld)]
		strippedWindow := make([]string, len(window))
		for j, l := range window {
			strippedWindow[j] = strings.TrimSpace(l)
		}
		if matches(strippedOld, strippedWindow) {
			candidates = append(candidates, strings.Join(window, "\n"))
		}
	}

	if len(candidates) > 0 {
		return candidates[0], len(candidates)
	}
	return "", 0
}

// matches 比较两个字符串切片是否相等
func matches(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// notFoundMsg 当找不到匹配时，返回最相似位置的 unified diff
func (t *EditTool) notFoundMsg(oldText, content, displayPath, absPath string) string {
	lines := strings.Split(content, "\n")
	oldLines := strings.Split(oldText, "\n")
	window := len(oldLines)

	bestRatio := 0.0
	bestStart := 0

	for i := 0; i <= max(1, len(lines)-window); i++ {
		windowLines := lines[i : i+min(window, len(lines)-i)]
		ratio := similarity(oldLines, windowLines)
		if ratio > bestRatio {
			bestRatio = ratio
			bestStart = i
		}
	}

	if bestRatio > 0.5 {
		diff := unifiedDiff(oldLines, lines[bestStart:bestStart+min(window, len(lines)-bestStart)],
			"old_text (provided)", fmt.Sprintf("%s (actual, line %d)", displayPath, bestStart+1))
		return fmt.Sprintf("Error: old_text not found in %s.\nBest match (%.0f%% similar) at line %d:\n%s",
			displayPath, bestRatio*100, bestStart+1, diff)
	}
	return fmt.Sprintf("Error: old_text not found in %s. No similar text found. Verify the file content.", displayPath)
}

// similarity 计算两个字符串切片的相似度（简单版本）
func similarity(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	matches := 0
	for i := range min(len(a), len(b)) {
		if a[i] == b[i] {
			matches++
		}
	}
	return float64(matches) / float64(max(len(a), len(b)))
}

// unifiedDiff 生成 unified diff 格式的字符串
func unifiedDiff(from, to []string, fromFile, toFile string) string {
	var buf bytes.Buffer
	fromLines := len(from)
	toLines := len(to)

	// 计算上下文范围
	start := 0
	end := min(fromLines, toLines)

	// Header
	buf.WriteString("--- " + fromFile + "\n")
	buf.WriteString("+++ " + toFile + "\n")

	// Hunk header
	buf.WriteString(fmt.Sprintf("@@ -1,%d +1,%d @@\n", fromLines, toLines))

	// Content
	for i := start; i < end; i++ {
		if i < fromLines {
			buf.WriteString("-")
			buf.WriteString(from[i])
			buf.WriteString("\n")
		}
		if i < toLines {
			buf.WriteString("+")
			buf.WriteString(to[i])
			buf.WriteString("\n")
		}
	}
	// 处理多余的行
	if fromLines > toLines {
		for i := toLines; i < fromLines; i++ {
			buf.WriteString("-")
			buf.WriteString(from[i])
			buf.WriteString("\n")
		}
	} else if toLines > fromLines {
		for i := fromLines; i < toLines; i++ {
			buf.WriteString("+")
			buf.WriteString(to[i])
			buf.WriteString("\n")
		}
	}

	return buf.String()
}
