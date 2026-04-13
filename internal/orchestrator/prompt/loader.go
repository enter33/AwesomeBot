package prompt

import (
	"os"
	"path/filepath"
)

// Loader 提示词加载器
type Loader struct {
	basePath string
}

// NewLoader 创建新的加载器
func NewLoader(basePath string) *Loader {
	return &Loader{basePath: basePath}
}

// Load 加载指定名称的提示词文件
func (l *Loader) Load(name string) (string, error) {
	path := filepath.Join(l.basePath, name+".txt")
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(content), nil
}
