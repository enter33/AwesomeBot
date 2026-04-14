package orchestrator

import (
	"context"
	"strings"
	"sync"

	"github.com/enter33/AwesomeBot/internal/agent"
	"github.com/enter33/AwesomeBot/internal/subagent"
	"github.com/enter33/AwesomeBot/pkg/config"
)

// ErrMaxRetriesExceeded 超过最大重试次数
var ErrMaxRetriesExceeded = &OrchestratorError{"max retries exceeded"}

// OrchestratorError 编排器错误
type OrchestratorError struct {
	msg string
}

func (e *OrchestratorError) Error() string {
	return e.msg
}

// ReviewNotPassedError 审查未通过错误
type ReviewNotPassedError struct {
	Comments string
}

func (e *ReviewNotPassedError) Error() string {
	return "review not passed: " + e.Comments
}

// Orchestrator 主编排器（包装 WorkflowEngine）
type Orchestrator struct {
	engine     *Engine
	config     config.MultiAgentConfig
	complexity *ComplexityDetector
	mu         sync.RWMutex
}

// NewOrchestrator 创建编排器
func NewOrchestrator(
	cfg config.MultiAgentConfig,
	subagentMgr *subagent.Manager,
	mainAgent *agent.Agent,
) *Orchestrator {
	engine, err := NewEngine(subagentMgr, mainAgent)
	if err != nil {
		panic(err)
	}

	return &Orchestrator{
		engine:     engine,
		config:     cfg,
		complexity: NewComplexityDetector(cfg.ComplexityThreshold),
	}
}

// SetMainAgentViewCh 设置主 agent 的 view channel（供 TUI 展示流式输出）
func (o *Orchestrator) SetMainAgentViewCh(ch chan<- agent.MessageVO) {
	o.engine.SetMainAgentViewCh(ch)
}

// ShouldOrchestrate 根据配置和复杂度自动判断是否启用编排
func (o *Orchestrator) ShouldOrchestrate(request string) bool {
	if o.config.Enabled {
		return true
	}
	return o.complexity.Detect(request) == ComplexityComplex
}

// State 返回当前状态（线程安全）
func (o *Orchestrator) State() OrchestrationState {
	return stateFromString(o.engine.CurrentNode())
}

// ExecuteAsync 异步执行编排流程，返回状态更新 channel
func (o *Orchestrator) ExecuteAsync(ctx context.Context, request string) <-chan OrchestratorUpdate {
	return o.engine.ExecuteAsync(ctx, request)
}

// SubmitUserInput 提交用户输入（由 TUI 调用）
func (o *Orchestrator) SubmitUserInput(input string) bool {
	return o.engine.SubmitUserInput(input)
}

// IsPendingInput 是否正在等待用户输入
func (o *Orchestrator) IsPendingInput() bool {
	return o.engine.IsPendingInput()
}

func isContextErr(err error) bool {
	if err == nil {
		return false
	}
	return err == context.Canceled || err == context.DeadlineExceeded || strings.Contains(err.Error(), "context canceled")
}
