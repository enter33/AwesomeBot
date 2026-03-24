package llm

import (
	"net/http"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	"github.com/enter33/AwesomeBot/pkg/config"
)

// NewOpenAIClient 创建 OpenAI 客户端
func NewOpenAIClient(modelConf config.Config) openai.Client {
	// 确定超时时间
	timeout := time.Duration(config.DefaultLLMTimeout) * time.Second
	if modelConf.Timeout > 0 {
		timeout = time.Duration(modelConf.Timeout) * time.Second
	}

	httpClient := &http.Client{
		Timeout: timeout,
	}

	client := openai.NewClient(
		option.WithBaseURL(modelConf.BaseURL),
		option.WithAPIKey(modelConf.ApiKey),
		option.WithHTTPClient(httpClient),
		option.WithHeader("X-Title", "AwesomeBot"),
		option.WithHeader("HTTP-Referer", "https://github.com/enter33/AwesomeBot"),
	)
	return client
}
