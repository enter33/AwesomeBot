package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/enter33/AwesomeBot/internal/subagent"
)

var (
	subagentStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("201"))
	subagentRunningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("82")).Bold(true)
)

// SubagentEntry 子代理日志条目
type SubagentEntry struct {
	ID     string
	Name   string
	Type   string
	Status subagent.SubagentStatus
}

// Render 渲染子代理面板
func (e *SubagentEntry) Render() string {
	var statusIcon string
	var style lipgloss.Style

	switch e.Status {
	case subagent.StatusRunning:
		statusIcon = "●"
		style = subagentRunningStyle
	case subagent.StatusCompleted:
		statusIcon = "✓"
		style = subagentDoneStyle
	case subagent.StatusFailed:
		statusIcon = "✗"
		style = errorStyle
	case subagent.StatusStopped:
		statusIcon = "■"
		style = subagentDoneStyle
	default:
		statusIcon = "○"
		style = subagentStyle
	}

	return style.Render(fmt.Sprintf("%s [%s] %s (%s)",
		statusIcon, e.Type, e.Name, e.Status))
}

// RenderSubagentList 渲染子代理列表
func RenderSubagentList(subagents []subagent.Subagent) string {
	if len(subagents) == 0 {
		return ""
	}

	var lines []string
	lines = append(lines, subagentStyle.Render("--- Subagents ---"))

	for _, s := range subagents {
		entry := &SubagentEntry{
			ID:     s.ID(),
			Name:   s.Name(),
			Type:   string(s.Type()),
			Status: s.Status(),
		}
		lines = append(lines, entry.Render())
	}

	return strings.Join(lines, "\n")
}
