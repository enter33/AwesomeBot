package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// McpServerConfig MCP 服务器配置
type McpServerConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Url     string            `json:"url,omitempty"`
}

// IsStdio 检查是否为 stdio 传输
func (m *McpServerConfig) IsStdio() bool {
	return m.Command != ""
}

// ReplacePlaceholders 替换占位符
func (m *McpServerConfig) ReplacePlaceholders(vars map[string]string) McpServerConfig {
	result := *m
	if result.Args != nil {
		newArgs := make([]string, len(result.Args))
		for i, arg := range result.Args {
			for k, v := range vars {
				arg = replaceAll(arg, k, v)
			}
			newArgs[i] = arg
		}
		result.Args = newArgs
	}
	return result
}

func replaceAll(s, old, new string) string {
	for {
		idx := indexOf(s, old)
		if idx == -1 {
			break
		}
		s = s[:idx] + new + s[idx+len(old):]
	}
	return s
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// GetAwesomeDir 获取 awesomebot 配置目录
func GetAwesomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".awesome")
}

// EnsureAwesomeDir 确保 awesomebot 配置目录存在
func EnsureAwesomeDir() error {
	dir := GetAwesomeDir()
	if dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0755)
}

// GetConfigPath 获取配置文件路径
func GetConfigPath() string {
	return filepath.Join(GetAwesomeDir(), "config.json")
}

// GetMcpConfigPath 获取 MCP 配置文件路径
func GetMcpConfigPath() string {
	return filepath.Join(GetAwesomeDir(), "mcp.json")
}

// EnsureMcpConfigFile 确保 MCP 配置文件存在
func EnsureMcpConfigFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	// 确保目录存在
	if err := EnsureAwesomeDir(); err != nil {
		return err
	}
	// 创建空的 MCP 配置文件
	emptyCfg := map[string]McpServerConfig{}
	content, err := json.MarshalIndent(emptyCfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, content, 0644)
}

// LoadMcpServerConfig 加载 MCP 服务器配置
func LoadMcpServerConfig(path string) (map[string]McpServerConfig, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var config map[string]McpServerConfig
	err = json.Unmarshal(content, &config)
	if err != nil {
		return nil, err
	}
	return config, nil
}

// GetWorkspaceDir 获取当前工作目录
func GetWorkspaceDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	return dir
}

// AwesomeConfig awesomebot 全局配置
type AwesomeConfig struct {
	UseMemory bool `json:"use_memory"`
}

// GetAwesomeConfigPath 获取 awesome 配置文件的路径
func GetAwesomeConfigPath() string {
	return filepath.Join(GetAwesomeDir(), "awesome.json")
}

// EnsureAwesomeConfigFile 确保 awesome 配置文件存在（如果不存在则创建默认配置）
func EnsureAwesomeConfigFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	// 确保目录存在
	if err := EnsureAwesomeDir(); err != nil {
		return err
	}
	// 创建默认配置文件，useMemory 默认为 true
	defaultCfg := AwesomeConfig{UseMemory: true}
	content, err := json.MarshalIndent(defaultCfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, content, 0644)
}

// LoadAwesomeConfig 加载 awesome 配置
func LoadAwesomeConfig(path string) (AwesomeConfig, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return AwesomeConfig{}, err
	}
	var cfg AwesomeConfig
	// 如果解析失败，返回默认配置（useMemory 为 true）
	if err := json.Unmarshal(content, &cfg); err != nil {
		return AwesomeConfig{UseMemory: true}, nil
	}
	return cfg, nil
}
