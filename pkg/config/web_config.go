package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// WebSearchConfig Web 搜索工具配置
type WebSearchConfig struct {
	Provider  string `json:"provider"`  // duckduckgo, tavily, searxng, jina, brave
	MaxResults int    `json:"max_results"` // 最大结果数，默认 5
	ApiKey    string `json:"api_key"`    // API Key
	BaseURL   string `json:"base_url"`   // SearXNG 等自托管服务地址
}

// WebFetchConfig Web 抓取工具配置
type WebFetchConfig struct {
	MaxChars  int    `json:"max_chars"`  // 最大字符数，默认 50000
	JinaAPIKey string `json:"jina_api_key"` // Jina Reader API Key
	Proxy     string `json:"proxy"`      // 代理地址
}

// GetWebSearchConfigPath 获取 Web 搜索配置路径
func GetWebSearchConfigPath() string {
	return filepath.Join(GetAwesomeDir(), "web_search.json")
}

// GetWebFetchConfigPath 获取 Web 抓取配置路径
func GetWebFetchConfigPath() string {
	return filepath.Join(GetAwesomeDir(), "web_fetch.json")
}

// EnsureWebSearchConfigFile 确保 Web 搜索配置文件存在
func EnsureWebSearchConfigFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := EnsureAwesomeDir(); err != nil {
		return err
	}
	// 不设置默认 provider，强制用户配置
	defaultCfg := WebSearchConfig{
		MaxResults: 5,
	}
	content, err := json.MarshalIndent(defaultCfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, content, 0644)
}

// EnsureWebFetchConfigFile 确保 Web 抓取配置文件存在
func EnsureWebFetchConfigFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := EnsureAwesomeDir(); err != nil {
		return err
	}
	defaultCfg := WebFetchConfig{
		MaxChars: 50000,
	}
	content, err := json.MarshalIndent(defaultCfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, content, 0644)
}

// LoadWebSearchConfig 加载 Web 搜索配置
func LoadWebSearchConfig(path string) (WebSearchConfig, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return WebSearchConfig{}, err
	}
	var cfg WebSearchConfig
	if err := json.Unmarshal(content, &cfg); err != nil {
		// 解析失败返回空配置，让工具返回配置错误
		return WebSearchConfig{}, nil
	}
	if cfg.MaxResults <= 0 {
		cfg.MaxResults = 5
	}
	return cfg, nil
}

// LoadWebFetchConfig 加载 Web 抓取配置
func LoadWebFetchConfig(path string) (WebFetchConfig, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return WebFetchConfig{}, err
	}
	var cfg WebFetchConfig
	if err := json.Unmarshal(content, &cfg); err != nil {
		return WebFetchConfig{MaxChars: 50000}, nil
	}
	if cfg.MaxChars <= 0 {
		cfg.MaxChars = 50000
	}
	return cfg, nil
}
