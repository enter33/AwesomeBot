package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"

	"github.com/enter33/AwesomeBot/internal/tool"
	"github.com/enter33/AwesomeBot/pkg/config"
)

// 连接状态
type ConnStatus int

const (
	StatusDisconnected ConnStatus = iota
	StatusConnecting
	StatusConnected
	StatusError
)

const (
	// 默认 ping 存活最大时间
	DefaultMaxPingAge = 5 * time.Minute
	// 默认工具调用超时
	DefaultToolTimeout = 120 * time.Second
)

type Client struct {
	name         string
	client       *mcp.Client
	serverConfig config.McpServerConfig

	session *mcp.ClientSession
	tools   []tool.Tool

	mu         sync.RWMutex
	status     ConnStatus
	lastPing   time.Time
	maxPingAge time.Duration

	stopCh chan struct{}
}

// NewClient 创建 MCP 客户端
func NewClient(name string, server config.McpServerConfig) *Client {
	return &Client{
		name: name,
		client: mcp.NewClient(&mcp.Implementation{
			Name:    "awesomebot-mcp-client",
			Title:   "AwesomeBot",
			Version: "v1.0.0",
		}, nil),
		serverConfig: server.ReplacePlaceholders(initRunningVars()),
		tools:        make([]tool.Tool, 0),
		status:       StatusDisconnected,
		maxPingAge:   DefaultMaxPingAge,
	}
}

// Status 获取连接状态
func (e *Client) Status() ConnStatus {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.status
}

func initRunningVars() map[string]string {
	return map[string]string{
		"${workspaceFolder}": config.GetWorkspaceDir(),
	}
}

func (e *Client) Name() string {
	return e.name
}

func (e *Client) connect(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// 如果已有 session，先检测是否有效
	if e.session != nil {
		// 带超时的 ping context，避免在断开的连接上阻塞太久
		pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		if e.session.Ping(pingCtx, &mcp.PingParams{}) == nil {
			// 连接有效，更新 lastPing（如果距离上次太久了）
			if time.Since(e.lastPing) >= e.maxPingAge {
				e.lastPing = time.Now()
			}
			e.status = StatusConnected
			return nil
		}
		// 连接无效，关闭旧 session
		e.closeSession()
	}

	// 建立新连接
	e.status = StatusConnecting
	var err error
	if e.serverConfig.IsStdio() {
		cmd := exec.Command(e.serverConfig.Command, e.serverConfig.Args...)
		for k, v := range e.serverConfig.Env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
		e.session, err = e.client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	} else {
		e.session, err = e.client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: e.serverConfig.Url}, nil)
	}
	if err != nil {
		e.status = StatusError
		return fmt.Errorf("failed to connect to MCP server %s: %w", e.name, err)
	}

	// 验证连接
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if e.session.Ping(pingCtx, &mcp.PingParams{}) != nil {
		e.status = StatusError
		return fmt.Errorf("MCP server %s connection verification failed", e.name)
	}

	e.lastPing = time.Now()
	e.status = StatusConnected

	// 启动健康检测
	e.stopCh = make(chan struct{})
	go e.startHealthCheck(ctx)

	return nil
}

// closeSession 关闭 session（内部调用，不加锁）
func (e *Client) closeSession() {
	if e.session != nil {
		e.session.Close()
		e.session = nil
	}
}

// startHealthCheck 启动后台健康检测 goroutine
func (e *Client) startHealthCheck(ctx context.Context) {
	ticker := time.NewTicker(e.maxPingAge)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.stopCh:
			return
		case <-ticker.C:
			e.mu.RLock()
			session := e.session
			e.mu.RUnlock()

			if session == nil {
				continue
			}

			pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := session.Ping(pingCtx, &mcp.PingParams{})
			cancel()

			if err != nil {
				log.Printf("MCP server %s health check failed: %v, attempting reconnect", e.name, err)

				reconnectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				reconnectErr := e.connect(reconnectCtx)
				cancel()

				if reconnectErr != nil {
					log.Printf("MCP server %s reconnect failed: %v", e.name, reconnectErr)
				} else {
					log.Printf("MCP server %s reconnected successfully", e.name)
				}
			} else {
				e.mu.Lock()
				e.lastPing = time.Now()
				e.mu.Unlock()
			}
		}
	}
}

// Close 关闭客户端连接
func (e *Client) Close() {
	// 停止健康检测 goroutine
	if e.stopCh != nil {
		close(e.stopCh)
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	e.closeSession()
	e.status = StatusDisconnected
}

// RefreshTools 刷新工具列表
func (e *Client) RefreshTools(ctx context.Context) error {
	if err := e.connect(ctx); err != nil {
		return err
	}

	mcpToolResult, err := e.session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		return err
	}

	e.mu.Lock()
	e.tools = make([]tool.Tool, 0)
	for _, mcpTool := range mcpToolResult.Tools {
		agentTool := &McpTool{
			client:   e,
			toolName: mcpTool.Name,
			mcpTool:  mcpTool,
		}

		e.tools = append(e.tools, agentTool)
	}
	e.mu.Unlock()
	return nil
}

// GetTools 获取工具列表
func (e *Client) GetTools() []tool.Tool {
	return e.tools
}

func (e *Client) callTool(ctx context.Context, toolName string, argumentsInJSON string) (string, error) {
	if err := e.connect(ctx); err != nil {
		return "", err
	}

	// 创建带超时的 context
	toolCtx, cancel := context.WithTimeout(ctx, DefaultToolTimeout)
	defer cancel()

	mcpResult, err := e.session.CallTool(toolCtx, &mcp.CallToolParams{
		Name:      toolName,
		Arguments: json.RawMessage(argumentsInJSON),
	})
	if err != nil {
		// 检查是否是超时
		if toolCtx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("tool call timed out after %v", DefaultToolTimeout)
		}
		log.Printf("failed to call tool: %v", err)

		// 如果调用失败，检查连接是否断开，标记需要重连
		e.mu.Lock()
		if e.session != nil && e.session.Ping(ctx, &mcp.PingParams{}) != nil {
			e.status = StatusDisconnected
		}
		e.mu.Unlock()

		return "", err
	}

	// 更新 lastPing
	e.mu.Lock()
	e.lastPing = time.Now()
	e.mu.Unlock()

	var builder strings.Builder
	for _, content := range mcpResult.Content {
		if textContent, ok := content.(*mcp.TextContent); ok {
			builder.WriteString(textContent.Text)
		}
	}
	return builder.String(), nil
}

// McpTool 实现 tool.Tool 接口
type McpTool struct {
	toolName string // 给 mcp server 看的，和给模型看的不一样
	client   *Client
	mcpTool  *mcp.Tool
}

// ToolName 给模型看的，和给 mcp server 看的不一样
func (t *McpTool) ToolName() string {
	return fmt.Sprintf("awesomebot_mcp__%s__%s", t.client.Name(), t.toolName)
}

func (t *McpTool) Info() openai.ChatCompletionToolUnionParam {
	return openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
		Description: openai.String(t.mcpTool.Description),
		Name:        t.ToolName(),
		Parameters: openai.FunctionParameters{
			"type": "object",
			// 初始不提供 properties，让 LLM 通过 get_mcp_tool_schema 获取
		},
	})
}

func (t *McpTool) Execute(ctx context.Context, argumentsInJSON string) (string, error) {
	return t.client.callTool(ctx, t.toolName, argumentsInJSON)
}

