package context

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"strings"

	"github.com/openai/openai-go/v3"

	"github.com/enter33/AwesomeBot/internal/memory"
	"github.com/enter33/AwesomeBot/internal/skill"
	"github.com/enter33/AwesomeBot/internal/storage"
	"github.com/enter33/AwesomeBot/pkg/config"
)

type messageWrap struct {
	Message    config.OpenAIMessage
	Tokens     int
	OffloadKey string // 如果这条消息是从 offload 恢复的，记录其 key
}

// Engine 上下文引擎
type Engine struct {
	systemPromptTemplate string
	messages             []messageWrap
	policies             []Policy
	onPolicyEvent        func(policyName string, running bool, err error)
	onMemoryEvent        func(running bool, err error)
	contextTokens        int
	contextWindow        int
	offloadStorage       storage.Storage

	memory       memory.Memory
	skillManager SkillManager
}

// TokenBudget Token 预算
type TokenBudget struct {
	ContextWindow int
}

// Usage Token 使用量
type Usage struct {
	PromptTokens int
}

// TurnDraft 轮次草稿
type TurnDraft struct {
	NewMessages []config.OpenAIMessage
}

// SkillManager 技能管理器接口
type SkillManager interface {
	FormatForPrompt() string
}

// NewContextEngine 创建上下文引擎
func NewContextEngine(mem memory.Memory, policies []Policy, contextWindow int, offloadStorage storage.Storage) *Engine {
	skillManager := skill.NewManager()
	_ = skillManager.LoadAll()
	return &Engine{
		policies:      policies,
		messages:      make([]messageWrap, 0),
		contextWindow: contextWindow,
		memory:        mem,
		skillManager:  skillManager,
		offloadStorage: offloadStorage,
	}
}

// Init 初始化引擎
func (c *Engine) Init(systemPrompt string, budget TokenBudget) {
	c.systemPromptTemplate = systemPrompt
	if budget.ContextWindow > 0 {
		c.contextWindow = budget.ContextWindow
	}
}

// BuildRequestMessages 构建请求消息
func (c *Engine) BuildRequestMessages() []config.OpenAIMessage {
	result := make([]config.OpenAIMessage, 0, len(c.messages)+1)
	if c.systemPromptTemplate != "" {
		result = append(result, openai.SystemMessage(c.BuildSystemPrompt()))
	}
	for i := range c.messages {
		result = append(result, c.messages[i].Message)
	}
	return result
}

// StartTurn 开始轮次
func (c *Engine) StartTurn(userMsg config.OpenAIMessage) TurnDraft {
	return TurnDraft{
		NewMessages: []config.OpenAIMessage{userMsg},
	}
}

// CommitTurn 提交轮次
func (c *Engine) CommitTurn(ctx context.Context, draft TurnDraft, usage Usage, skipPoliciesAndMemory bool) error {
	for i := range draft.NewMessages {
		msg := draft.NewMessages[i]
		c.messages = append(c.messages, messageWrap{Message: msg, Tokens: CountTokens(msg)})
	}
	c.recountTokens()

	if skipPoliciesAndMemory {
		return nil
	}

	// 更新记忆（先于上下文压缩）
	shouldNotify := c.memory.ShouldNotify()
	if shouldNotify {
		if c.onMemoryEvent != nil {
			c.onMemoryEvent(true, nil)
		}
	}
	err := c.memory.Update(ctx, draft.NewMessages)
	if shouldNotify {
		if c.onMemoryEvent != nil {
			c.onMemoryEvent(false, err)
		}
	}
	if err != nil {
		return err
	}

	if err := c.applyPolicies(ctx); err != nil {
		return err
	}

	return nil
}

// AbortTurn 中止轮次
func (c *Engine) AbortTurn(_ TurnDraft) {
	// no-op: draft is only in-memory and never committed unless CommitTurn is called.
}

// GetContextUsage 获取上下文使用率
func (c *Engine) GetContextUsage() float64 {
	if c.contextWindow <= 0 {
		return 0
	}
	return float64(c.contextTokens) / float64(c.contextWindow)
}

func (c *Engine) recountTokens() {
	totalTokens := 0
	for i := range c.messages {
		totalTokens += c.messages[i].Tokens
	}
	c.contextTokens = totalTokens
}

func (c *Engine) applyPolicies(ctx context.Context) error {
	var allRemovedKeys []string

	for _, policy := range c.policies {
		if !policy.ShouldApply(ctx, c) {
			continue
		}
		if c.onPolicyEvent != nil {
			c.onPolicyEvent(policy.Name(), true, nil)
		}
		result, err := policy.Apply(ctx, c)
		if c.onPolicyEvent != nil {
			c.onPolicyEvent(policy.Name(), false, err)
		}
		if err != nil {
			return fmt.Errorf("apply policy %s: %w", policy.Name(), err)
		}

		allRemovedKeys = append(allRemovedKeys, result.RemovedKeys...)
		c.messages = result.Messages
		c.recountTokens()
	}

	// 清理不再需要的 offload 内容
	for _, key := range allRemovedKeys {
		if err := c.offloadStorage.Delete(ctx, key); err != nil {
			log.Printf("failed to delete offload key %s: %v", key, err)
		}
	}

	return nil
}

// SetPolicyEventHook 设置策略事件钩子
func (c *Engine) SetPolicyEventHook(hook func(policyName string, running bool, err error)) {
	c.onPolicyEvent = hook
}

// SetMemoryEventHook 设置记忆事件钩子
func (c *Engine) SetMemoryEventHook(hook func(running bool, err error)) {
	c.onMemoryEvent = hook
}

// BuildSystemPrompt 构建系统提示词
func (c *Engine) BuildSystemPrompt() string {
	replaceMap := make(map[string]string)
	replaceMap["{runtime}"] = runtime.GOOS
	replaceMap["{workspace_path}"] = config.GetWorkspaceDir()

	if c.memory != nil {
		replaceMap["{memory}"] = c.memory.String()
	} else {
		replaceMap["{memory}"] = ""
	}

	if c.skillManager != nil {
		replaceMap["{skills}"] = c.skillManager.FormatForPrompt()
	} else {
		replaceMap["{skills}"] = ""
	}

	prompt := c.systemPromptTemplate
	for k, v := range replaceMap {
		prompt = strings.ReplaceAll(prompt, k, v)
	}
	return prompt
}

// Reset 重置（清空所有消息，保留 system prompt）
func (c *Engine) Reset() {
	// 清理所有 offload 文件
	for _, msg := range c.messages {
		if msg.OffloadKey != "" {
			c.offloadStorage.Delete(context.Background(), msg.OffloadKey)
		}
	}
	c.messages = make([]messageWrap, 0)
	c.contextTokens = 0
}

