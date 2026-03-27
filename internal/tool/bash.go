package tool

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

const (
	DefaultTimeout = 60
	MaxTimeout     = 600
	MaxOutput      = 10_000
)

type BashTool struct {
	workspaceDir  string
	denyPatterns  []*regexp.Regexp
}

func NewBashTool() *BashTool {
	return &BashTool{
		denyPatterns: getDenyPatterns(),
	}
}

func NewBashToolWithWorkspace(dir string) *BashTool {
	return &BashTool{
		workspaceDir: dir,
		denyPatterns: getDenyPatterns(),
	}
}

func getDenyPatterns() []*regexp.Regexp {
	return []*regexp.Regexp{
		regexp.MustCompile(`\brm\s+-[rf]{1,2}\b`),         // rm -r, rm -rf, rm -fr
		regexp.MustCompile(`\bdel\s+/[fq]\b`),             // del /f, del /q
		regexp.MustCompile(`\brmdir\s+/s\b`),              // rmdir /s
		regexp.MustCompile(`(?:^|[;&|]\s*)format\b`),       // format (as standalone command only)
		regexp.MustCompile(`\b(mkfs|diskpart)\b`),         // disk operations
		regexp.MustCompile(`\bdd\s+if=`),                  // dd
		regexp.MustCompile(`>\s*/dev/sd`),                  // write to disk
		regexp.MustCompile(`\b(shutdown|reboot|poweroff)\b`), // system power
		regexp.MustCompile(`:\(\)\s*\{.*\};\s*:`),         // fork bomb
	}
}

type BashToolParam struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

func (t *BashTool) ToolName() AgentTool {
	return AgentToolBash
}

func (t *BashTool) Info() openai.ChatCompletionToolUnionParam {
	return openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
		Name:        string(AgentToolBash),
		Description: openai.String("Execute bash commands with safety guards.\n\nFeatures:\n- Timeout protection (default 60s, max 600s)\n- Dangerous command filtering (rm -rf, del /f, fork bombs, disk operations, etc.)\n- Path traversal protection (prevents access outside workspace)\n- Output truncation at 10k chars\n\nUse when: need to run shell commands, git operations, compile code, run tests, etc.\nAvoid: file operations (use read/write/edit instead).\n\nDangerous commands blocked: rm -rf, del /f, fork bombs, disk operations (mkfs, dd, format), shutdown/reboot"),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "the bash command to execute (e.g., 'git status', 'go build ./...', 'npm test')",
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

func (t *BashTool) Execute(ctx context.Context, argumentsInJSON string) (string, error) {
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

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		powershellPath := findPowerShell()
		if powershellPath == "" {
			return "", exec.ErrNotFound
		}
		cmd = exec.CommandContext(ctx, powershellPath, "-NoProfile", "-Command", p.Command)
		cmd.Env = os.Environ()
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", p.Command)
	}

	output, err := cmd.CombinedOutput()
	result := string(output)

	if ctx.Err() == context.DeadlineExceeded {
		return "", &TimeoutError{Timeout: timeout}
	}

	if err != nil {
		if result != "" {
			return result + "\n[Error: " + err.Error() + "]", nil
		}
		return "", err
	}

	return t._truncateOutput(result), nil
}

func (t *BashTool) _guardCommand(cmd string) error {
	lower := strings.ToLower(cmd)
	for _, pattern := range t.denyPatterns {
		if pattern.MatchString(lower) {
			return &DangerousCommandError{Command: cmd}
		}
	}
	return nil
}

func (t *BashTool) _guardPath(cmd string) error {
	// Check for path traversal patterns
	if strings.Contains(cmd, "..\\") || strings.Contains(cmd, "../") {
		return &PathTraversalError{Path: ".."}
	}

	// Extract and check absolute paths
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
			// Check if path is outside workspace
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

func (t *BashTool) _truncateOutput(output string) string {
	if len(output) <= MaxOutput {
		return output
	}
	half := MaxOutput / 2
	return output[:half] +
		"\n\n... (" + formatNumber(len(output)-MaxOutput) + " chars truncated) ...\n\n" +
		output[len(output)-half:]
}

func formatNumber(n int) string {
	if n < 1000 {
		return string(rune('0'+n/100)) + string(rune('0'+(n/10)%10)) + string(rune('0'+n%10))
	}
	return string(rune('0'+n/100000)) + "," + formatNumberSmall(n%100000)
}

func formatNumberSmall(n int) string {
	if n < 100 {
		return string(rune('0'+n/10)) + string(rune('0'+n%10))
	}
	return string(rune('0'+n/1000)) + "," + formatNumberSmallSmall(n%1000)
}

func formatNumberSmallSmall(n int) string {
	return string(rune('0'+n/100)) + string(rune('0'+(n/10)%10)) + string(rune('0'+n%10))
}

// Error types
type TimeoutError struct {
	Timeout int
}

func (e *TimeoutError) Error() string {
	return "Error: Command timed out after " + itoa(e.Timeout) + " seconds"
}

type DangerousCommandError struct {
	Command string
}

func (e *DangerousCommandError) Error() string {
	return "Error: Command blocked by safety guard (dangerous pattern detected)"
}

type PathTraversalError struct {
	Path string
}

func (e *PathTraversalError) Error() string {
	return "Error: Command blocked by safety guard (path traversal detected)"
}

type PathOutsideWorkspaceError struct {
	Path string
}

func (e *PathOutsideWorkspaceError) Error() string {
	return "Error: Command blocked by safety guard (path outside working dir)"
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}

// findPowerShell 查找 PowerShell 可执行文件的完整路径
func findPowerShell() string {
	paths := []string{
		`C:\Program Files\PowerShell\7\pwsh.exe`,
		`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
		`C:\Windows\SysWOW64\WindowsPowerShell\v1.0\powershell.exe`,
	}

	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	if path, err := exec.LookPath("powershell.exe"); err == nil {
		if absPath, err := filepath.Abs(path); err == nil {
			return absPath
		}
		return path
	}
	if path, err := exec.LookPath("pwsh.exe"); err == nil {
		if absPath, err := filepath.Abs(path); err == nil {
			return absPath
		}
		return path
	}

	return ""
}
