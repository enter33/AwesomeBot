package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/enter33/AwesomeBot/internal/orchestrator"
)

var (
	orchestratorPendingStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	orchestratorActiveStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("226")).Bold(true)
	orchestratorDoneStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	orchestratorFailedStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	orchestratorScoreStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("147"))
	orchestratorPanelBorderStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

// RenderOrchestratorState 渲染编排器状态面板
func RenderOrchestratorState(state orchestrator.OrchestrationState, scores map[orchestrator.OrchestrationState]*orchestrator.ReviewScore) string {
	stages := []struct {
		key   orchestrator.OrchestrationState
		label string
	}{
		{orchestrator.StatePlanning,      "Planning"},
		{orchestrator.StatePlanReviewing, "PlanReview"},
		{orchestrator.StateCoding,        "Coding"},
		{orchestrator.StateCodeReviewing, "CodeReview"},
		{orchestrator.StateFinalReview,   "FinalReview"},
	}

	var parts []string
	for i, s := range stages {
		style := orchestratorPendingStyle
		icon := "○"
		extra := ""

		if isStateCompleted(state, s.key) {
			style = orchestratorDoneStyle
			icon = "✓"
			if sc := scores[s.key]; sc != nil {
				extra = fmt.Sprintf(" (%.0f)", sc.TotalScore)
			}
		} else if state == s.key {
			style = orchestratorActiveStyle
			icon = "●"
			if sc := scores[s.key]; sc != nil {
				extra = fmt.Sprintf(" [%.0f]", sc.TotalScore)
			}
		}

		part := style.Render(fmt.Sprintf("%s %s%s", icon, s.label, extra))
		parts = append(parts, part)

		if i < len(stages)-1 {
			parts = append(parts, orchestratorPanelBorderStyle.Render(" → "))
		}
	}

	line := strings.Join(parts, "")

	if state == orchestrator.StateDone {
		line += " " + orchestratorDoneStyle.Render("✓ Done")
	} else if state == orchestrator.StatePaused {
		line += " " + orchestratorFailedStyle.Render("■ Paused")
	}

	return orchestratorPanelBorderStyle.Render("[编排] ") + line
}

// isStateCompleted 判断某个阶段是否已经完成
func isStateCompleted(current, target orchestrator.OrchestrationState) bool {
	order := map[orchestrator.OrchestrationState]int{
		orchestrator.StatePending:       0,
		orchestrator.StatePlanning:      1,
		orchestrator.StatePlanReviewing: 2,
		orchestrator.StateCoding:        3,
		orchestrator.StateCodeReviewing: 4,
		orchestrator.StateFinalReview:   5,
		orchestrator.StateDone:          6,
		orchestrator.StatePaused:        6,
	}
	return order[current] > order[target]
}
