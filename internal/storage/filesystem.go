package storage

import (
	"context"
	"os"
	"path/filepath"
)

// FileSystemStorage 文件系统存储
type FileSystemStorage struct {
	baseDir string
}

// NewFileSystemStorage 创建文件系统存储
func NewFileSystemStorage(baseDir string) *FileSystemStorage {
	return &FileSystemStorage{
		baseDir: baseDir,
	}
}

// Store 保存数据
func (fs *FileSystemStorage) Store(ctx context.Context, key string, value string) error {
	filePath := filepath.Clean(filepath.Join(fs.baseDir, key))
	// 如果目录不存在，创建目录
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	err := os.WriteFile(filePath, []byte(value), 0644)
	return err
}

// Load 加载数据
func (fs *FileSystemStorage) Load(ctx context.Context, key string) (string, error) {
	filePath := filepath.Clean(filepath.Join(fs.baseDir, key))
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return string(content), nil
}
