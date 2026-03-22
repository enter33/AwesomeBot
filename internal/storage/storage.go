package storage

import "context"

// Storage 存储接口
type Storage interface {
	Load(ctx context.Context, key string) (string, error)
	Store(ctx context.Context, key string, value string) error
}

// MemoryStorage 内存存储
type MemoryStorage struct {
	data map[string]string
}

// NewMemoryStorage 创建内存存储
func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{
		data: make(map[string]string),
	}
}

func (s *MemoryStorage) Load(ctx context.Context, key string) (string, error) {
	return s.data[key], nil
}

func (s *MemoryStorage) Store(ctx context.Context, key string, value string) error {
	s.data[key] = value
	return nil
}
