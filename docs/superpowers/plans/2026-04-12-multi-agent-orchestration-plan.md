# 多 Agent 层级编排系统实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现带状态转移的多 Agent 流水线编排系统，包含 PlanAgent、PlanReviewer、CodingAgent、CodeReviewer、TaskReviewer 五个专精 Agent，通过评分阈值和重试机制协作。

**Architecture:** 基于现有 `subagent` 模块构建，新增 `orchestrator` 模块管理状态流转。每个 Agent 通过 channel 与 orchestrator 通信，采用增量上下文传递信息。

**Tech Stack:** Go, 现有 `internal/subagent`, `internal/agent`, `pkg/config`

---

## 关键设计变更

**Final Review 不合格 → 回退到 Planning 阶段重新开始**

TaskReviewer 不合格时，问题可能在计划层面，所以需要重新走 Plan → Review → Code → CodeReview → Final 的完整流程，而不是仅在 Code 阶段重试。

---

## 文件结构

```
internal/orchestrator/
  orchestrator.go       # 主编排器，状态机核心
  state.go              # 状态定义与转换
  config.go             # 多 Agent 配置加载
  metrics.go            # 评分系统
  error_logger.go       # 异常记录与学习
  complexity.go         # 任务复杂度判断
  tui_panel.go          # TUI 展示
  prompt/
    planner.txt         # PlanAgent 提示词
    planner_reviewer.txt # PlanReviewer 提示词
    coder.txt           # CodingAgent 提示词
    coder_reviewer.txt  # CodeReviewer 提示词
    final_reviewer.txt  # TaskReviewer 提示词
    loader.go           # 提示词加载器

pkg/config/
  loader.go             # 修改: 添加 MultiAgentConfig
```

---

## Phase 1: 核心基础设施

### Task 1: 创建目录结构和状态定义

**Files:**
- Create: `internal/orchestrator/state.go`
- Create: `internal/orchestrator/error_logger.go`

- [ ] **Step 1: 创建 state.go**

```go
package orchestrator

// OrchestrationState 编排状态
type OrchestrationState string

const (
	StatePending        OrchestrationState = "pending"
	StatePlanning       OrchestrationState = "planning"
	StatePlanReviewing  OrchestrationState = "plan_reviewing"
	StateCoding         OrchestrationState = "coding"
	StateCodeReviewing  OrchestrationState = "code_reviewing"
	StateFinalReview    OrchestrationState = "final_review"
	StateDone           OrchestrationState = "done"
	StatePaused         OrchestrationState = "paused"
)

// PhaseResult 阶段执行结果
type PhaseResult struct {
	State       OrchestrationState
	Output      string
	Score       *ReviewScore
	RetryCount  int
	Error       error
	LearnedFrom string // 从错误中学到的经验
}

// ReviewScore 审查评分
type ReviewScore struct {
	TotalScore float64 // 0-100
	Dimensions []DimensionScore
	Passed     bool
	Comments   string
}

// DimensionScore 单个维度评分
type DimensionScore struct {
	Name   string
	Score  float64 // 0-100
	Weight float64 // 0-1
}
```

- [ ] **Step 2: 创建 error_logger.go**

```go
package orchestrator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/enter33/AwesomeBot/pkg/config"
)

// ErrorType 错误类型
type ErrorType string

const (
	ErrorTypeToolFailed     ErrorType = "tool_execution_failed"
	ErrorTypeLLMFailed      ErrorType = "llm_response_failed"
	ErrorTypeContextOverflow ErrorType = "context_overflow"
	ErrorTypeUnknown        ErrorType = "unknown"
)

// ErrorLog 错误日志
type ErrorLog struct {
	Phase         string      `json:"phase"`
	ErrorType     ErrorType   `json:"error_type"`
	ErrorDetail   string      `json:"error_detail"`
	ContextSnap   string      `json:"context_snapshot"`
	LearnedLesson string      `json:"learned_lesson"`
	Timestamp     time.Time   `json:"timestamp"`
	RetryCount    int         `json:"retry_count"`
}

// ErrorLogger 错误记录器
type ErrorLogger struct {
	mu      sync.RWMutex
	logs    []ErrorLog
	logPath string
}

func NewErrorLogger() *ErrorLogger {
	path := filepath.Join(config.GetAwesomeDir(), "logs", "orchestrator_errors.json")
	return &ErrorLogger{logPath: path}
}

func (e *ErrorLogger) Log(err ErrorLog) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.logs = append(e.logs, err)
	e.persist()
}

func (e *ErrorLogger) persist() {
	content, _ := json.MarshalIndent(e.logs, "", "  ")
	os.WriteFile(e.logPath, content, 0644)
}

func (e *ErrorLogger) GetLessons(phase string) []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var lessons []string
	for _, log := range e.logs {
		if log.Phase == phase && log.LearnedLesson != "" {
			lessons = append(lessons, log.LearnedLesson)
		}
	}
	return lessons
}
```

- [ ] **Step 3: 提交**

```bash
git add internal/orchestrator/state.go internal/orchestrator/error_logger.go
git commit -m "feat(orchestrator): add state definitions and error logger"
```

---

### Task 2: 配置管理

**Files:**
- Modify: `pkg/config/loader.go:128-173`

- [ ] **Step 1: 添加 MultiAgentConfig 结构体**

```go
// MultiAgentConfig 多Agent配置
type MultiAgentConfig struct {
	Enabled             bool              `json:"enabled"`
	ComplexityThreshold int               `json:"complexity_threshold"`
	RetryLimits         RetryLimitsConfig `json:"retry_limits"`
	Thresholds          ThresholdsConfig  `json:"thresholds"`
	InteractionMode     string            `json:"interaction_mode"` // "auto" or "manual"
}

// RetryLimitsConfig 重试限制配置
type RetryLimitsConfig struct {
	PlanPhase     int `json:"plan_phase"`     // 默认 3
	CodePhase     int `json:"code_phase"`     // 默认 5
	MaxTotalRetries int `json:"max_total_retries"` // 全局重试上限，默认 10
}

// ThresholdsConfig 评分阈值配置
type ThresholdsConfig struct {
	PlanReviewer float64 `json:"plan_reviewer"` // 默认 70
	CodeReviewer float64 `json:"code_reviewer"` // 默认 70
	TaskReviewer float64 `json:"task_reviewer"` // 默认 70
}
```

- [ ] **Step 2: 修改 AwesomeConfig 添加多Agent配置**

在 `AwesomeConfig` 结构体中添加:
```go
MultiAgent MultiAgentConfig `json:"multi_agent"`
```

- [ ] **Step 3: 修改 LoadAwesomeConfig 加载默认值**

```go
// 在 LoadAwesomeConfig 中添加默认值加载
if cfg.MultiAgent.RetryLimits.PlanPhase == 0 {
	cfg.MultiAgent.RetryLimits.PlanPhase = 3
}
if cfg.MultiAgent.RetryLimits.CodePhase == 0 {
	cfg.MultiAgent.RetryLimits.CodePhase = 5
}
if cfg.MultiAgent.RetryLimits.MaxTotalRetries == 0 {
	cfg.MultiAgent.RetryLimits.MaxTotalRetries = 10 // 全局重试上限
}
// ... 类似处理 thresholds
```

- [ ] **Step 4: 提交**

```bash
git add pkg/config/loader.go
git commit -m "feat(config): add MultiAgentConfig to AwesomeConfig"
```

---

### Task 3: 评分系统

**Files:**
- Create: `internal/orchestrator/metrics.go`

- [ ] **Step 1: 创建 metrics.go**

```go
package orchestrator

// ScoringDimensions 各 Reviewer 的评分维度
var PlanReviewerDimensions = []DimensionConfig{
	{Name: "目标清晰度", Weight: 0.20},
	{Name: "步骤可行性", Weight: 0.30},
	{Name: "风险识别", Weight: 0.25},
	{Name: "完整性", Weight: 0.25},
}

var CodeReviewerDimensions = []DimensionConfig{
	{Name: "逻辑正确性", Weight: 0.30},
	{Name: "边界处理", Weight: 0.20},
	{Name: "安全性", Weight: 0.25},
	{Name: "可维护性", Weight: 0.25},
}

var TaskReviewerDimensions = []DimensionConfig{
	{Name: "需求覆盖度", Weight: 0.35},
	{Name: "完成度", Weight: 0.35},
	{Name: "质量评估", Weight: 0.30},
}

// DimensionConfig 维度配置
type DimensionConfig struct {
	Name   string
	Weight float64
}

// ScoreCalculator 评分计算器
type ScoreCalculator struct{}

func NewScoreCalculator() *ScoreCalculator {
	return &ScoreCalculator{}
}

// CalculateWithThreshold 计算总分，使用指定的阈值
func (s *ScoreCalculator) CalculateWithThreshold(dimensions []DimensionConfig, scores []float64, threshold float64) *ReviewScore {
	if len(dimensions) != len(scores) {
		return &ReviewScore{TotalScore: 0, Passed: false}
	}

	var total float64
	var dimScores []DimensionScore
	for i, d := range dimensions {
		weighted := scores[i] * d.Weight
		total += weighted
		dimScores = append(dimScores, DimensionScore{
			Name:   d.Name,
			Score:  scores[i],
			Weight: d.Weight,
		})
	}

	return &ReviewScore{
		TotalScore: total,
		Dimensions: dimScores,
		Passed:     total >= threshold, // 使用配置的阈值
	}
}
```

- [ ] **Step 2: 提交**

```bash
git add internal/orchestrator/metrics.go
git commit -m "feat(orchestrator): add scoring system with dimension configs"
```

---

## Phase 2: 提示词系统

### Task 4: 创建 Agent 提示词

**Files:**
- Create: `internal/orchestrator/prompt/planner.txt`
- Create: `internal/orchestrator/prompt/planner_reviewer.txt`
- Create: `internal/orchestrator/prompt/coder.txt`
- Create: `internal/orchestrator/prompt/coder_reviewer.txt`
- Create: `internal/orchestrator/prompt/final_reviewer.txt`
- Create: `internal/orchestrator/prompt/loader.go`

- [ ] **Step 1: 创建提示词文件** (5个 .txt 文件，内容见原始计划)

- [ ] **Step 2: 创建 loader.go**

```go
package prompt

import (
	"os"
	"path/filepath"
)

type Loader struct {
	basePath string
}

func NewLoader(basePath string) *Loader {
	return &Loader{basePath: basePath}
}

func (l *Loader) Load(name string) (string, error) {
	path := filepath.Join(l.basePath, name+".txt")
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(content), nil
}
```

- [ ] **Step 3: 提交**

```bash
git add internal/orchestrator/prompt/
git commit -m "feat(orchestrator): add agent prompts for all 5 agents"
```

---

## Phase 3: 主编排器

### Task 5: 主编排器实现

**Files:**
- Create: `internal/orchestrator/orchestrator.go`

- [ ] **Step 1: 创建 orchestrator.go 核心结构**

```go
package orchestrator

import (
	"context"
	"sync"
	"time"

	"github.com/enter33/AwesomeBot/internal/agent"
	"github.com/enter33/AwesomeBot/internal/subagent"
	"github.com/enter33/AwesomeBot/pkg/config"
	"github.com/enter33/AwesomeBot/pkg/llm"
)

// ErrMaxRetriesExceeded 超过最大重试次数
var ErrMaxRetriesExceeded = &OrchestratorError{"max retries exceeded"}

// Orchestrator 主编排器
type Orchestrator struct {
	state        OrchestrationState
	config       MultiAgentConfig
	subagentMgr  *subagent.Manager
	errorLogger  *ErrorLogger
	scoreCalc    *ScoreCalculator
	promptLoader *prompt.Loader

	mu          sync.RWMutex
	planRetry   int // Plan 阶段重试计数
	codeRetry   int // Code 阶段重试计数
	totalRetry  int // 全局重试计数

	userRequest string
	plan        string
	codeResult  string
}

// NewOrchestrator 创建编排器
func NewOrchestrator(
	llmConfig config.Config,
	confirmConfig agent.ToolConfirmConfig,
	tools []tool.Tool,
) *Orchestrator {
	return &Orchestrator{
		state:       StatePending,
		config:      defaultConfig(),
		errorLogger: NewErrorLogger(),
		scoreCalc:   NewScoreCalculator(),
	}
}
```

- [ ] **Step 2: 实现 Execute 主流程（带状态回退）**

```go
// Execute 执行完整的编排流程
func (o *Orchestrator) Execute(ctx context.Context, request string) error {
	o.mu.Lock()
	o.state = StatePlanning
	o.userRequest = request
	o.plan = ""
	o.codeResult = ""
	o.planRetry = 0
	o.codeRetry = 0
	o.totalRetry = 0
	o.mu.Unlock()

	// 主循环：会在 planning/coding/final_review 之间流转
	for {
		// 检查全局重试上限
		if o.totalRetry >= o.config.RetryLimits.MaxTotalRetries {
			o.setState(StatePaused)
			return ErrMaxRetriesExceeded
		}

		switch o.getState() {
		case StatePlanning:
			plan, err := o.runPlanningPhase(ctx)
			if err != nil {
				if o.planRetry >= o.config.RetryLimits.PlanPhase {
					o.setState(StatePaused)
					return ErrMaxRetriesExceeded
				}
				continue // 重试 planning
			}
			o.plan = plan

		case StateCoding:
			result, err := o.runCodingPhase(ctx, o.plan)
			if err != nil {
				if o.codeRetry >= o.config.RetryLimits.CodePhase {
					o.setState(StatePaused)
					return ErrMaxRetriesExceeded
				}
				continue // 重试 coding
			}
			o.codeResult = result

		case StateFinalReview:
			passed, err := o.runFinalPhase(ctx, o.codeResult)
			if err != nil {
				return err
			}
			if !passed {
				// Final Review 不合格 → 回退到 Planning 阶段重新开始
				o.resetForRepan()
				o.setState(StatePlanning)
				continue
			}
			o.setState(StateDone)
			return nil

		case StatePaused:
			return ErrMaxRetriesExceeded

		default:
			return &OrchestratorError{"unexpected state"}
		}
	}
}

// resetForRepan 重置状态用于重新规划
func (o *Orchestrator) resetForRepan() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.plan = ""
	o.codeResult = ""
	o.planRetry = 0
	o.codeRetry = 0
	o.totalRetry++
}
```

- [ ] **Step 3: 实现 Plan 阶段**

```go
// runPlanningPhase 执行计划阶段
func (o *Orchestrator) runPlanningPhase(ctx context.Context) (string, error) {
	o.setState(StatePlanning)

	// 1. 执行 PlanAgent
	plan, err := o.executePlanAgent(ctx)
	if err != nil {
		o.handleError(StatePlanning, err)
		o.planRetry++
		return "", err
	}

	// 2. 执行 PlanReviewer
	score, err := o.executePlanReviewer(ctx, plan)
	if err != nil {
		o.handleError(StatePlanReviewing, err)
		o.planRetry++
		return "", err
	}

	// 3. 检查是否通过
	if score.Passed {
		o.setState(StateCoding)
		return plan, nil
	}

	// 4. 打回重做（携带审查意见）
	o.planRetry++
	logging.Info("PlanReviewer 打回计划，携带意见: %s", score.Comments)

	// 注意：不切换状态，外部循环会处理重试
	return "", &ReviewNotPassedError{score.Comments}
}
```

- [ ] **Step 4: 实现 Code 阶段**

```go
// runCodingPhase 执行编码阶段
func (o *Orchestrator) runCodingPhase(ctx context.Context, plan string) (string, error) {
	o.setState(StateCoding)

	// 1. 执行 CodingAgent
	result, err := o.executeCodingAgent(ctx, plan)
	if err != nil {
		o.handleError(StateCoding, err)
		o.codeRetry++
		return "", err
	}

	// 2. 执行 CodeReviewer
	score, err := o.executeCodeReviewer(ctx, result)
	if err != nil {
		o.handleError(StateCodeReviewing, err)
		o.codeRetry++
		return "", err
	}

	// 3. 检查是否通过
	if score.Passed {
		o.setState(StateFinalReview)
		return result, nil
	}

	// 4. 打回重做
	o.codeRetry++
	logging.Info("CodeReviewer 打回代码，携带意见: %s", score.Comments)
	return "", &ReviewNotPassedError{score.Comments}
}
```

- [ ] **Step 5: 实现 Final 阶段**

```go
// ReviewNotPassedError 审查未通过错误
type ReviewNotPassedError struct {
	Comments string
}

func (e *ReviewNotPassedError) Error() string {
	return "review not passed: " + e.Comments
}

// runFinalPhase 执行最终验收
func (o *Orchestrator) runFinalPhase(ctx context.Context, codeResult string) (bool, error) {
	o.setState(StateFinalReview)

	score, err := o.executeTaskReviewer(ctx, codeResult)
	if err != nil {
		o.handleError(StateFinalReview, err)
		return false, err
	}

	return score.Passed, nil
}
```

- [ ] **Step 6: 提交**

```bash
git add internal/orchestrator/orchestrator.go
git commit -m "feat(orchestrator): implement core orchestrator with state machine and rollback"
```

---

### Task 6: Agent 执行器实现

**Files:**
- Modify: `internal/orchestrator/orchestrator.go` (添加方法)

- [ ] **Step 1: 添加 executePlanAgent 方法**

```go
// executePlanAgent 执行 PlanAgent
func (o *Orchestrator) executePlanAgent(ctx context.Context) (string, error) {
	prompt, err := o.promptLoader.Load("planner")
	if err != nil {
		return "", err
	}

	id, err := o.subagentMgr.CreateSubagent(
		"planner",
		subagent.SubagentTypePlan,
		prompt,
		o.userRequest,
	)
	if err != nil {
		return "", err
	}

	resultCh := o.subagentMgr.GetResultChannel(id)
	select {
	case result := <-resultCh:
		return result.Result, result.Err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
```

- [ ] **Step 2: 添加 executePlanReviewer 方法**

```go
// executePlanReviewer 执行 PlanReviewer
func (o *Orchestrator) executePlanReviewer(ctx context.Context, plan string) (*ReviewScore, error) {
	prompt, err := o.promptLoader.Load("planner_reviewer")
	if err != nil {
		return nil, err
	}

	input := fmt.Sprintf("用户需求:\n%s\n\n计划:\n%s", o.userRequest, plan)

	id, err := o.subagentMgr.CreateSubagent(
		"planner_reviewer",
		subagent.SubagentTypePlan,
		prompt,
		input,
	)
	if err != nil {
		return nil, err
	}

	resultCh := o.subagentMgr.GetResultChannel(id)
	select {
	case result := <-resultCh:
		return o.parseReviewScore(result.Result, PlanReviewerDimensions, o.config.Thresholds.PlanReviewer)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
```

- [ ] **Step 3: 添加 executeCodingAgent 方法**

```go
// executeCodingAgent 执行 CodingAgent
func (o *Orchestrator) executeCodingAgent(ctx context.Context, plan string) (string, error) {
	prompt, err := o.promptLoader.Load("coder")
	if err != nil {
		return "", err
	}

	input := fmt.Sprintf("用户需求:\n%s\n\n执行计划:\n%s", o.userRequest, plan)

	id, err := o.subagentMgr.CreateSubagent(
		"coder",
		subagent.SubagentTypeGeneral,
		prompt,
		input,
	)
	if err != nil {
		return "", err
	}

	resultCh := o.subagentMgr.GetResultChannel(id)
	select {
	case result := <-resultCh:
		return result.Result, result.Err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
```

- [ ] **Step 4: 添加 executeCodeReviewer 方法**

```go
// executeCodeReviewer 执行 CodeReviewer
func (o *Orchestrator) executeCodeReviewer(ctx context.Context, codeResult string) (*ReviewScore, error) {
	prompt, err := o.promptLoader.Load("coder_reviewer")
	if err != nil {
		return nil, err
	}

	input := fmt.Sprintf("用户需求:\n%s\n\n代码结果:\n%s", o.userRequest, codeResult)

	id, err := o.subagentMgr.CreateSubagent(
		"code_reviewer",
		subagent.SubagentTypePlan,
		prompt,
		input,
	)
	if err != nil {
		return nil, err
	}

	resultCh := o.subagentMgr.GetResultChannel(id)
	select {
	case result := <-resultCh:
		return o.parseReviewScore(result.Result, CodeReviewerDimensions, o.config.Thresholds.CodeReviewer)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
```

- [ ] **Step 5: 添加 parseReviewScore 解析评分**

```go
// parseReviewScore 从 Reviewer 输出解析评分
func (o *Orchestrator) parseReviewScore(output string, dimensions []DimensionConfig, threshold float64) (*ReviewScore, error) {
	// 从输出中提取 "总分: X/100"
	re := regexp.MustCompile(`总分:\s*(\d+)`)
	matches := re.FindStringSubmatch(output)

	var score float64
	if len(matches) >= 2 {
		score, _ = strconv.ParseFloat(matches[1], 64)
	} else {
		// 无法解析时，请求用户确认或使用保守默认值
		// 这里使用维度平均分作为fallback
		score = 50
		logging.Warn("无法从Reviewer输出解析评分，使用默认值50")
	}

	return &ReviewScore{
		TotalScore: score,
		Dimensions: nil,
		Passed:     score >= threshold,
		Comments:   extractComments(output),
	}, nil
}

func extractComments(output string) string {
	// 提取质疑点或评论
	re := regexp.MustCompile(`(?s)(质疑点|问题列表|改进建议|通过与否).*`)
	if re == nil {
		return ""
	}
	match := re.FindString(output)
	// 截取前500字符避免过长
	if len(match) > 500 {
		return match[:500] + "..."
	}
	return match
}
```

- [ ] **Step 6: 提交**

```bash
git add internal/orchestrator/orchestrator.go
git commit -m "feat(orchestrator): add agent executor methods"
```

---

## Phase 4: 辅助功能

### Task 7: 复杂度判断

**Files:**
- Create: `internal/orchestrator/complexity.go`

- [ ] **Step 1: 创建 complexity.go**

```go
package orchestrator

import (
	"regexp"
	"strings"
)

// TaskComplexity 任务复杂度
type TaskComplexity int

const (
	ComplexitySimple TaskComplexity = iota
	ComplexityComplex
)

// ComplexityDetector 复杂度检测器
type ComplexityDetector struct {
	threshold int
}

func NewComplexityDetector(threshold int) *ComplexityDetector {
	return &ComplexityDetector{threshold: threshold}
}

// Detect 检测任务复杂度
func (d *ComplexityDetector) Detect(request string) TaskComplexity {
	score := 0

	// 读取文件数 > 3: +1分
	if countFiles(request) > 3 {
		score++
	}

	// 关键词检测: +1分
	keywords := []string{"修改", "重构", "新增", "创建", "重写", "更新"}
	for _, kw := range keywords {
		if strings.Contains(request, kw) {
			score++
			break
		}
	}

	if score >= d.threshold {
		return ComplexityComplex
	}
	return ComplexitySimple
}

func countFiles(request string) int {
	re := regexp.MustCompile(`[\w/\\]+\.\w+`)
	return len(re.FindAllString(request, -1))
}
```

- [ ] **Step 2: 提交**

```bash
git add internal/orchestrator/complexity.go
git commit -m "feat(orchestrator): add task complexity detection"
```

---

### Task 8: TUI 集成

**Files:**
- Create: `internal/orchestrator/tui_panel.go`

- [ ] **Step 1: 创建 tui_panel.go**

```go
package orchestrator

import (
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

// TUIPanel TUI 展示面板
type TUIPanel struct {
	viewport viewport.Model
}

func NewTUIPanel() *TUIPanel {
	return &TUIPanel{}
}

// RenderState 渲染当前状态
func (p *TUIPanel) RenderState(state OrchestrationState, output string) string {
	styles := map[OrchestrationState]lipgloss.Style{
		StatePlanning:       lipgloss.NewStyle().Foreground(lipgloss.Color("86")),
		StatePlanReviewing:  lipgloss.NewStyle().Foreground(lipgloss.Color("208")),
		StateCoding:         lipgloss.NewStyle().Foreground(lipgloss.Color("82")),
		StateCodeReviewing:  lipgloss.NewStyle().Foreground(lipgloss.Color("201")),
		StateFinalReview:    lipgloss.NewStyle().Foreground(lipgloss.Color("147")),
		StateDone:           lipgloss.NewStyle().Foreground(lipgloss.Color("82")),
		StatePaused:         lipgloss.NewStyle().Foreground(lipgloss.Color("196")),
	}

	style := styles[state]
	return style.Render(string(state)) + "\n\n" + output
}
```

- [ ] **Step 2: 提交**

```bash
git add internal/orchestrator/tui_panel.go
git commit -m "feat(orchestrator): add TUI panel for state visualization"
```

---

## Phase 5: 测试

### Task 9: 单元测试

**Files:**
- Create: `internal/orchestrator/metrics_test.go`

- [ ] **Step 1: 编写 metrics_test.go**

```go
package orchestrator

import "testing"

func TestScoreCalculator(t *testing.T) {
	calc := NewScoreCalculator()

	dimensions := []DimensionConfig{
		{Name: "维度1", Weight: 0.5},
		{Name: "维度2", Weight: 0.5},
	}
	scores := []float64{80, 60}

	result := calc.Calculate(dimensions, scores)

	expected := 70.0
	if result.TotalScore != expected {
		t.Errorf("expected %f, got %f", expected, result.TotalScore)
	}

	if !result.Passed {
		t.Error("expected passed=true")
	}
}

func TestComplexityDetector(t *testing.T) {
	detector := NewComplexityDetector(2)

	// 测试简单任务
	simple := "读取文件 test.go"
	if detector.Detect(simple) != ComplexitySimple {
		t.Error("expected simple")
	}

	// 测试复杂任务
	complex := "重构 src/utils/helper.go 和 src/models/user.go"
	if detector.Detect(complex) != ComplexityComplex {
		t.Error("expected complex")
	}
}
```

- [ ] **Step 2: 提交**

```bash
git add internal/orchestrator/metrics_test.go
git commit -m "test(orchestrator): add metrics unit tests"
```

---

## 依赖关系

```
Task 1 (state + error_logger)
    ↓
Task 2 (config) ← pkg/config/loader.go
    ↓
Task 3 (metrics)
    ↓
Task 4 (prompts)
    ↓
Task 5 (orchestrator core) ← Task 1, 2, 3
    ↓
Task 6 (agent executors) ← Task 5, Task 4
    ↓
Task 7 (complexity)
    ↓
Task 8 (TUI integration)
    ↓
Task 9 (tests)
```

---

**关键变更说明**

Final Review 不合格时的流程：

```
之前设计:
final_review failed → paused (终止)

现在设计:
final_review failed → 回退到 planning → plan_review → coding → code_review → final_review → ...

直到 final_review 通过或达到 Plan 阶段重试上限
```

这意味着整个系统可能多次完整遍历所有阶段，直到 TaskReviewer 满意为止。
