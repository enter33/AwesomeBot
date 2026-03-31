package tui

import (
	"os"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
)

var (
	headerBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("39")).
			Padding(0, 1)

	headerTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("39"))

	headerLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("69"))

	headerValueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	headerIconStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("226"))
)

type HeaderInfo struct {
	WorkingDir string
	ModelName  string
	Version    string
	Width      int
}

func getWorkingDirName() string {
	wd, err := os.Getwd()
	if err != nil {
		return "unknown"
	}
	return filepath.Base(wd)
}

func RenderHeader(info HeaderInfo) string {
	width := info.Width
	if width < 40 {
		width = 40
	}
	if width > 80 {
		width = 80
	}

	innerWidth := width - 4

	var b strings.Builder

	icon := headerIconStyle.Render("🤖")
	title := headerTitleStyle.Render(" AwesomeBot")
	b.WriteString(icon)
	b.WriteString(title)
	b.WriteString("\n")

	b.WriteString(headerLabelStyle.Render("  ├─ 工作目录: "))
	b.WriteString(headerValueStyle.Render(info.WorkingDir))
	b.WriteString("\n")

	b.WriteString(headerLabelStyle.Render("  └─ 模型: "))
	b.WriteString(headerValueStyle.Render(info.ModelName))
	b.WriteString(" · ")
	b.WriteString(headerLabelStyle.Render("版本: "))
	b.WriteString(headerValueStyle.Render(info.Version))

	content := b.String()

	return headerBoxStyle.Width(innerWidth).Render(content)
}
