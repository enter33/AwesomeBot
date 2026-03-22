package tool

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

type BashTool struct{}

func NewBashTool() *BashTool {
	return &BashTool{}
}

type BashToolParam struct {
	Command string `json:"command"`
}

func (t *BashTool) ToolName() AgentTool {
	return AgentToolBash
}

func (t *BashTool) Info() openai.ChatCompletionToolUnionParam {
	return openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
		Name:        string(AgentToolBash),
		Description: openai.String("execute bash command"),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "the bash command to execute",
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

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		// Windows: 使用 PowerShell 执行命令，使用完整路径避免 PATH 问题
		powershellPath := findPowerShell()
		if powershellPath == "" {
			return "", exec.ErrNotFound
		}
		cmd = exec.CommandContext(ctx, powershellPath, "-NoProfile", "-Command", p.Command)
		// 继承当前进程的环境变量
		cmd.Env = os.Environ()
	} else {
		// Linux/macOS: use POSIX sh
		cmd = exec.CommandContext(ctx, "sh", "-c", p.Command)
	}

	output, err := cmd.CombinedOutput()
	result := string(output)

	// 即使有错误也返回输出内容，错误信息附加在结果后面
	if err != nil {
		if result != "" {
			return result + "\n[Error: " + err.Error() + "]", nil
		}
		return "", err
	}
	return result, nil
}

// findPowerShell 查找 PowerShell 可执行文件的完整路径
func findPowerShell() string {
	// 常见的 PowerShell 安装路径
	paths := []string{
		// PowerShell Core (pwsh)
		`C:\Program Files\PowerShell\7\pwsh.exe`,
		// Windows PowerShell
		`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
		// 备用路径
		`C:\Windows\SysWOW64\WindowsPowerShell\v1.0\powershell.exe`,
	}

	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	// 尝试在 PATH 中查找
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
