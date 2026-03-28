package msgs

// ConfirmationAction 确认动作
type ConfirmationAction int

const (
	ConfirmAllow ConfirmationAction = iota
	ConfirmReject
	ConfirmAlwaysAllow
)

// MessageVO 用于流式展示当前模型流式输出或者状态
type MessageVO struct {
	Type string `json:"type"`

	ReasoningContent        *string              `json:"reasoning_content,omitempty"`
	Content                *string              `json:"content,omitempty"`
	ToolCall               *ToolCallVO          `json:"tool,omitempty"`
	Policy                 *PolicyVO            `json:"policy,omitempty"`
	Memory                 *MemoryVO            `json:"memory,omitempty"`
	ToolConfirmationRequest *ToolConfirmationVO `json:"tool_confirmation_request,omitempty"`
	TokenUsage             *TokenUsageVO        `json:"token_usage,omitempty"`
}

// PolicyVO 策略执行状态
type PolicyVO struct {
	Name    string `json:"name"`
	Running bool   `json:"running"`
	Error   error  `json:"error"`
}

// MemoryVO 记忆更新状态
type MemoryVO struct {
	Running bool  `json:"running"`
	Error   error `json:"error"`
}

// ToolCallVO 工具调用信息
type ToolCallVO struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolConfirmationVO 工具确认请求
type ToolConfirmationVO struct {
	ToolName  string `json:"tool_name"`
	Arguments string `json:"arguments"`
}

// TokenUsageVO Token 用量信息
type TokenUsageVO struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	Speed            float64 `json:"speed"`    // tokens/秒
	Duration         float64 `json:"duration"` // 总耗时（秒）
}
