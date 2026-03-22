package memory

import (
	"context"
	"log"
	"regexp"
	"strings"

	"github.com/openai/openai-go/v3"

	"github.com/enter33/AwesomeBot/pkg/config"
	"github.com/enter33/AwesomeBot/pkg/llm"
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

func (c *ConditionalMemoryUpdater) Update(ctx context.Context, oldMemory MemoryContent, newMessages []config.OpenAIMessage) (MemoryContent, error) {
	// 如果 useMemory 为 false，不执行更新，直接返回原 memory
	if !c.useMemory {
		return oldMemory, nil
	}
	return c.updater.Update(ctx, oldMemory, newMessages)
}

func (u *LLMMemoryUpdater) Update(ctx context.Context, oldMemory MemoryContent, newMessages []config.OpenAIMessage) (MemoryContent, error) {
	if len(newMessages) == 0 {
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

	request := openai.ChatCompletionNewParams{
		Model: u.modelConf.Model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage(prompt),
		},
	}

	resp, err := u.client.Chat.Completions.New(ctx, request)
	if err != nil {
		log.Printf("failed to update memory through llm: %v", err)
		return oldMemory, err
	}
	if len(resp.Choices) == 0 {
		log.Printf("no choices returned, resp: %s", resp.RawJSON())
		return oldMemory, nil
	}

	respContent := resp.Choices[0].Message.Content
	newMemory := MemoryContent{}
	newMemory.GlobalMemory = extractXMLTag(respContent, "global")
	newMemory.WorkspaceMemory = extractXMLTag(respContent, "workspace")

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
