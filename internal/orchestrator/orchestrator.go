package orchestrator

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/enter33/AwesomeBot/internal/logging"
	"github.com/enter33/AwesomeBot/internal/subagent"
	orchestratorPrompt "github.com/enter33/AwesomeBot/internal/orchestrator/prompt"
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

// Orchestrator 主编排器
type Orchestrator struct {
	state        OrchestrationState
	config       config.MultiAgentConfig
	subagentMgr  *subagent.Manager
	errorLogger  *ErrorLogger
	scoreCalc    *ScoreCalculator
	promptLoader *orchestratorPrompt.Loader
	complexity   *ComplexityDetector

	mu          sync.RWMutex
	planRetry   int // Plan 阶段重试计数
	codeRetry   int // Code 阶段重试计数
	totalRetry  int // 全局重试计数

	userRequest            string
	plan                   string
	codeResult             string
	lastPlanReviewComments string
	lastCodeReviewComments string

	userInputCh       chan string
	pendingUserInput  bool
}

// NewOrchestrator 创建编排器
func NewOrchestrator(
	cfg config.MultiAgentConfig,
	subagentMgr *subagent.Manager,
) *Orchestrator {
	promptPath := filepath.Join("internal", "orchestrator", "prompt")
	return &Orchestrator{
		state:        StatePending,
		config:       cfg,
		subagentMgr:  subagentMgr,
		errorLogger:  NewErrorLogger(),
		scoreCalc:    NewScoreCalculator(),
		promptLoader: orchestratorPrompt.NewLoader(promptPath),
		complexity:   NewComplexityDetector(cfg.ComplexityThreshold),
		userInputCh:  make(chan string, 1),
	}
}

// SetPromptLoader 设置提示词加载器（用于测试）
func (o *Orchestrator) SetPromptLoader(loader *orchestratorPrompt.Loader) {
	o.promptLoader = loader
}

// getState 获取当前状态
func (o *Orchestrator) getState() OrchestrationState {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.state
}

// setState 设置当前状态
func (o *Orchestrator) setState(state OrchestrationState) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.state = state
}

// SubmitUserInput 提交用户输入（由 TUI 调用）
func (o *Orchestrator) SubmitUserInput(input string) bool {
	o.mu.Lock()
	pending := o.pendingUserInput
	o.mu.Unlock()
	if !pending {
		return false
	}
	select {
	case o.userInputCh <- input:
		o.setPendingInput(false)
		return true
	default:
		return false
	}
}

// IsPendingInput 是否正在等待用户输入
func (o *Orchestrator) IsPendingInput() bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.pendingUserInput
}

func (o *Orchestrator) setPendingInput(v bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.pendingUserInput = v
}

func (o *Orchestrator) waitForUserInput(ctx context.Context, updateCh chan<- OrchestratorUpdate, question string) (string, error) {
	o.setPendingInput(true)
	defer o.setPendingInput(false)

	select {
	case updateCh <- OrchestratorUpdate{
		State:          StatePaused,
		NeedsUserInput: true,
		Question:       question,
	}:
	case <-ctx.Done():
		return "", ctx.Err()
	}

	select {
	case resp := <-o.userInputCh:
		return resp, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
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
	return o.getState()
}

// ExecuteAsync 异步执行编排流程，返回状态更新 channel
func (o *Orchestrator) ExecuteAsync(ctx context.Context, request string) <-chan OrchestratorUpdate {
	updateCh := make(chan OrchestratorUpdate, 10)

	go func() {
		defer close(updateCh)

		// 发送初始状态
		updateCh <- OrchestratorUpdate{
			State:   StatePlanning,
			Message: "开始编排任务",
		}

		// 执行主流程
		err := o.executeWithUpdates(ctx, request, updateCh)

		// 发送最终状态
		finalState := StateDone
		if err != nil {
			if err == ErrMaxRetriesExceeded {
				finalState = StatePaused
			} else {
				finalState = o.getState()
			}
		}

		updateCh <- OrchestratorUpdate{
			State:   finalState,
			Message: "编排结束",
			IsFinal: true,
			Error:   err,
		}
	}()

	return updateCh
}

// executeWithUpdates 带状态更新的执行流程
func (o *Orchestrator) executeWithUpdates(ctx context.Context, request string, updateCh chan<- OrchestratorUpdate) error {
	o.mu.Lock()
	o.state = StatePlanning
	o.userRequest = request
	o.plan = ""
	o.codeResult = ""
	o.lastPlanReviewComments = ""
	o.lastCodeReviewComments = ""
	o.planRetry = 0
	o.codeRetry = 0
	o.totalRetry = 0
	o.mu.Unlock()

	sendUpdate := func(state OrchestrationState, msg string, score *ReviewScore) {
		select {
		case updateCh <- OrchestratorUpdate{
			State:    state,
			Message:  msg,
			Score:    score,
			PhaseLog: msg,
		}:
		case <-ctx.Done():
		}
	}

	for {
		select {
		case <-ctx.Done():
			o.setState(StatePaused)
			return ctx.Err()
		default:
		}

		// 检查全局重试上限
		if o.totalRetry >= o.config.RetryLimits.MaxTotalRetries {
			o.setState(StatePaused)
			return ErrMaxRetriesExceeded
		}

		switch o.getState() {
		case StatePlanning:
			plan, err := o.runPlanningPhase(ctx, updateCh)
			if err != nil {
				if isContextErr(err) {
					return err
				}
				if o.planRetry >= o.config.RetryLimits.PlanPhase {
					o.setState(StatePaused)
					return ErrMaxRetriesExceeded
				}
				o.setState(StatePlanning)
				msg := fmt.Sprintf("计划阶段失败，准备重试 (%d/%d)", o.planRetry, o.config.RetryLimits.PlanPhase)
				if rerr, ok := err.(*ReviewNotPassedError); ok {
					msg = fmt.Sprintf("计划审查未通过，准备重试 (%d/%d): %s", o.planRetry, o.config.RetryLimits.PlanPhase, rerr.Comments)
				} else if err.Error() == "clarification needed" {
					msg = fmt.Sprintf("已收到用户澄清，重新制定计划 (%d/%d)", o.planRetry, o.config.RetryLimits.PlanPhase)
				} else {
					msg = fmt.Sprintf("计划阶段执行错误，准备重试 (%d/%d): %v", o.planRetry, o.config.RetryLimits.PlanPhase, err)
				}
				sendUpdate(StatePlanning, msg, nil)
				continue
			}
			o.plan = plan
			sendUpdate(StateCoding, "计划通过，进入编码阶段", nil)

		case StateCoding:
			result, err := o.runCodingPhase(ctx, o.plan)
			if err != nil {
				if isContextErr(err) {
					return err
				}
				if o.codeRetry >= o.config.RetryLimits.CodePhase {
					o.setState(StatePaused)
					return ErrMaxRetriesExceeded
				}
				o.setState(StateCoding)
				msg := fmt.Sprintf("编码阶段失败，准备重试 (%d/%d)", o.codeRetry, o.config.RetryLimits.CodePhase)
				if rerr, ok := err.(*ReviewNotPassedError); ok {
					msg = fmt.Sprintf("代码审查未通过，准备重试 (%d/%d): %s", o.codeRetry, o.config.RetryLimits.CodePhase, rerr.Comments)
				} else {
					msg = fmt.Sprintf("编码阶段执行错误，准备重试 (%d/%d): %v", o.codeRetry, o.config.RetryLimits.CodePhase, err)
				}
				sendUpdate(StateCoding, msg, nil)
				continue
			}
			o.codeResult = result
			sendUpdate(StateFinalReview, "编码通过，进入最终验收", nil)

		case StateFinalReview:
			passed, score, err := o.runFinalPhase(ctx, o.codeResult)
			if err != nil {
				if isContextErr(err) {
					return err
				}
				return err
			}
			if !passed {
				// Final Review 不合格 -> 回退到 Planning 阶段重新开始
				o.resetForReplan()
				o.setState(StatePlanning)
				msg := "最终验收未通过，回退到计划阶段重新开始"
				if score != nil {
					msg += fmt.Sprintf(" (评分: %.0f)", score.TotalScore)
				}
				sendUpdate(StatePlanning, msg, score)
				continue
			}
			o.setState(StateDone)
			return nil

		case StatePaused:
			return ErrMaxRetriesExceeded

		default:
			return &OrchestratorError{fmt.Sprintf("unexpected state: %s", o.getState())}
		}
	}
}

// resetForReplan 重置状态用于重新规划
func (o *Orchestrator) resetForReplan() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.plan = ""
	o.codeResult = ""
	o.planRetry = 0
	o.codeRetry = 0
	o.totalRetry++
}

// runPlanningPhase 执行计划阶段
func (o *Orchestrator) runPlanningPhase(ctx context.Context, updateCh chan<- OrchestratorUpdate) (string, error) {
	o.setState(StatePlanning)

	// 1. 执行 PlanAgent
	plan, err := o.executePlanAgent(ctx)
	if err != nil {
		if isContextErr(err) {
			return "", err
		}
		o.handleError(StatePlanning, err)
		o.planRetry++
		if o.planRetry >= o.config.RetryLimits.PlanPhase {
			logging.Warn("PlanAgent 执行多次失败，强制结束计划阶段")
			return "", err
		}
		return "", err
	}

	// 检查是否需要用户澄清
	if questions := extractClarification(plan); questions != "" {
		answer, err := o.waitForUserInput(ctx, updateCh,
			fmt.Sprintf("PlanAgent 在制定计划前需要澄清以下问题：\n%s\n\n请直接回复你的答案（可以简化回答）。", questions))
		if err != nil {
			return "", err
		}
		o.userRequest += "\n[用户澄清]\n" + strings.TrimSpace(answer)
		o.planRetry++ // 不算严格失败，但消耗一次重试机会
		return "", fmt.Errorf("clarification needed")
	}

	// 2. 执行 PlanReviewer
	o.setState(StatePlanReviewing)
	score, err := o.executePlanReviewer(ctx, plan)
	if err != nil {
		if isContextErr(err) {
			return "", err
		}
		o.handleError(StatePlanReviewing, err)
		o.planRetry++
		if o.planRetry >= o.config.RetryLimits.PlanPhase {
			logging.Warn("PlanReviewer 重试次数耗尽，强制使用当前计划继续")
			o.setState(StateCoding)
			return plan + "\n\n[注: PlanReviewer 多次未能通过，携带可能的风险继续执行]", nil
		}
		return "", err
	}

	// 3. 检查是否通过
	if score.Passed {
		// 请求用户确认计划
		answer, err := o.waitForUserInput(ctx, updateCh,
			fmt.Sprintf("计划已生成并通过初步审查（评分: %.0f），是否确认执行？\n\n计划内容:\n%s\n\n请输入 y/确认 执行，或输入修改意见重新制定计划。", score.TotalScore, plan))
		if err != nil {
			return "", err
		}
		ans := strings.ToLower(strings.TrimSpace(answer))
		if ans == "y" || ans == "yes" || ans == "确认" || ans == "好" || ans == "ok" {
			o.setState(StateCoding)
			return plan, nil
		}
		// 用户有修改意见
		o.userRequest += "\n[用户对计划的修改意见]\n" + answer
		o.lastPlanReviewComments = "用户确认时提出修改意见: " + answer
		o.planRetry++
		return "", &ReviewNotPassedError{Comments: "用户要求修改计划: " + answer}
	}

	// 4. 打回重做
	o.planRetry++
	o.lastPlanReviewComments = score.Comments
	logging.Info("PlanReviewer 打回计划，评分: %.0f, 意见: %s", score.TotalScore, score.Comments)

	if o.planRetry >= o.config.RetryLimits.PlanPhase {
		logging.Warn("PlanReviewer 重试次数耗尽，强制使用当前计划继续")
		o.setState(StateCoding)
		return plan + "\n\n[注: PlanReviewer 多次未能通过，携带以下审查意见继续执行: " + score.Comments + "]", nil
	}

	return "", &ReviewNotPassedError{score.Comments}
}

// runCodingPhase 执行编码阶段
func (o *Orchestrator) runCodingPhase(ctx context.Context, plan string) (string, error) {
	o.setState(StateCoding)

	// 1. 执行 CodingAgent
	result, err := o.executeCodingAgent(ctx, plan)
	if err != nil {
		if isContextErr(err) {
			return "", err
		}
		o.handleError(StateCoding, err)
		o.codeRetry++
		if o.codeRetry >= o.config.RetryLimits.CodePhase {
			logging.Warn("CodingAgent 执行多次失败，强制结束编码阶段")
			return "", err
		}
		return "", err
	}

	// 2. 执行 CodeReviewer
	o.setState(StateCodeReviewing)
	score, err := o.executeCodeReviewer(ctx, result)
	if err != nil {
		if isContextErr(err) {
			return "", err
		}
		o.handleError(StateCodeReviewing, err)
		o.codeRetry++
		if o.codeRetry >= o.config.RetryLimits.CodePhase {
			logging.Warn("CodeReviewer 重试次数耗尽，强制使用当前代码结果继续")
			o.setState(StateFinalReview)
			return result + "\n\n[注: CodeReviewer 多次未能通过，携带可能的风险继续执行]", nil
		}
		return "", err
	}

	// 3. 检查是否通过
	if score.Passed {
		o.setState(StateFinalReview)
		return result, nil
	}

	// 4. 打回重做
	o.codeRetry++
	o.lastCodeReviewComments = score.Comments
	logging.Info("CodeReviewer 打回代码，评分: %.0f, 意见: %s", score.TotalScore, score.Comments)

	if o.codeRetry >= o.config.RetryLimits.CodePhase {
		logging.Warn("CodeReviewer 重试次数耗尽，强制使用当前代码结果继续")
		o.setState(StateFinalReview)
		return result + "\n\n[注: CodeReviewer 多次未能通过，携带以下审查意见继续执行: " + score.Comments + "]", nil
	}

	return "", &ReviewNotPassedError{score.Comments}
}

// runFinalPhase 执行最终验收
func (o *Orchestrator) runFinalPhase(ctx context.Context, codeResult string) (bool, *ReviewScore, error) {
	o.setState(StateFinalReview)

	score, err := o.executeTaskReviewer(ctx, codeResult)
	if err != nil {
		o.handleError(StateFinalReview, err)
		return false, nil, err
	}

	return score.Passed, score, nil
}

// executePlanAgent 执行 PlanAgent
func (o *Orchestrator) executePlanAgent(ctx context.Context) (string, error) {
	prompt, err := o.promptLoader.Load("planner")
	if err != nil {
		return "", err
	}

	task := o.userRequest
	if o.lastPlanReviewComments != "" {
		task = fmt.Sprintf("%s\n\n【上次审查意见（请针对以下问题改进计划）】\n%s", o.userRequest, o.lastPlanReviewComments)
	}

	id, err := o.subagentMgr.CreateSubagent(
		"planner",
		subagent.SubagentTypePlan,
		prompt,
		task,
	)
	if err != nil {
		return "", err
	}

	resultCh, err := o.subagentMgr.GetResultChannel(id)
	if err != nil {
		return "", err
	}

	select {
	case result := <-resultCh:
		return result.Result, result.Err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

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

	resultCh, err := o.subagentMgr.GetResultChannel(id)
	if err != nil {
		return nil, err
	}

	select {
	case result := <-resultCh:
		return o.parseReviewScore(result.Result, PlanReviewerDimensions, o.config.Thresholds.PlanReviewer)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// executeCodingAgent 执行 CodingAgent
func (o *Orchestrator) executeCodingAgent(ctx context.Context, plan string) (string, error) {
	prompt, err := o.promptLoader.Load("coder")
	if err != nil {
		return "", err
	}

	input := fmt.Sprintf("用户需求:\n%s\n\n执行计划:\n%s", o.userRequest, plan)
	if o.lastCodeReviewComments != "" {
		input = fmt.Sprintf("%s\n\n【上次审查意见（请针对以下问题改进代码）】\n%s", input, o.lastCodeReviewComments)
	}

	id, err := o.subagentMgr.CreateSubagent(
		"coder",
		subagent.SubagentTypeGeneral,
		prompt,
		input,
	)
	if err != nil {
		return "", err
	}

	resultCh, err := o.subagentMgr.GetResultChannel(id)
	if err != nil {
		return "", err
	}

	select {
	case result := <-resultCh:
		return result.Result, result.Err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

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

	resultCh, err := o.subagentMgr.GetResultChannel(id)
	if err != nil {
		return nil, err
	}

	select {
	case result := <-resultCh:
		return o.parseReviewScore(result.Result, CodeReviewerDimensions, o.config.Thresholds.CodeReviewer)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// executeTaskReviewer 执行 TaskReviewer
func (o *Orchestrator) executeTaskReviewer(ctx context.Context, codeResult string) (*ReviewScore, error) {
	prompt, err := o.promptLoader.Load("final_reviewer")
	if err != nil {
		return nil, err
	}

	input := fmt.Sprintf("用户需求:\n%s\n\n最终结果:\n%s", o.userRequest, codeResult)

	id, err := o.subagentMgr.CreateSubagent(
		"final_reviewer",
		subagent.SubagentTypePlan,
		prompt,
		input,
	)
	if err != nil {
		return nil, err
	}

	resultCh, err := o.subagentMgr.GetResultChannel(id)
	if err != nil {
		return nil, err
	}

	select {
	case result := <-resultCh:
		return o.parseReviewScore(result.Result, TaskReviewerDimensions, o.config.Thresholds.TaskReviewer)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// parseReviewScore 从 Reviewer 输出解析评分
func (o *Orchestrator) parseReviewScore(output string, dimensions []DimensionConfig, threshold float64) (*ReviewScore, error) {
	// 从输出中提取 "总分: XX/100" 或 "总分: XX"
	re := regexp.MustCompile(`总分[:：]\s*(\d+)`)
	matches := re.FindStringSubmatch(output)

	var score float64
	if len(matches) >= 2 {
		score, _ = strconv.ParseFloat(matches[1], 64)
	} else {
		score = 50
		logging.Warn("无法从 Reviewer 输出解析评分，使用默认值 50")
	}

	return &ReviewScore{
		TotalScore: score,
		Dimensions: nil,
		Passed:     score >= threshold,
		Comments:   extractComments(output),
	}, nil
}

func extractComments(output string) string {
	// 提取质疑点、问题列表、改进建议、通过与否等部分
	re := regexp.MustCompile(`(?s)((质疑点|问题列表|改进建议|验收结论|通过与否).*)`)
	match := re.FindString(output)
	if len(match) > 500 {
		return match[:500] + "..."
	}
	return match
}

// handleError 处理阶段执行错误
func (o *Orchestrator) handleError(phase OrchestrationState, err error) {
	o.errorLogger.Log(ErrorLog{
		Phase:       string(phase),
		ErrorType:   classifyError(err),
		ErrorDetail: err.Error(),
		Timestamp:   time.Now(),
		RetryCount:  o.totalRetry,
	})
}

func classifyError(err error) ErrorType {
	if err == nil {
		return ErrorTypeUnknown
	}
	errStr := strings.ToLower(err.Error())
	switch {
	case strings.Contains(errStr, "context") && strings.Contains(errStr, "deadline"):
		return ErrorTypeContextOverflow
	case strings.Contains(errStr, "tool"):
		return ErrorTypeToolFailed
	case strings.Contains(errStr, "llm") || strings.Contains(errStr, "api"):
		return ErrorTypeLLMFailed
	default:
		return ErrorTypeUnknown
	}
}

func isContextErr(err error) bool {
	if err == nil {
		return false
	}
	return err == context.Canceled || err == context.DeadlineExceeded || strings.Contains(err.Error(), "context canceled")
}

// extractClarification 从 PlanAgent 输出中提取需要澄清的问题
func extractClarification(output string) string {
	re := regexp.MustCompile(`(?is)CLARIFICATION_NEEDED[:：]?\s*(.*)`)
	matches := re.FindStringSubmatch(output)
	if len(matches) >= 2 {
		q := strings.TrimSpace(matches[1])
		if len(q) > 800 {
			q = q[:800] + "..."
		}
		return q
	}
	return ""
}
