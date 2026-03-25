package agent

import (
	"context"
	"encoding/json"
	// "math/rand"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	ctxengine "github.com/enter33/AwesomeBot/internal/context"
	"github.com/enter33/AwesomeBot/internal/logging"
	"github.com/enter33/AwesomeBot/internal/mcp"
	"github.com/enter33/AwesomeBot/internal/tool"
	"github.com/enter33/AwesomeBot/pkg/config"
)

// LLM 重试配置
const maxLLMRetries = 3

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
	contextWindow int,
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

	a.contextEngine.Init(systemPrompt, ctxengine.TokenBudget{ContextWindow: contextWindow})

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

	attempt := 0
	for attempt <= maxLLMRetries {
		params := openai.ChatCompletionNewParams{
			Model:    a.model,
			Messages: messages,
			Tools:    a.buildTools(),
			StreamOptions: openai.ChatCompletionStreamOptionsParam{
				IncludeUsage: openai.Bool(true),
			},
		}

		logging.Info("========== 调用 LLM ==========")
		logging.Info("Model: %s", a.model)
		stream := a.client.Chat.Completions.NewStreaming(ctx, params,
			option.WithJSONSet("reasoning_split", true),
			option.WithJSONSet("stream_options", map[string]any{
				"include_usage": true,
				"include_reasoning": true,
			}),
		)
		acc := openai.ChatCompletionAccumulator{}
		for stream.Next() {
			chunk := stream.Current()
			acc.AddChunk(chunk)

			// 检查 chunk 是否包含 usage 信息（当 StreamOptions.IncludeUsage=true 时，最后一个 chunk 包含 usage）
			if chunk.Usage.TotalTokens > 0 {
				usage = chunk.Usage
			}

			if len(chunk.Choices) > 0 {
				deltaRaw := chunk.Choices[0].Delta
				// MiniMax 返回的 reasoning_details 是一个数组，需要单独解析
				var rawDelta map[string]any
				if err := json.Unmarshal([]byte(deltaRaw.RawJSON()), &rawDelta); err == nil {
					// 处理 reasoning_details 数组（MiniMax 格式）
					if reasoningDetails, ok := rawDelta["reasoning_details"].([]any); ok {
						for _, detail := range reasoningDetails {
							if detailMap, ok := detail.(map[string]any); ok {
								if text, ok := detailMap["text"].(string); ok && text != "" {
									viewCh <- MessageVO{
										Type:             MessageTypeReasoning,
										ReasoningContent: &text,
									}
								}
							}
						}
					}
				}
				// 解析 content 和 reasoning_content（标准 OpenAI 格式）
				delta := deltaWithReasoning{}
				_ = json.Unmarshal([]byte(deltaRaw.RawJSON()), &delta)
				// 推理内容
				if delta.ReasoningContent != "" {
					viewCh <- MessageVO{
						Type:             MessageTypeReasoning,
						ReasoningContent: &delta.ReasoningContent,
					}
				}
				// 回答内容（content 直接输出，包括 <think>...</think> 标签作为普通文本）
				if delta.Content != "" {
					viewCh <- MessageVO{
						Type:    MessageTypeContent,
						Content: &delta.Content,
					}
				}
			}
		}
		if err := stream.Err(); err != nil {
			logging.Error("LLM 流式响应错误: %v", err)
			attempt ++
			continue
		}
		if len(acc.Choices) == 0 {
			logging.Warn("LLM 返回为空")
			attempt ++
			continue
		}
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
	if attempt > maxLLMRetries {
		viewCh <- MessageVO{
			Type:    MessageTypeError,
			Content: config.Ptr("达到最大重试次数，任务未能完成"),
		}
		logging.Error("达到最大重试次数 %d，LLM 任务未能完成", maxLLMRetries)
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
