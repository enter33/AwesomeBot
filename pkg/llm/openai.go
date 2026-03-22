package llm

import (
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	"github.com/awesome/awesomebot/pkg/config"
)

// NewOpenAIClient 创建 OpenAI 客户端
func NewOpenAIClient(modelConf config.Config) openai.Client {
	client := openai.NewClient(
		option.WithBaseURL(modelConf.BaseURL),
		option.WithAPIKey(modelConf.ApiKey),
		option.WithHeader("X-Title", "AwesomeBot"),
		option.WithHeader("HTTP-Referer", "https://github.com/awesome/awesomebot"),
	)
	return client
}
