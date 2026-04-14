package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/enter33/AwesomeBot/internal/subagent"
	"gopkg.in/yaml.v3"
)

// workflowYAML 是 workflow.yaml 的结构
type workflowYAML struct {
	ID                  string            `yaml:"id"`
	Name                string            `yaml:"name"`
	Description         string            `yaml:"description"`
	EntryNode           string            `yaml:"entry_node"`
	Nodes               []string          `yaml:"nodes"`
	DefaultTransitions  map[string]string `yaml:"default_transitions"`
	Router              RouterConfig      `yaml:"router"`
}

// nodeYAML 是 nodes/{node}/agent.yaml 的结构
type nodeYAML struct {
	Name          string               `yaml:"name"`
	Description   string               `yaml:"description"`
	ExecutionMode string               `yaml:"execution_mode"`
	SubagentType  string               `yaml:"subagent_type"`
	PromptFile    string               `yaml:"prompt_file"`
	InputTemplate string               `yaml:"input_template"`
	OutputFormat  string               `yaml:"output_format"`
	RetryLimit    int                  `yaml:"retry_limit"`
}

// LoadWorkflow 加载指定目录的工作流定义
func LoadWorkflow(dir string) (*Workflow, map[string]*Node, error) {
	wfPath := filepath.Join(dir, "workflow.yaml")
	data, err := os.ReadFile(wfPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read workflow.yaml: %w", err)
	}

	var wy workflowYAML
	if err := yaml.Unmarshal(data, &wy); err != nil {
		return nil, nil, fmt.Errorf("failed to parse workflow.yaml: %w", err)
	}

	wf := &Workflow{
		ID:                 wy.ID,
		Name:               wy.Name,
		Description:        wy.Description,
		EntryNode:          wy.EntryNode,
		Nodes:              wy.Nodes,
		DefaultTransitions: wy.DefaultTransitions,
		Router:             wy.Router,
	}

	nodes := make(map[string]*Node)
	for _, nodeID := range wy.Nodes {
		node, err := loadNode(dir, nodeID)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to load node %s: %w", nodeID, err)
		}
		nodes[nodeID] = node
	}

	return wf, nodes, nil
}

func loadNode(baseDir, nodeID string) (*Node, error) {
	nodeDir := filepath.Join(baseDir, "nodes", nodeID)
	agentPath := filepath.Join(nodeDir, "agent.yaml")

	data, err := os.ReadFile(agentPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read agent.yaml: %w", err)
	}

	var ny nodeYAML
	if err := yaml.Unmarshal(data, &ny); err != nil {
		return nil, fmt.Errorf("failed to parse agent.yaml: %w", err)
	}

	promptPath := filepath.Join(nodeDir, ny.PromptFile)
	promptData, err := os.ReadFile(promptPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read prompt file %s: %w", ny.PromptFile, err)
	}

	mode := ny.ExecutionMode
	if mode == "" {
		mode = ExecutionModeSubagent
	}

	retryLimit := ny.RetryLimit
	if retryLimit <= 0 {
		retryLimit = 3
	}

	node := &Node{
		ID:            nodeID,
		Name:          ny.Name,
		Description:   ny.Description,
		ExecutionMode: mode,
		SubagentType:  subagent.SubagentType(ny.SubagentType),
		Prompt:        string(promptData),
		InputTemplate: ny.InputTemplate,
		OutputFormat:  ny.OutputFormat,
		RetryLimit:    retryLimit,
	}

	return node, nil
}

// RenderInput 渲染节点的输入模板
func RenderInput(tplStr string, data map[string]any) (string, error) {
	tpl, err := template.New("input").Parse(tplStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse input template: %w", err)
	}

	var buf strings.Builder
	if err := tpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute input template: %w", err)
	}
	return buf.String(), nil
}
