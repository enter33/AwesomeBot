package memory

import (
	"context"
	"strings"
	"sync"

	"github.com/enter33/AwesomeBot/internal/storage"
	"github.com/enter33/AwesomeBot/pkg/config"
)

type Memory interface {
	String() string
	Update(ctx context.Context, newMessages []config.OpenAIMessage, callback UpdateCallback) error
	Enabled() bool
	ShouldNotify() bool
}

type MultiLevelMemory struct {
	content MemoryContent

	globalStorage    storage.Storage
	globalKey        string
	workspaceStorage storage.Storage
	workspaceKey     string

	updater MemoryUpdater
	mu      sync.RWMutex // 保护 content 共享状态
}

type MemoryUpdater interface {
	Update(ctx context.Context, oldMemory MemoryContent, newMessages []config.OpenAIMessage, onDone UpdateCallback) (MemoryContent, error)
	Enabled() bool
	ShouldNotify() bool
}

func NewMultiLevelMemory(globalStorage storage.Storage, workspaceStorage storage.Storage, u MemoryUpdater) *MultiLevelMemory {
	m := &MultiLevelMemory{
		globalStorage:    globalStorage,
		globalKey:        "/memory/MEMORY.md",
		workspaceStorage: workspaceStorage,
		workspaceKey:     "/.memory/MEMORY.md",
		updater:          u,
	}
	m.content = m.load()
	return m
}

func (m *MultiLevelMemory) load() MemoryContent {
	ctx := context.Background()
	content := MemoryContent{}

	content.WorkspaceMemory, _ = m.workspaceStorage.Load(ctx, m.workspaceKey)
	content.GlobalMemory, _ = m.globalStorage.Load(ctx, m.globalKey)

	return content
}

func (m *MultiLevelMemory) String() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.content.String()
}

func (m *MultiLevelMemory) Enabled() bool {
	return m.updater.Enabled()
}

func (m *MultiLevelMemory) ShouldNotify() bool {
	return m.updater.ShouldNotify()
}

type UpdateCallback func(newMemory MemoryContent, shouldNotify bool, err error)

func (m *MultiLevelMemory) Update(ctx context.Context, newMessages []config.OpenAIMessage, callback UpdateCallback) error {
	if len(newMessages) == 0 {
		return nil
	}

	_, err := m.updater.Update(ctx, m.content, newMessages, func(newMem MemoryContent, shouldNotify bool, updaterErr error) {
		if updaterErr != nil {
			if callback != nil {
				callback(newMem, shouldNotify, updaterErr)
			}
			return
		}

		if err := m.globalStorage.Store(ctx, m.globalKey, newMem.GlobalMemory); err != nil {
			if callback != nil {
				callback(newMem, shouldNotify, err)
			}
			return
		}
		if err := m.workspaceStorage.Store(ctx, m.workspaceKey, newMem.WorkspaceMemory); err != nil {
			if callback != nil {
				callback(newMem, shouldNotify, err)
			}
			return
		}

		m.mu.Lock()
		m.content = newMem
		m.mu.Unlock()

		if callback != nil {
			callback(newMem, shouldNotify, nil)
		}
	})
	if err != nil {
		return err
	}

	return nil
}

// MemoryContent 记忆内容
type MemoryContent struct {
	GlobalMemory    string `json:"global_memory,omitempty"`
	WorkspaceMemory string `json:"workspace_memory,omitempty"`
}

func (m *MemoryContent) String() string {
	prompt := memoryPromptTemplate
	prompt = strings.ReplaceAll(prompt, "{global_memory}", m.GlobalMemory)
	prompt = strings.ReplaceAll(prompt, "{workspace_memory}", m.WorkspaceMemory)
	return prompt
}

const memoryPromptTemplate = `### Global Memory
Here is the memory about the user among all conversations:
{global_memory}

### Workspace Memory
The memory of the current workspace is:
{workspace_memory}
`
