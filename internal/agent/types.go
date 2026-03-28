package agent

import "github.com/enter33/AwesomeBot/internal/msgs"

// 消息类型常量
const (
	MessageTypeReasoning   = "reasoning"
	MessageTypeContent     = "content"
	MessageTypeToolCall    = "tool_call"
	MessageTypeError       = "error"
	MessageTypePolicy      = "policy"
	MessageTypeMemory      = "memory"
	MessageTypeToolConfirm = "tool_confirm"
	MessageTypeTokenUsage  = "token_usage"
)

// ConfirmationAction 确认动作 (re-export from msgs for backward compatibility)
type ConfirmationAction = msgs.ConfirmationAction

const (
	ConfirmAllow ConfirmationAction = msgs.ConfirmAllow
	ConfirmReject ConfirmationAction = msgs.ConfirmReject
	ConfirmAlwaysAllow ConfirmationAction = msgs.ConfirmAlwaysAllow
)

// MessageVO 用于流式展示当前模型流式输出或者状态
type MessageVO = msgs.MessageVO

// PolicyVO 策略执行状态
type PolicyVO = msgs.PolicyVO

// MemoryVO 记忆更新状态
type MemoryVO = msgs.MemoryVO

// ToolCallVO 工具调用信息
type ToolCallVO = msgs.ToolCallVO

// ToolConfirmationVO 工具确认请求
type ToolConfirmationVO = msgs.ToolConfirmationVO

// TokenUsageVO Token 用量信息
type TokenUsageVO = msgs.TokenUsageVO
