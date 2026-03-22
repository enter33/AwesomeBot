package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

const (
	DefaultSandboxContainer = "awesomebot-sandbox"
	DefaultSandboxImage     = "alpine:3.19"
)

// generateContainerName 根据工作目录生成唯一的容器名称
func generateContainerName(workspaceDir string) string {
	projectName := filepath.Base(workspaceDir)
	if projectName == "" || projectName == "." || projectName == "/" {
		return DefaultSandboxContainer
	}
	return fmt.Sprintf("%s-%s", DefaultSandboxContainer, projectName)
}

type DockerBashTool struct {
	containerName string
	image         string
	workspaceDir  string

	once     sync.Once
	startErr error
}

func NewDockerBashTool(containerName, workspaceDir string) *DockerBashTool {
	if containerName == "" {
		containerName = generateContainerName(workspaceDir)
	}
	return &DockerBashTool{
		containerName: containerName,
		image:         DefaultSandboxImage,
		workspaceDir:  workspaceDir,
	}
}

func (t *DockerBashTool) ToolName() AgentTool {
	return AgentToolBash
}

func (t *DockerBashTool) Info() openai.ChatCompletionToolUnionParam {
	return openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
		Name:        string(AgentToolBash),
		Description: openai.String("execute bash command in a docker sandbox container"),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "the bash command to execute in the sandbox",
				},
			},
			"required": []string{"command"},
		},
	})
}

func (t *DockerBashTool) Execute(ctx context.Context, argumentsInJSON string) (string, error) {
	// Lazy initialization: start container on first use
	t.once.Do(func() {
		t.startErr = t.ensureSandboxContainer(ctx)
	})
	if t.startErr != nil {
		return "", fmt.Errorf("failed to start sandbox container: %w", t.startErr)
	}

	p := BashToolParam{}
	err := json.Unmarshal([]byte(argumentsInJSON), &p)
	if err != nil {
		return "", err
	}

	// Execute command in container via docker exec
	cmd := exec.CommandContext(ctx, "docker", "exec",
		t.containerName,
		"sh", "-c", p.Command)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("docker exec failed: %w", err)
	}
	return string(output), nil
}

func (t *DockerBashTool) ensureSandboxContainer(ctx context.Context) error {
	// First, try to start existing container
	startCmd := exec.CommandContext(ctx, "docker", "start", t.containerName)
	if startCmd.Run() == nil {
		return nil
	}

	// Container doesn't exist, create new one
	createCmd := exec.CommandContext(ctx, "docker", "run", "-d",
		"--name", t.containerName,
		"--restart", "unless-stopped",
		"-v", t.workspaceDir+":/workspace:rw",
		"-w", "/workspace",
		t.image,
		"sleep", "infinity")

	output, err := createCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create sandbox container: %s: %w", string(output), err)
	}
	return nil
}
