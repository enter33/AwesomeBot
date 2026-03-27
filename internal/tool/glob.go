package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

type GlobTool struct {
	pathResolver *PathResolver
}

func NewGlobTool() *GlobTool {
	return &GlobTool{
		pathResolver: NewPathResolver("", ""),
	}
}

func NewGlobToolWithResolver(resolver *PathResolver) *GlobTool {
	return &GlobTool{
		pathResolver: resolver,
	}
}

type GlobToolParam struct {
	Pattern     string `json:"pattern"`
	Path        string `json:"path"`
	Recursive   bool   `json:"recursive"`
	MaxResults  int    `json:"max_results"`
}

func (t *GlobTool) ToolName() AgentTool {
	return AgentToolGlob
}

func (t *GlobTool) Info() openai.ChatCompletionToolUnionParam {
	return openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
		Name:        string(AgentToolGlob),
		Description: openai.String("Find files by matching glob patterns.\n\nSupports:\n- Standard glob patterns (e.g., \"**/*.go\", \"*.md\", \"src/**/*.ts\")\n- Recursive search with ** syntax\n- Non-recursive search for top-level only\n- Results sorted by modification time (newest first)\n- Configurable result limit\n\nTip: Use '**/*.go' to find all Go files recursively, '*.md' for markdown in current directory only."),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{
					"type":        "string",
					"description": "glob pattern to match files (e.g., \"**/*.go\", \"*.md\", \"src/**/*.ts\")",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "root directory to search from (default \".\")",
				},
				"recursive": map[string]any{
					"type":        "boolean",
					"description": "whether to search recursively (default true). Set to false for top-level only",
				},
				"max_results": map[string]any{
					"type":        "integer",
					"description": "maximum number of results to return (default 200)",
				},
			},
			"required": []string{"pattern"},
		},
	})
}

func (t *GlobTool) Execute(ctx context.Context, argumentsInJSON string) (string, error) {
	p := GlobToolParam{}
	err := json.Unmarshal([]byte(argumentsInJSON), &p)
	if err != nil {
		return "", err
	}

	// 设置默认值
	if p.Path == "" {
		p.Path = "."
	}
	if p.Recursive {
		// 默认启用递归
	} else {
		p.Recursive = true
	}
	if p.MaxResults <= 0 {
		p.MaxResults = 200
	}

	// 解析路径
	searchPath, err := t.pathResolver.Resolve(p.Path)
	if err != nil {
		return "", err
	}

	return t.glob(searchPath, p)
}

// glob 执行 glob 搜索
func (t *GlobTool) glob(searchPath string, p GlobToolParam) (string, error) {
	info, err := os.Stat(searchPath)
	if err != nil {
		return "", err
	}

	var matches []string

	if info.IsDir() {
		if p.Recursive {
			matches, err = t.globRecursive(searchPath, p.Pattern, p.MaxResults)
		} else {
			matches, err = t.globNonRecursive(searchPath, p.Pattern, p.MaxResults)
		}
	} else {
		// 如果搜索路径是文件，直接返回
		matches = append(matches, searchPath)
	}

	if err != nil {
		return "", err
	}

	if len(matches) == 0 {
		return "", nil
	}

	// 按修改时间排序（最新优先）
	sortByModTime(matches)

	// 限制结果数量
	if len(matches) > p.MaxResults {
		matches = matches[:p.MaxResults]
	}

	return strings.Join(matches, "\n"), nil
}

// globRecursive 递归搜索
func (t *GlobTool) globRecursive(root, pattern string, maxResults int) ([]string, error) {
	var matches []string

	// 将 pattern 转换为适合 Walk 的形式
	// 如果 pattern 以 **/ 开头，去掉 **/ 前缀，因为 Walk 会遍历所有子目录
	walkPattern := pattern
	if strings.HasPrefix(walkPattern, "**/") {
		walkPattern = walkPattern[3:]
	}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // 跳过无法访问的路径
		}

		if info.IsDir() {
			return nil
		}

		// 匹配 glob pattern
		matched, err := filepath.Match(walkPattern, info.Name())
		if err != nil {
			return nil
		}
		if matched {
			matches = append(matches, path)
			if len(matches) >= maxResults {
				return filepath.SkipAll
			}
		}

		return nil
	})

	if err != nil && err != filepath.SkipAll {
		return nil, err
	}

	return matches, nil
}

// globNonRecursive 非递归搜索（仅当前目录）
func (t *GlobTool) globNonRecursive(dir, pattern string, maxResults int) ([]string, error) {
	var matches []string

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		matched, err := filepath.Match(pattern, entry.Name())
		if err != nil {
			continue
		}
		if matched {
			fullPath := filepath.Join(dir, entry.Name())
			matches = append(matches, fullPath)
			if len(matches) >= maxResults {
				break
			}
		}
	}

	return matches, nil
}

// sortByModTime 按修改时间排序（最新优先）
func sortByModTime(paths []string) {
	type fileModTime struct {
		path    string
		modTime time.Time
	}

	files := make([]fileModTime, len(paths))
	for i, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			files[i] = fileModTime{path: p, modTime: time.Time{}}
			continue
		}
		files[i] = fileModTime{path: p, modTime: info.ModTime()}
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})

	for i, f := range files {
		paths[i] = f.path
	}
}