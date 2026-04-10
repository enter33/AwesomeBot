package context

import (
	"log"

	"github.com/tiktoken-go/tokenizer"

	"github.com/enter33/AwesomeBot/pkg/config"
)

var tokenEnc tokenizer.Codec

func init() {
	var err error
	tokenEnc, err = tokenizer.Get(tokenizer.Cl100kBase)
	if err != nil {
		log.Fatal(err)
	}
}

// CountTokens 计算消息的 token 数量
func CountTokens(message config.OpenAIMessage) int {
	var total int

	// 消息基础 overhead（约 4 tokens per message）
	total += 4

	// 根据消息类型分别计算
	if message.OfSystem != nil {
		// System 消息：计算 content
		contentAny := message.GetContent().AsAny()
		if contentStr, ok := contentAny.(*string); ok && contentStr != nil {
			count, _ := tokenEnc.Count(*contentStr)
			total += count
		}
	} else if message.OfUser != nil {
		// User 消息：计算 content
		contentAny := message.GetContent().AsAny()
		if contentStr, ok := contentAny.(*string); ok && contentStr != nil {
			count, _ := tokenEnc.Count(*contentStr)
			total += count
		}
	} else if message.OfAssistant != nil {
		// Assistant 消息：计算 content + tool_calls
		contentAny := message.GetContent().AsAny()
		if contentStr, ok := contentAny.(*string); ok && contentStr != nil {
			count, _ := tokenEnc.Count(*contentStr)
			total += count
		}
		// 计算 tool_calls 的 function.name 和 function.arguments
		for _, tc := range message.OfAssistant.ToolCalls {
			fn := tc.GetFunction()
			if fn != nil && fn.Name != "" {
				count, _ := tokenEnc.Count(fn.Name)
				total += count
			}
			if fn != nil && fn.Arguments != "" {
				count, _ := tokenEnc.Count(fn.Arguments)
				total += count
			}
		}
	} else if message.OfTool != nil {
		// Tool 消息：计算 tool_call_id + content
		if message.OfTool.ToolCallID != "" {
			count, _ := tokenEnc.Count(message.OfTool.ToolCallID)
			total += count
		}
		contentAny := message.GetContent().AsAny()
		if contentStr, ok := contentAny.(*string); ok && contentStr != nil {
			count, _ := tokenEnc.Count(*contentStr)
			total += count
		}
	} else if message.OfDeveloper != nil {
		// Developer 消息：计算 content
		contentAny := message.GetContent().AsAny()
		if contentStr, ok := contentAny.(*string); ok && contentStr != nil {
			count, _ := tokenEnc.Count(*contentStr)
			total += count
		}
	} else if message.OfFunction != nil {
		// Function 消息：计算 content
		contentAny := message.GetContent().AsAny()
		if contentStr, ok := contentAny.(*string); ok && contentStr != nil {
			count, _ := tokenEnc.Count(*contentStr)
			total += count
		}
	}

	return total
}
