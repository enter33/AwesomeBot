package llm

import (
	"context"

	"github.com/openai/openai-go/v3"
)

// Client LLM 客户端接口
type Client interface {
	// Chat 返回聊天补全流
	Chat(ctx context.Context) *openai.ChatCompletionNewParams
}

// StreamHandler 流式响应处理接口
type StreamHandler interface {
	// OnContent 处理内容块
	OnContent(content string)
	// OnReasoning 处理推理内容
	OnReasoning(reasoning string)
	// OnError 处理错误
	OnError(err error)
	// OnComplete 完成
	OnComplete(usage openai.CompletionUsage)
}
