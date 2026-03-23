package tool

import (
	"os"
	"path/filepath"
	"strings"
)

// PathResolver 统一处理路径解析
type PathResolver struct {
	workspaceDir string
	allowedDir   string
}

// NewPathResolver 创建 PathResolver
func NewPathResolver(workspaceDir, allowedDir string) *PathResolver {
	return &PathResolver{
		workspaceDir: workspaceDir,
		allowedDir:   allowedDir,
	}
}

// Resolve 解析路径：
// - ~ 扩展为用户主目录
// - 相对路径基于 workspaceDir 解析
// - 安全检查不允许访问 allowedDir 之外的目录
func (r *PathResolver) Resolve(path string) (string, error) {
	// 处理 ~ 扩展
	resolved := expandUser(path)

	// 如果不是绝对路径，基于 workspaceDir 解析
	if !filepath.IsAbs(resolved) {
		if r.workspaceDir != "" {
			resolved = filepath.Join(r.workspaceDir, resolved)
		}
	}

	// 获取绝对路径
	absPath, err := filepath.Abs(resolved)
	if err != nil {
		return "", err
	}

	// 安全检查：确保路径在 allowedDir 之下
	if r.allowedDir != "" {
		allowedAbs, err := filepath.Abs(r.allowedDir)
		if err != nil {
			return "", err
		}
		if !r.isUnder(absPath, allowedAbs) {
			return "", &PathOutsideAllowedError{Path: path, AllowedDir: r.allowedDir}
		}
	}

	return absPath, nil
}

// expandUser 处理 ~ 扩展为用户主目录
func expandUser(path string) string {
	if strings.HasPrefix(path, "~") {
		home := os.Getenv("HOME")
		if home == "" {
			home = os.Getenv("USERPROFILE")
		}
		if home != "" {
			path = filepath.Join(home, path[1:])
		}
	}
	return path
}

// isUnder 检查 path 是否在 dir 之下
func (r *PathResolver) isUnder(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	// 如果相对路径以 .. 开头，说明在 dir 之外
	return !strings.HasPrefix(rel, "..")
}

// PathOutsideAllowedError 路径超出允许目录的错误
type PathOutsideAllowedError struct {
	Path       string
	AllowedDir string
}

func (e *PathOutsideAllowedError) Error() string {
	return "path " + e.Path + " is outside allowed directory " + e.AllowedDir
}
