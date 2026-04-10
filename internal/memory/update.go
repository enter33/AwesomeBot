package memory

import (
	"context"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/openai/openai-go/v3"

	"github.com/enter33/AwesomeBot/pkg/config"
	"github.com/enter33/AwesomeBot/pkg/llm"
	"github.com/enter33/AwesomeBot/internal/logging"
)

type LLMMemoryUpdater struct {
	client    openai.Client
	modelConf config.Config
}

func NewLLMMemoryUpdater(modelConf config.Config) *LLMMemoryUpdater {
	return &LLMMemoryUpdater{
		client:    llm.NewOpenAIClient(modelConf),
		modelConf: modelConf,
	}
}

func (u *LLMMemoryUpdater) Enabled() bool {
	return true
}

func (u *LLMMemoryUpdater) ShouldNotify() bool {
	return true
}

// ConditionalMemoryUpdater 根据条件决定是否执行 memory 更新
type ConditionalMemoryUpdater struct {
	updater   MemoryUpdater
	useMemory bool
}

// NewConditionalMemoryUpdater 创建条件更新器
func NewConditionalMemoryUpdater(updater MemoryUpdater, useMemory bool) *ConditionalMemoryUpdater {
	return &ConditionalMemoryUpdater{
		updater:   updater,
		useMemory: useMemory,
	}
}

func (c *ConditionalMemoryUpdater) Enabled() bool {
	return c.useMemory
}

func (c *ConditionalMemoryUpdater) ShouldNotify() bool {
	return c.useMemory
}

func (c *ConditionalMemoryUpdater) Update(ctx context.Context, oldMemory MemoryContent, newMessages []config.OpenAIMessage, onDone UpdateCallback) (MemoryContent, error) {
	if !c.useMemory {
		if onDone != nil {
			onDone(oldMemory, false, nil)
		}
		return oldMemory, nil
	}
	return c.updater.Update(ctx, oldMemory, newMessages, onDone)
}

// ThrottledMemoryUpdater 基于对话轮数的节流更新器
type ThrottledMemoryUpdater struct {
	updater   MemoryUpdater
	threshold int                      // 阈值，达到此轮数才执行更新
	counter   int                      // 当前计数器
	messages   []config.OpenAIMessage   // 累积的消息队列
	running   bool                      // 是否正在执行异步更新
	mu        sync.Mutex                // 保护 running 状态
}

// NewThrottledMemoryUpdater 创建节流更新器
func NewThrottledMemoryUpdater(updater MemoryUpdater, threshold int) *ThrottledMemoryUpdater {
	return &ThrottledMemoryUpdater{
		updater:   updater,
		threshold: threshold,
		counter:   0,
		messages:  nil,
		running:   false,
	}
}

func (t *ThrottledMemoryUpdater) Enabled() bool {
	return t.updater.Enabled()
}

func (t *ThrottledMemoryUpdater) ShouldNotify() bool {
	if !t.updater.Enabled() {
		return false
	}
	t.mu.Lock()
	running := t.running
	t.mu.Unlock()
	if running {
		return false
	}
	return t.counter+1 >= t.threshold
}

func (t *ThrottledMemoryUpdater) Update(ctx context.Context, oldMemory MemoryContent, newMessages []config.OpenAIMessage, onDone UpdateCallback) (MemoryContent, error) {
	if !t.updater.Enabled() {
		if onDone != nil {
			onDone(oldMemory, false, nil)
		}
		return oldMemory, nil
	}

	t.messages = append(t.messages, newMessages...)
	t.counter++

	t.mu.Lock()
	if t.running {
		t.mu.Unlock()
		if onDone != nil {
			onDone(oldMemory, false, nil)
		}
		return oldMemory, nil
	}

	if t.counter >= t.threshold {
		t.running = true
		t.mu.Unlock()

		messagesToUpdate := t.messages
		currentMemory := oldMemory

		go func() {
			t.updater.Update(ctx, currentMemory, messagesToUpdate, func(newMem MemoryContent, _ bool, err error) {
				if onDone != nil {
					onDone(newMem, true, err)
				}

				t.mu.Lock()
				pendingMessages := t.messages
				pendingCount := t.counter

				if len(pendingMessages) > 0 && pendingCount >= t.threshold {
					t.running = true
					nextMessages := pendingMessages
					nextMemory := newMem
					t.mu.Unlock()

					go func() {
						t.mu.Lock()
						t.counter = 0
						t.messages = nil
						t.mu.Unlock()

						t.updater.Update(ctx, nextMemory, nextMessages, func(finalMem MemoryContent, _ bool, finalErr error) {
							t.mu.Lock()
							t.running = false
							t.mu.Unlock()
							if onDone != nil {
								onDone(finalMem, true, finalErr)
							}
						})
					}()
				} else {
					t.counter = 0
					t.messages = nil
					t.running = false
					t.mu.Unlock()
				}
			})
		}()
		return oldMemory, nil
	}

	t.mu.Unlock()

	if onDone != nil {
		onDone(oldMemory, false, nil)
	}
	return oldMemory, nil
}

func (u *LLMMemoryUpdater) Update(ctx context.Context, oldMemory MemoryContent, newMessages []config.OpenAIMessage, onDone UpdateCallback) (MemoryContent, error) {
	if len(newMessages) == 0 {
		if onDone != nil {
			onDone(oldMemory, true, nil)
		}
		return oldMemory, nil
	}

	var b strings.Builder
	for _, msg := range newMessages {

		contentAny := msg.GetContent().AsAny()
		contentStr, ok := contentAny.(*string)
		if !ok {
			continue
		}

		b.WriteString(config.GetRoleName(msg))
		b.WriteString(": ")
		b.WriteString(*contentStr)
		b.WriteString("\n")
	}

	prompt := updateMemoryPrompt
	prompt = strings.ReplaceAll(prompt, "{current_memory}", oldMemory.String())
	prompt = strings.ReplaceAll(prompt, "{new_messages}", b.String())

	logging.Info("========== 发送给 LLM 进行提取记忆 的 Messages ==========")
	logging.Info("Message: %s", string(prompt))

	request := openai.ChatCompletionNewParams{
		Model: u.modelConf.Model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage(prompt),
		},
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := u.client.Chat.Completions.New(timeoutCtx, request)
	if err != nil {
		log.Printf("failed to update memory through llm: %v", err)
		if onDone != nil {
			onDone(oldMemory, true, err)
		}
		return oldMemory, err
	}
	if len(resp.Choices) == 0 {
		log.Printf("no choices returned, resp: %s", resp.RawJSON())
		if onDone != nil {
			onDone(oldMemory, true, nil)
		}
		return oldMemory, nil
	}

	respContent := resp.Choices[0].Message.Content
	newMemory := MemoryContent{}
	newMemory.GlobalMemory = extractXMLTag(respContent, "global")
	newMemory.WorkspaceMemory = extractXMLTag(respContent, "workspace")

	if onDone != nil {
		onDone(newMemory, true, nil)
	}

	return newMemory, nil
}

// extractXMLTag 使用正则表达式从文本中提取 XML 标签的内容
func extractXMLTag(content, tagName string) string {
	// 匹配 <tagName>...</tagName>，支持多行内容
	pattern := regexp.MustCompile(`<` + regexp.QuoteMeta(tagName) + `>([\s\S]*?)</` + regexp.QuoteMeta(tagName) + `>`)
	matches := pattern.FindStringSubmatch(content)
	if len(matches) < 2 {
		return ""
	}
	return strings.TrimSpace(matches[1])
}

const updateMemoryPrompt = `You are a memory management system for an AI coding assistant. Your task is to analyze conversation messages and update two levels of memory.

## Current Memory
{current_memory}

## New Messages to Process
{new_messages}

## Instructions

Analyze the new messages and update the two memory levels accordingly. Each memory level should be formatted in Markdown.

### Global Memory (User-level)
- User preferences, coding style, frequently used tools/libraries
- Long-term patterns observed across conversations
- User's background, expertise level, recurring needs

### Workspace Memory (Project-level)
- Project structure, architecture, key files
- Build commands, test commands, deployment processes
- Project-specific conventions, tech stack
- Issues encountered and their solutions

## Output Format

Return the updated memories using XML tags. Each memory content should be a valid Markdown string:

<global>
<updated global memory in Markdown format>
</global>

<workspace>
<updated workspace memory in Markdown format>
</workspace>

## Guidelines

1. Use Markdown formatting:
   - Use ## for section headings within a memory level
   - Use - for bullet points
   - Use **bold** for emphasis
   - Use backticks for code, commands, and file names

2. Content principles:
   - Be concise but informative
   - Only update memory levels affected by new messages
   - Preserve existing important information
   - Remove outdated or superseded information

3. If a memory level doesn't need updates, return it unchanged

## Example

Input messages:
- User: I prefer using vim for editing and always run tests with verbose flag
- User: Can you help me set up a Go project?
- Assistant: Created go.mod and main.go files. Used module name "example.com/myapp"

Output:
<global>
## User Preferences
- **Editor**: vim
- **Testing**: Always use verbose flag
</global>

<workspace>
## Project Structure
- go.mod - module: example.com/myapp
- main.go - application entry point
</workspace>

Now process the messages and return the updated memory using XML tags with Markdown-formatted content.
`
