package tui

import (
	"github.com/enter33/AwesomeBot/internal/orchestrator"
)

// RenderOrchestratorState 渲染编排器状态面板
// 已简化为不展示进度条，直接复用 subagent / main agent 的展示逻辑
func RenderOrchestratorState(state orchestrator.OrchestrationState, scores map[orchestrator.OrchestrationState]*orchestrator.ReviewScore) string {
	return ""
}
