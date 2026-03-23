package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

const (
	// DEFAULT_MAX_ENTRIES 默认最大条目数
	DEFAULT_MAX_ENTRIES = 200
)

// ignoreDirs 忽略的目录集合
var ignoreDirs = map[string]bool{
	".git":            true,
	"node_modules":    true,
	"__pycache__":     true,
	".venv":           true,
	"venv":            true,
	"dist":            true,
	"build":           true,
	".tox":            true,
	".mypy_cache":     true,
	".pytest_cache":   true,
	".ruff_cache":     true,
	".coverage":       true,
	"htmlcov":         true,
	".idea":           true,
	".vscode":         true,
	"vendor":          true,
	"target":          true,
	".gradle":         true,
	"bin":             true,
	"obj":             true,
	"packages":        true,
	".next":           true,
	".nuxt":           true,
	".svelte-kit":     true,
}

type ListDirTool struct {
	pathResolver *PathResolver
}

func NewListDirTool() *ListDirTool {
	return &ListDirTool{
		pathResolver: NewPathResolver("", ""),
	}
}

// NewListDirToolWithResolver 创建带 PathResolver 的 ListDirTool
func NewListDirToolWithResolver(resolver *PathResolver) *ListDirTool {
	return &ListDirTool{
		pathResolver: resolver,
	}
}

type ListDirToolParam struct {
	Path       string `json:"path"`
	Recursive  bool   `json:"recursive"`
	MaxEntries *int   `json:"max_entries"` // 默认200
}

func (t *ListDirTool) ToolName() AgentTool {
	return AgentToolList
}

func (t *ListDirTool) Info() openai.ChatCompletionToolUnionParam {
	return openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
		Name:        string(AgentToolList),
		Description: openai.String("list the contents of a directory. Set recursive=true to explore nested structure. Common noise directories (.git, node_modules, __pycache__, etc.) are auto-ignored."),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "the directory path to list",
				},
				"recursive": map[string]any{
					"type":        "boolean",
					"description": "recursively list all files (default false)",
				},
				"max_entries": map[string]any{
					"type":        "integer",
					"description": "maximum entries to return (default 200)",
					"minimum":     1,
				},
			},
			"required": []string{"path"},
		},
	})
}

func (t *ListDirTool) Execute(ctx context.Context, argumentsInJSON string) (string, error) {
	p := ListDirToolParam{}
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
	if !fileInfo.IsDir() {
		return "", fmt.Errorf("path is not a directory")
	}

	cap := DEFAULT_MAX_ENTRIES
	if p.MaxEntries != nil && *p.MaxEntries > 0 {
		cap = *p.MaxEntries
	}

	var items []string
	total := 0

	if p.Recursive {
		items, total = t.listRecursive(resolvedPath, cap)
	} else {
		items, total = t.listNonRecursive(resolvedPath, cap)
	}

	if len(items) == 0 && total == 0 {
		return fmt.Sprintf("Directory %s is empty", p.Path), nil
	}

	result := strings.Join(items, "\n")
	if total > cap {
		result += fmt.Sprintf("\n\n(truncated, showing first %d of %d entries)", cap, total)
	}
	return result, nil
}

// listNonRecursive 非递归列出目录内容
func (t *ListDirTool) listNonRecursive(dir string, cap int) ([]string, int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0
	}

	var items []string
	total := 0

	// 排序：目录在前，文件在后
	sort.Slice(entries, func(i, j int) bool {
		aIsDir := entries[i].IsDir()
		bIsDir := entries[j].IsDir()
		if aIsDir != bIsDir {
			return aIsDir
		}
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if ignoreDirs[entry.Name()] {
			continue
		}
		total++
		if len(items) < cap {
			isDir := entry.IsDir()
			if isDir {
				items = append(items, fmt.Sprintf("📁 %s/", entry.Name()))
			} else {
				items = append(items, fmt.Sprintf("📄 %s", entry.Name()))
			}
		}
	}

	return items, total
}

// listRecursive 递归列出目录内容
func (t *ListDirTool) listRecursive(dir string, cap int) ([]string, int) {
	var items []string
	total := 0

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		// 根目录不处理
		if path == dir {
			return nil
		}

		// 获取相对路径
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return nil
		}

		// 检查是否在忽略目录中
		parts := strings.Split(rel, string(filepath.Separator))
		for _, part := range parts {
			if ignoreDirs[part] {
				return filepath.SkipDir
			}
		}

		total++
		if len(items) < cap {
			isDir := info.IsDir()
			if isDir {
				items = append(items, fmt.Sprintf("%s/", rel))
			} else {
				items = append(items, rel)
			}
		}

		return nil
	})

	if err != nil {
		return items, total
	}

	// 排序
	sort.Strings(items)

	return items, total
}
