package subagent

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/google/uuid"

	"github.com/enter33/AwesomeBot/internal/agent"
	ctxengine "github.com/enter33/AwesomeBot/internal/context"
	"github.com/enter33/AwesomeBot/internal/logging"
	"github.com/enter33/AwesomeBot/internal/storage"
	"github.com/enter33/AwesomeBot/pkg/config"
	"github.com/enter33/AwesomeBot/pkg/llm"

	"github.com/enter33/AwesomeBot/internal/mcp"
	"github.com/enter33/AwesomeBot/internal/tool"
)

// Manager 子代理生命周期管理器
type Manager struct {
	mu        sync.RWMutex
	subagents map[string]*Instance

	// 创建 Agent 需要的资源
	llmConfig     config.Config
	confirmConfig agent.ToolConfirmConfig
	tools         []tool.Tool
	mcpClients    []*mcp.Client
	contextWindow int

	// 回调和完成通知
	statusCallbacks []StatusCallback
	completionCh    chan CompletionNotification
}

// NewManager 创建新的管理器
func NewManager(
	llmConfig config.Config,
	confirmConfig agent.ToolConfirmConfig,
	tools []tool.Tool,
	mcpClients []*mcp.Client,
	contextWindow int,
) *Manager {
	return &Manager{
		subagents:     make(map[string]*Instance),
		llmConfig:     llmConfig,
		confirmConfig: confirmConfig,
		tools:         tools,
		mcpClients:    mcpClients,
		contextWindow: contextWindow,
		completionCh:  make(chan CompletionNotification, 10),
	}
}

// filterOutTool 从工具列表中移除指定名称的工具
func filterOutTool(tools []tool.Tool, name tool.AgentTool) []tool.Tool {
	filtered := make([]tool.Tool, 0, len(tools))
	for _, t := range tools {
		if t.ToolName() != name {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// CreateSubagent 创建新的子代理 (实现 SubagentManager 接口)
func (m *Manager) CreateSubagent(name string, subagentType SubagentType, systemPrompt string, task string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := uuid.New().String()
	if name == "" {
		name = fmt.Sprintf("%s-%s", subagentType, id[:8])
	}

	// 1. 创建独立的 offload storage
	offloadStorage := storage.NewFileSystemStorage(
		filepath.Join(config.GetAwesomeDir(), "offload", "subagent", id))

	// 2. 创建上下文引擎
	policies := []ctxengine.Policy{
		ctxengine.NewOffloadPolicy(offloadStorage, 0.4, 0, 100, id),
		ctxengine.NewSummaryPolicy(nil, 10, 20, 0.6),
		ctxengine.NewTruncatePolicy(0, 0.85),
	}
	contextEngine := ctxengine.NewContextEngine(nil, policies, m.contextWindow, offloadStorage)

	// 3. 创建 LLM 客户端
	llmClient := llm.NewOpenAIClient(m.llmConfig)

	// 4. 过滤掉 spawn tool，防止 subagent 创建嵌套 subagent
	filteredTools := filterOutTool(m.tools, "spawn")

	// 5. 创建 Agent
	agentInstance := agent.NewAgent(
		m.llmConfig,
		systemPrompt,
		m.confirmConfig,
		filteredTools,
		m.mcpClients,
		contextEngine,
		llmClient,
		m.contextWindow,
	)

	// 5. 创建 Instance
	instance := &Instance{
		id:             id,
		name:           name,
		typ:            subagentType,
		status:         StatusCreated,
		agentInstance:  agentInstance,
		messageHistory: make([]string, 0),
		stopCh:         make(chan struct{}),
		task:           task,
		idleTimeout:    defaultIdleTimeout,
	}

	m.subagents[id] = instance

	// 设置完成通知 channel
	instance.SetCompletionCh(m.completionCh)

	// 注册 Manager 的回调到 Instance
	instance.RegisterStatusCallback(func(subagentID string, status SubagentStatus, result string, err error) {
		m.mu.RLock()
		callbacks := make([]StatusCallback, len(m.statusCallbacks))
		copy(callbacks, m.statusCallbacks)
		m.mu.RUnlock()

		for _, callback := range callbacks {
			callback(subagentID, status, result, err)
		}
	})

	// 6. 立即启动（后台运行），传入 task 作为初始 query
	go func() {
		ctx := context.Background()
		viewCh := make(chan agent.MessageVO, 10)
		confirmCh := make(chan agent.ConfirmationAction)
		err := instance.Run(ctx, task, viewCh, confirmCh)
		if err != nil {
			logging.Error("Failed to start subagent %s: %v", name, err)
		}
	}()

	return id, nil
}

// GetSubagent 根据 ID 获取子代理 (实现 SubagentSender 接口)
func (m *Manager) GetSubagent(id string) (Subagent, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	subagent, ok := m.subagents[id]
	return subagent, ok
}

// ListSubagents 列出所有子代理
func (m *Manager) ListSubagents() []Subagent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]Subagent, 0, len(m.subagents))
	for _, subagent := range m.subagents {
		result = append(result, subagent)
	}
	return result
}

// RemoveSubagent 移除子代理
func (m *Manager) RemoveSubagent(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	subagent, ok := m.subagents[id]
	if !ok {
		return fmt.Errorf("subagent not found: %s", id)
	}

	// 先尝试停止
	_ = subagent.Stop()
	delete(m.subagents, id)
	return nil
}

// SubagentStream 子代理流信息
type SubagentStream struct {
	ID        string
	Name      string
	ViewCh    chan agent.MessageVO
	ConfirmCh chan agent.ConfirmationAction
	Status    SubagentStatus
}

// ListStreams 返回所有运行中的子代理流
func (m *Manager) ListStreams() []SubagentStream {
	m.mu.RLock()
	defer m.mu.RUnlock()

	streams := make([]SubagentStream, 0, len(m.subagents))
	for _, s := range m.subagents {
		streams = append(streams, SubagentStream{
			ID:        s.id,
			Name:      s.name,
			ViewCh:    s.viewCh,
			ConfirmCh: s.confirmCh,
			Status:    s.status,
		})
	}
	return streams
}

// RegisterStatusCallback 注册状态回调
func (m *Manager) RegisterStatusCallback(callback StatusCallback) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statusCallbacks = append(m.statusCallbacks, callback)
}

// CompletionChan 返回完成通知 channel
func (m *Manager) CompletionChan() <-chan CompletionNotification {
	return m.completionCh
}
