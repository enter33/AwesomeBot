package agent

import (
	"context"
	"encoding/json"
	"time"

	"github.com/openai/openai-go/v3"

	ctxengine "github.com/awesome/awesomebot/internal/context"
	"github.com/awesome/awesomebot/internal/logging"
	"github.com/awesome/awesomebot/internal/mcp"
	"github.com/awesome/awesomebot/internal/tool"
	"github.com/awesome/awesomebot/pkg/config"
)

type ToolConfirmConfig struct {
	RequireConfirmTools map[tool.AgentTool]bool
}

// Agent Agent 实例
type Agent struct {
	confirmConfig    ToolConfirmConfig
	alwaysAllowTools map[tool.AgentTool]bool
	model           string
	client          openai.Client
	contextEngine   *ctxengine.Engine
	nativeTools     map[tool.AgentTool]tool.Tool
	mcpClients      map[string]*mcp.Client
}

// NewAgent 创建 Agent 实例
func NewAgent(
	modelConf config.Config,
	systemPrompt string,
	confirmConfig ToolConfirmConfig,
	tools []tool.Tool,
	mcpClients []*mcp.Client,
	contextEngine *ctxengine.Engine,
	llmClient openai.Client,
) *Agent {
	a := Agent{
		confirmConfig:    confirmConfig,
		alwaysAllowTools: make(map[tool.AgentTool]bool),
		model:            modelConf.Model,
		client:           llmClient,
		contextEngine:    contextEngine,
		nativeTools:      make(map[tool.AgentTool]tool.Tool),
		mcpClients:       make(map[string]*mcp.Client),
	}

	a.contextEngine.Init(systemPrompt, ctxengine.TokenBudget{ContextWindow: config.ContextWindow})

	for _, t := range tools {
		a.nativeTools[t.ToolName()] = t
	}
	for _, mcpClient := range mcpClients {
		a.mcpClients[mcpClient.Name()] = mcpClient
	}

	return &a
}

func (a *Agent) findTool(toolName string) (tool.Tool, bool) {
	t, ok := a.nativeTools[toolName]
	if ok {
		return t, true
	}
	for _, mcpClient := range a.mcpClients {
		for _, t := range mcpClient.GetTools() {
			if t.ToolName() == toolName {
				return t, true
			}
		}
	}
	return nil, false
}

func (a *Agent) buildTools() []openai.ChatCompletionToolUnionParam {
	tools := make([]openai.ChatCompletionToolUnionParam, 0)
	// 集成 native tools
	for _, t := range a.nativeTools {
		tools = append(tools, t.Info())
	}
	// 集成 mcp tools
	for _, mcpClient := range a.mcpClients {
		for _, t := range mcpClient.GetTools() {
			tools = append(tools, t.Info())
		}
	}
	return tools
}

// ResetSession 重置会话
func (a *Agent) ResetSession() {
	a.contextEngine.Reset()
}

// RunStreaming 流式运行
func (a *Agent) RunStreaming(ctx context.Context, query string, viewCh chan MessageVO, confirmCh chan ConfirmationAction) error {
	startTime := time.Now()

	// 记录用户输入
	logging.Info("========== 用户输入 ==========")
	logging.Info("Query: %s", query)

	a.contextEngine.SetPolicyEventHook(func(policyName string, running bool, err error) {
		viewCh <- MessageVO{
			Type: MessageTypePolicy,
			Policy: &PolicyVO{
				Name:    policyName,
				Running: running,
				Error:   err,
			},
		}
	})
	a.contextEngine.SetMemoryEventHook(func(running bool, err error) {
		viewCh <- MessageVO{
			Type: MessageTypeMemory,
			Memory: &MemoryVO{
				Running: running,
				Error:   err,
			},
		}
	})
	defer a.contextEngine.SetPolicyEventHook(nil)
	defer a.contextEngine.SetMemoryEventHook(nil)

	draft := a.contextEngine.StartTurn(openai.UserMessage(query))
	defer a.contextEngine.AbortTurn(draft)

	// 为本轮次创建新的消息链。草稿消息在 commit 前不会污染上下文。
	messages := a.contextEngine.BuildRequestMessages()
	messages = append(messages, draft.NewMessages...)

	// 记录发送给 LLM 的 messages
	logging.Info("========== 发送给 LLM 的 Messages ==========")
	for i, msg := range messages {
		msgJSON, _ := json.Marshal(msg)
		logging.Debug("Message[%d]: %s", i, string(msgJSON))
	}

	var usage openai.CompletionUsage
	var totalTokens int

	for {
		params := openai.ChatCompletionNewParams{
			Model:    a.model,
			Messages: messages,
			Tools:    a.buildTools(),
		}

		logging.Info("========== 调用 LLM ==========")
		logging.Info("Model: %s", a.model)
		stream := a.client.Chat.Completions.NewStreaming(ctx, params)
		acc := openai.ChatCompletionAccumulator{}
		for stream.Next() {
			chunk := stream.Current()
			acc.AddChunk(chunk)

			if len(chunk.Choices) > 0 {
				deltaRaw := chunk.Choices[0].Delta
				// 推理模型会返回 reasoning_content（有些模型使用 reasoning 字段）
				delta := deltaWithReasoning{}
				_ = json.Unmarshal([]byte(deltaRaw.RawJSON()), &delta)
				if reasoningContent := delta.ReasoningContent; reasoningContent != "" {
					viewCh <- MessageVO{
						Type:             MessageTypeReasoning,
						ReasoningContent: &reasoningContent,
					}
				}
				if delta.Content != "" {
					viewCh <- MessageVO{
						Type:    MessageTypeContent,
						Content: &chunk.Choices[0].Delta.Content,
					}
				}
			}
		}
		if err := stream.Err(); err != nil {
			logging.Error("LLM 流式响应错误: %v", err)
			viewCh <- MessageVO{
				Type:    MessageTypeError,
				Content: config.Ptr(err.Error()),
			}
			return err
		}
		if len(acc.Choices) == 0 {
			logging.Warn("LLM 返回为空")
			return nil
		}
		// 累加 token 用量（每次 LLM 调用后累加）
		usage.PromptTokens += acc.Usage.PromptTokens
		usage.CompletionTokens += acc.Usage.CompletionTokens
		usage.TotalTokens += acc.Usage.TotalTokens
		totalTokens = int(usage.TotalTokens)
		message := acc.Choices[0].Message

		// 记录 LLM 返回
		logging.Info("========== LLM 返回 ==========")
		logging.Info("Content: %s", message.Content)
		if len(message.ToolCalls) > 0 {
			logging.Info("ToolCalls: %d", len(message.ToolCalls))
		}
		logging.Info("Usage - PromptTokens: %d, CompletionTokens: %d, TotalTokens: %d",
			usage.PromptTokens, usage.CompletionTokens, totalTokens)

		// 拼接 assistant message 到整体消息链中
		assistantMsg := message.ToParam()
		messages = append(messages, assistantMsg)
		draft.NewMessages = append(draft.NewMessages, assistantMsg)

		// tool loop 结束，可以返回结果
		if len(message.ToolCalls) == 0 {
			break
		}

		for _, toolCall := range message.ToolCalls {
			// 记录工具调用
			logging.Info("========== 工具调用 ==========")
			logging.Info("Tool: %s", toolCall.Function.Name)
			logging.Info("Arguments: %s", toolCall.Function.Arguments)

			t, ok := a.findTool(toolCall.Function.Name)
			if !ok {
				logging.Error("工具未找到: %s", toolCall.Function.Name)
				viewCh <- MessageVO{
					Type:    MessageTypeError,
					Content: config.Ptr("tool not found"),
				}
				toolMsg := openai.ToolMessage("tool not found", toolCall.ID)
				messages = append(messages, toolMsg)
				draft.NewMessages = append(draft.NewMessages, toolMsg)
				continue
			}

			toolName := t.ToolName()
			needConfirm := a.confirmConfig.RequireConfirmTools[toolName] && !a.alwaysAllowTools[toolName]

			if needConfirm {
				confirmReq := ToolConfirmationVO{
					ToolName:  toolCall.Function.Name,
					Arguments: toolCall.Function.Arguments,
				}
				viewCh <- MessageVO{
					Type:                    MessageTypeToolConfirm,
					ToolConfirmationRequest: &confirmReq,
				}

				select {
				case <-ctx.Done():
					return nil
				case action := <-confirmCh:
					switch action {
					case ConfirmReject:
						logging.Info("用户拒绝工具调用: %s", toolCall.Function.Name)
						toolMsg := openai.ToolMessage("user rejected tool call", toolCall.ID)
						messages = append(messages, toolMsg)
						draft.NewMessages = append(draft.NewMessages, toolMsg)
						continue
					case ConfirmAlwaysAllow:
						logging.Info("用户选择始终允许工具: %s", toolCall.Function.Name)
						a.alwaysAllowTools[toolName] = true
					case ConfirmAllow:
						logging.Info("用户允许工具调用: %s", toolCall.Function.Name)
					}
				}
			}

			toolResult, err := t.Execute(ctx, toolCall.Function.Arguments)

			// 记录工具执行结果
			if err != nil {
				logging.Error("工具执行错误: %s, Error: %v", toolCall.Function.Name, err)
				// 如果 toolResult 为空才用错误信息覆盖
				if toolResult == "" {
					toolResult = err.Error()
				}
				viewCh <- MessageVO{
					Type:    MessageTypeError,
					Content: &toolResult,
				}
			} else {
				logging.Info("工具执行成功: %s", toolCall.Function.Name)
				logging.Debug("工具返回内容: %s", truncateString(toolResult, 500))
			}

			viewCh <- MessageVO{
				Type: MessageTypeToolCall,
				ToolCall: &ToolCallVO{
					Name:      toolCall.Function.Name,
					Arguments: toolCall.Function.Arguments,
				},
			}

			toolMsg := openai.ToolMessage(toolResult, toolCall.ID)
			messages = append(messages, toolMsg)
			draft.NewMessages = append(draft.NewMessages, toolMsg)
		}

		// 每次循环后检查 context 是否被取消（用户按 ESC）
		select {
		case <-ctx.Done():
			logging.Info("用户取消操作 (ESC)")
			// 用户按 ESC，保存消息但不执行 policies/memory
			_ = a.contextEngine.CommitTurn(ctx, draft, ctxengine.Usage{PromptTokens: int(usage.TotalTokens)}, true)
			return nil
		default:
		}

	}

	err := a.contextEngine.CommitTurn(ctx, draft, ctxengine.Usage{PromptTokens: int(usage.TotalTokens)}, false)

	// 计算速度并发送 token 用量
	duration := time.Since(startTime).Seconds()
	speed := 0.0
	if duration > 0 {
		speed = float64(totalTokens) / duration
	}

	logging.Info("========== Token 用量统计 ==========")
	logging.Info("PromptTokens: %d, CompletionTokens: %d, TotalTokens: %d, Speed: %.2f tok/s, Duration: %.2fs",
		usage.PromptTokens, usage.CompletionTokens, totalTokens, speed, duration)

	viewCh <- MessageVO{
		Type: MessageTypeTokenUsage,
		TokenUsage: &TokenUsageVO{
			PromptTokens:     int(usage.PromptTokens),
			CompletionTokens: int(usage.CompletionTokens),
			TotalTokens:      totalTokens,
			Speed:            speed,
			Duration:         duration,
		},
	}

	return err
}

type deltaWithReasoning struct {
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content"`
}

// truncateString 截断字符串
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
