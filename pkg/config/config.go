package config

import (
	"encoding/json"
	"os"

	"github.com/openai/openai-go/v3"
)

// OpenAIMessage OpenAI 消息类型别名
type OpenAIMessage = openai.ChatCompletionMessageParamUnion

// GetRoleName 获取消息角色名称
func GetRoleName(message OpenAIMessage) string {
	if message.OfSystem != nil {
		return "system"
	}
	if message.OfUser != nil {
		return "user"
	}
	if message.OfAssistant != nil {
		return "assistant"
	}
	if message.OfTool != nil {
		return "tool"
	}
	if message.OfDeveloper != nil {
		return "developer"
	}
	if message.OfFunction != nil {
		return "function"
	}
	return "unknown"
}

// Ptr 返回指针
func Ptr[T any](v T) *T {
	return &v
}

// Config LLM 配置
type Config struct {
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
	ApiKey  string `json:"api_key"`
}

// ContextWindow 硬编码的上下文窗口大小
const ContextWindow = 200000

// IsValid 检查配置是否有效
func (c *Config) IsValid() bool {
	return c.BaseURL != "" && c.Model != "" && c.ApiKey != ""
}

// LoadConfig 加载配置文件
func LoadConfig(path string) (Config, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	err = json.Unmarshal(content, &cfg)
	if err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// SaveConfig 保存配置文件
func SaveConfig(path string, cfg Config) error {
	content, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, content, 0644)
}

// EnsureConfigFile 确保配置文件存在
func EnsureConfigFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	// 创建空配置文件
	emptyCfg := Config{}
	content, err := json.MarshalIndent(emptyCfg, "", "  ")
	if err != nil {
		return err
	}
	// 确保目录存在
	dir := GetAwesomeDir()
	if dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, content, 0644)
}
