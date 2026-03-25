package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

const (
	DefaultSandboxContainer = "awesomebot-sandbox"
	DefaultSandboxImage     = "alpine:3.19"
)

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
	denyPatterns  []*regexp.Regexp

	once     sync.Once
	startErr error
}

func NewDockerBashTool(containerName, workspaceDir string) *DockerBashTool {
	return &DockerBashTool{
		containerName: containerName,
		image:         DefaultSandboxImage,
		workspaceDir:  workspaceDir,
		denyPatterns:  getDenyPatterns(),
	}
}

func (t *DockerBashTool) ToolName() AgentTool {
	return AgentToolBash
}

func (t *DockerBashTool) Info() openai.ChatCompletionToolUnionParam {
	return openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
		Name:        string(AgentToolBash),
		Description: openai.String("execute bash command in a docker sandbox container with safety guards (timeout, dangerous command filtering, path traversal protection, output truncation)"),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "the bash command to execute in the sandbox",
				},
				"timeout": map[string]any{
					"type":        "integer",
					"description": "timeout in seconds (default 60, max 600)",
					"minimum":     1,
					"maximum":     600,
				},
			},
			"required": []string{"command"},
		},
	})
}

func (t *DockerBashTool) Execute(ctx context.Context, argumentsInJSON string) (string, error) {
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

	// Guard: dangerous command filtering
	if err := t._guardCommand(p.Command); err != nil {
		return err.Error(), nil
	}

	// Guard: path traversal protection
	if t.workspaceDir != "" {
		if err := t._guardPath(p.Command); err != nil {
			return err.Error(), nil
		}
	}

	// Determine timeout
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	if timeout > MaxTimeout {
		timeout = MaxTimeout
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "exec",
		t.containerName,
		"sh", "-c", p.Command)

	output, err := cmd.CombinedOutput()
	result := string(output)

	if ctx.Err() == context.DeadlineExceeded {
		return "", &TimeoutError{Timeout: timeout}
	}

	if err != nil {
		return result + "\n[Error: " + err.Error() + "]", nil
	}

	return t._truncateOutput(result), nil
}

func (t *DockerBashTool) _guardCommand(cmd string) error {
	lower := strings.ToLower(cmd)
	for _, pattern := range t.denyPatterns {
		if pattern.MatchString(lower) {
			return &DangerousCommandError{Command: cmd}
		}
	}
	return nil
}

func (t *DockerBashTool) _guardPath(cmd string) error {
	if strings.Contains(cmd, "..\\") || strings.Contains(cmd, "../") {
		return &PathTraversalError{Path: ".."}
	}

	winPaths := regexp.MustCompile(`[A-Za-z]:\\[^\s"'|><;]+`).FindAllString(cmd, -1)
	posixPaths := regexp.MustCompile(`(?:^|[\s|>'\"])(/[^\s"'>;|<]+)`).FindAllString(cmd, -1)
	homePaths := regexp.MustCompile(`(?:^|[\s|>'\"])(~[^\s"'>;|<]*)`).FindAllString(cmd, -1)

	workspacePath, _ := filepath.Abs(t.workspaceDir)

	for _, path := range append(append(winPaths, posixPaths...), homePaths...) {
		path = strings.TrimSpace(path)
		expanded := os.ExpandEnv(path)
		expanded = strings.Replace(expanded, "~", os.Getenv("HOME"), 1)
		absPath, err := filepath.Abs(expanded)
		if err != nil {
			continue
		}
		if filepath.IsAbs(absPath) {
			rel, err := filepath.Rel(workspacePath, absPath)
			if err != nil {
				continue
			}
			if strings.HasPrefix(rel, "..") {
				return &PathOutsideWorkspaceError{Path: path}
			}
		}
	}
	return nil
}

func (t *DockerBashTool) _truncateOutput(output string) string {
	if len(output) <= MaxOutput {
		return output
	}
	half := MaxOutput / 2
	return output[:half] +
		"\n\n... (" + formatNumber(len(output)-MaxOutput) + " chars truncated) ...\n\n" +
		output[len(output)-half:]
}

func (t *DockerBashTool) ensureSandboxContainer(ctx context.Context) error {
	startCmd := exec.CommandContext(ctx, "docker", "start", t.containerName)
	if startCmd.Run() == nil {
		return nil
	}

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
