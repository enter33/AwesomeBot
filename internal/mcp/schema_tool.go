package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

type GetSchemaTool struct {
	clients map[string]*Client
}

func NewGetSchemaTool(clients []*Client) *GetSchemaTool {
	m := make(map[string]*Client)
	for _, c := range clients {
		m[c.Name()] = c
	}
	return &GetSchemaTool{clients: m}
}

func (t *GetSchemaTool) ToolName() string {
	return "get_mcp_tool_schema"
}

func (t *GetSchemaTool) Info() openai.ChatCompletionToolUnionParam {
	return openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
		Description: openai.String("Get the full schema of a specific MCP tool. " +
			"Call this BEFORE calling an MCP tool to understand its parameters. " +
			"Parameters: server (the MCP server name), tool (the tool name)."),
		Name: t.ToolName(),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"server": map[string]any{
					"type":        "string",
					"description": "The MCP server name (e.g., 'filesystem', 'memory')",
				},
				"tool": map[string]any{
					"type":        "string",
					"description": "The tool name on that server (e.g., 'read_file')",
				},
			},
			"required": []string{"server", "tool"},
		},
	})
}

type GetSchemaParam struct {
	Server string `json:"server"`
	Tool   string `json:"tool"`
}

func (t *GetSchemaTool) Execute(ctx context.Context, argumentsInJSON string) (string, error) {
	p := GetSchemaParam{}
	if err := json.Unmarshal([]byte(argumentsInJSON), &p); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	client, ok := t.clients[p.Server]
	if !ok {
		return "", fmt.Errorf("MCP server not found: %s", p.Server)
	}

	for _, tool := range client.GetTools() {
		mt, ok := tool.(*McpTool)
		if !ok {
			continue
		}
		if mt.toolName == p.Tool {
			schema := map[string]any{
				"name":        mt.mcpTool.Name,
				"description": mt.mcpTool.Description,
				"inputSchema": mt.mcpTool.InputSchema,
			}
			data, _ := json.MarshalIndent(schema, "", "  ")
			return string(data), nil
		}
	}

	return "", fmt.Errorf("tool not found: %s on server %s", p.Tool, p.Server)
}
