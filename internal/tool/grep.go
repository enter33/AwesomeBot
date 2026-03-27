package tool

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

type GrepTool struct {
	pathResolver *PathResolver
}

func NewGrepTool() *GrepTool {
	return &GrepTool{
		pathResolver: NewPathResolver("", ""),
	}
}

func NewGrepToolWithResolver(resolver *PathResolver) *GrepTool {
	return &GrepTool{
		pathResolver: resolver,
	}
}

type GrepToolParam struct {
	Pattern     string `json:"pattern"`
	Path        string `json:"path"`
	Glob        string `json:"glob"`
	OutputMode  string `json:"output_mode"`
	Context     int    `json:"context"`
	Type        string `json:"type"`
	HeadLimit   int    `json:"head_limit"`
	Multiline   bool   `json:"multiline"`
	IgnoreCase  bool   `json:"ignore_case"`
}

func (t *GrepTool) ToolName() AgentTool {
	return AgentToolGrep
}

func (t *GrepTool) Info() openai.ChatCompletionToolUnionParam {
	return openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
		Name:        string(AgentToolGrep),
		Description: openai.String("Search for patterns in files using regular expressions.\n\nSupports:\n- Full regex syntax with optional multiline mode\n- File type filtering (go, js, py, etc.)\n- Glob pattern filtering for filenames\n- Context lines (lines before/after match)\n- Two output modes: 'content' (with line numbers) and 'files_with_matches'\n\nTip: Use 'files_with_matches' for finding which files contain a pattern, use 'content' for detailed results."),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{
					"type":        "string",
					"description": "regular expression pattern to search for",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "directory path to search in (default \".\")",
				},
				"glob": map[string]any{
					"type":        "string",
					"description": "glob pattern to filter filenames (e.g., \"*.go\", \"*.{js,ts}\")",
				},
				"output_mode": map[string]any{
					"type":        "string",
					"description": "output format: 'content' (default, shows filepath:linenum:content) or 'files_with_matches' (lists files only)",
					"enum":        []any{"content", "files_with_matches"},
				},
				"context": map[string]any{
					"type":        "integer",
					"description": "number of context lines before and after match (default 0)",
				},
				"type": map[string]any{
					"type":        "string",
					"description": "filter by file type/extension (e.g., \"go\", \"js\", \"py\")",
				},
				"head_limit": map[string]any{
					"type":        "integer",
					"description": "maximum number of matching lines to return (default unlimited)",
				},
				"multiline": map[string]any{
					"type":        "boolean",
					"description": "enable multiline matching mode (default false)",
				},
				"ignore_case": map[string]any{
					"type":        "boolean",
					"description": "perform case-insensitive matching (default false)",
				},
			},
			"required": []string{"pattern"},
		},
	})
}

func (t *GrepTool) Execute(ctx context.Context, argumentsInJSON string) (string, error) {
	p := GrepToolParam{}
	err := json.Unmarshal([]byte(argumentsInJSON), &p)
	if err != nil {
		return "", err
	}

	// 设置默认值
	if p.Path == "" {
		p.Path = "."
	}
	if p.OutputMode == "" {
		p.OutputMode = "content"
	}

	// 解析路径
	searchPath, err := t.pathResolver.Resolve(p.Path)
	if err != nil {
		return "", err
	}

	// 检查 ripgrep 是否可用
	if rgPath := findRipgrep(); rgPath != "" {
		return t.executeWithRipgrep(ctx, rgPath, searchPath, p)
	}

	// 回退到纯 Go 实现
	return t.executeWithGo(ctx, searchPath, p)
}

// findRipgrep 查找系统中的 ripgrep
func findRipgrep() string {
	// 尝试常见路径
	paths := []string{"rg", "ripgrep", "C:\\Program Files\\Ripgrep\\rg.exe"}
	for _, path := range paths {
		cmd := exec.Command(path, "--version")
		if cmd.Run() == nil {
			return path
		}
	}
	return ""
}

// executeWithRipgrep 使用 ripgrep 执行搜索
func (t *GrepTool) executeWithRipgrep(ctx context.Context, rgPath string, searchPath string, p GrepToolParam) (string, error) {
	args := []string{}

	// 输出模式
	if p.OutputMode == "files_with_matches" {
		args = append(args, "-l")
	} else {
		args = append(args, "-n") // 行号
		if p.Context > 0 {
			args = append(args, "-C", fmt.Sprintf("%d", p.Context))
		} else {
			args = append(args, "-n")
		}
	}

	// 忽略大小写
	if p.IgnoreCase {
		args = append(args, "-i")
	}

	// 多行模式
	if p.Multiline {
		args = append(args, "--multiline")
	}

	// 文件类型
	if p.Type != "" {
		args = append(args, "-t", p.Type)
	}

	// Glob 模式
	if p.Glob != "" {
		args = append(args, "-g", p.Glob)
	}

	// Head limit
	if p.HeadLimit > 0 {
		args = append(args, "-m", fmt.Sprintf("%d", p.HeadLimit))
	}

	// Pattern 和路径
	args = append(args, p.Pattern, searchPath)

	cmd := exec.CommandContext(ctx, rgPath, args...)
	cmd.Dir = searchPath

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// ripgrep 返回 1 表示没有匹配，这是正常的
			if exitErr.ExitCode() == 1 {
				return "", nil
			}
		}
		return "", err
	}

	return string(output), nil
}

// executeWithGo 使用纯 Go 实现搜索
func (t *GrepTool) executeWithGo(ctx context.Context, searchPath string, p GrepToolParam) (string, error) {
	// 编译正则表达式
	regexOpts := ""
	if p.Multiline {
		regexOpts += "m"
	}
	if p.IgnoreCase {
		regexOpts += "i"
	}

	pattern := p.Pattern
	if regexOpts != "" {
		pattern = "(?" + regexOpts + ")" + pattern
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex pattern: %w", err)
	}

	var results []string
	matchCount := 0
	hitLimit := p.HeadLimit > 0

	// 获取需要搜索的文件
	files, err := t.getFiles(searchPath, p.Glob, p.Type)
	if err != nil {
		return "", err
	}

	for _, file := range files {
		select {
		case <-ctx.Done():
			return strings.Join(results, "\n"), ctx.Err()
		default:
		}

		err := t.searchFile(file, re, p, &results, &matchCount)
		if err != nil {
			continue
		}

		if hitLimit && matchCount >= p.HeadLimit {
			break
		}
	}

	if len(results) == 0 {
		return "", nil
	}

	return strings.Join(results, "\n"), nil
}

// getFiles 获取需要搜索的文件列表
func (t *GrepTool) getFiles(searchPath, globPattern string, fileType string) ([]string, error) {
	var files []string

	info, err := os.Stat(searchPath)
	if err != nil {
		return nil, err
	}

	if info.IsDir() {
		err = filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil // 跳过无法访问的文件
			}
			if info.IsDir() {
				return nil
			}
			files = append(files, path)
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		files = append(files, searchPath)
	}

	// 过滤文件
	if globPattern != "" || fileType != "" {
		filtered := files[:0]
		for _, f := range files {
			if globPattern != "" {
				matched, err := filepath.Match(globPattern, filepath.Base(f))
				if err != nil || !matched {
					continue
				}
			}
			if fileType != "" {
				ext := strings.TrimPrefix(filepath.Ext(f), ".")
				if ext != fileType {
					continue
				}
			}
			filtered = append(filtered, f)
		}
		files = filtered
	}

	return files, nil
}

// searchFile 搜索单个文件
func (t *GrepTool) searchFile(file string, re *regexp.Regexp, p GrepToolParam, results *[]string, matchCount *int) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNum := 0
	var prevLines []string // 用于上下文

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		if p.OutputMode == "files_with_matches" {
			if re.MatchString(line) {
				*results = append(*results, file)
				return nil
			}
			continue
		}

		loc := re.FindStringIndex(line)
		if loc != nil {
			*matchCount++

			// 构建输出行
			output := fmt.Sprintf("%s:%d:%s", file, lineNum, line)

			// 添加上下文
			if p.Context > 0 {
				output = t.addContext(output, prevLines, lineNum, p.Context)
			}

			*results = append(*results, output)
		}

		// 维护上下文行
		if p.Context > 0 {
			prevLines = append(prevLines, line)
			if len(prevLines) > p.Context*2+1 {
				prevLines = prevLines[len(prevLines)-p.Context*2-1:]
			}
		}
	}

	return nil
}

// addContext 为匹配行添加上下文
func (t *GrepTool) addContext(output string, prevLines []string, lineNum int, context int) string {
	// prevLines 包含当前行之前的上下文行
	start := len(prevLines) - context
	if start < 0 {
		start = 0
	}

	var sb strings.Builder
	for i := start; i < len(prevLines); i++ {
		sb.WriteString(fmt.Sprintf("%s:%d:%s\n", filepath.Dir(output), lineNum-len(prevLines)+i, prevLines[i]))
	}
	sb.WriteString(output)
	return sb.String()
}