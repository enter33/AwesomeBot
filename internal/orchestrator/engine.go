package orchestrator

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/enter33/AwesomeBot/internal/agent"
	"github.com/enter33/AwesomeBot/internal/subagent"
	"github.com/enter33/AwesomeBot/internal/workflow"
	"github.com/enter33/AwesomeBot/pkg/config"
)

//go:embed workflows/*
var workflowFS embed.FS

// Engine 通用工作流引擎
type Engine struct {
	workflow    *workflow.Workflow
	nodes       map[string]*workflow.Node
	subagentMgr *subagent.Manager
	mainAgent   *agent.Agent

	currentNode string
	outputs     map[string]string
	retryCounts map[string]int

	userRequest        string
	lastReviewComments string

	userInputCh       chan string
	pendingUserInput  bool
	mainAgentViewCh   chan<- agent.MessageVO
	mu                sync.RWMutex
}

// NewEngine 创建新的工作流引擎
func NewEngine(subagentMgr *subagent.Manager, mainAgent *agent.Agent) (*Engine, error) {
	wfDir, err := ensureDefaultWorkflow()
	if err != nil {
		return nil, fmt.Errorf("failed to ensure default workflow: %w", err)
	}

	wf, nodes, err := workflow.LoadWorkflow(wfDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load default workflow: %w", err)
	}

	return &Engine{
		workflow:    wf,
		nodes:       nodes,
		subagentMgr: subagentMgr,
		mainAgent:   mainAgent,
		outputs:     make(map[string]string),
		retryCounts: make(map[string]int),
		userInputCh: make(chan string, 1),
	}, nil
}

// SetMainAgentViewCh 设置主 agent 的 view channel（供 TUI 展示流式输出）
func (e *Engine) SetMainAgentViewCh(ch chan<- agent.MessageVO) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.mainAgentViewCh = ch
}

func ensureDefaultWorkflow() (string, error) {
	awesomeDir := config.GetAwesomeDir()
	targetDir := filepath.Join(awesomeDir, "workflows", "default_dev")

	if _, err := os.Stat(targetDir); err == nil {
		return targetDir, nil
	}

	err := fs.WalkDir(workflowFS, "workflows/default_dev", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel("workflows/default_dev", path)
		dest := filepath.Join(targetDir, rel)

		if d.IsDir() {
			return os.MkdirAll(dest, 0755)
		}

		data, err := workflowFS.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0644)
	})

	return targetDir, err
}

func (e *Engine) CurrentNode() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.currentNode
}

func (e *Engine) SubmitUserInput(input string) bool {
	e.mu.Lock()
	pending := e.pendingUserInput
	e.mu.Unlock()
	if !pending {
		return false
	}
	select {
	case e.userInputCh <- input:
		e.setPendingInput(false)
		return true
	default:
		return false
	}
}

func (e *Engine) IsPendingInput() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.pendingUserInput
}

func (e *Engine) setPendingInput(v bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pendingUserInput = v
}

func (e *Engine) waitForUserInput(ctx context.Context, updateCh chan<- OrchestratorUpdate, question string) (string, error) {
	e.setPendingInput(true)
	defer e.setPendingInput(false)

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
	case resp := <-e.userInputCh:
		return resp, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (e *Engine) ExecuteAsync(ctx context.Context, request string) <-chan OrchestratorUpdate {
	updateCh := make(chan OrchestratorUpdate, 10)

	go func() {
		defer close(updateCh)

		updateCh <- OrchestratorUpdate{
			State:   StatePlanning,
			Message: "开始编排任务",
		}

		err := e.run(ctx, request, updateCh)

		finalState := StateDone
		if err != nil {
			if err == ErrMaxRetriesExceeded {
				finalState = StatePaused
			} else {
				finalState = stateFromString(e.CurrentNode())
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

func (e *Engine) run(ctx context.Context, request string, updateCh chan<- OrchestratorUpdate) error {
	e.mu.Lock()
	e.userRequest = request
	e.currentNode = e.workflow.EntryNode
	e.outputs = make(map[string]string)
	e.retryCounts = make(map[string]int)
	e.lastReviewComments = ""
	e.mu.Unlock()

	sendUpdate := func(nodeID string, msg string, score *ReviewScore) {
		select {
		case updateCh <- OrchestratorUpdate{
			State:    stateFromString(nodeID),
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
			return ctx.Err()
		default:
		}

		nodeID := e.CurrentNode()
		if nodeID == "done" {
			return nil
		}

		node, ok := e.nodes[nodeID]
		if !ok {
			return &OrchestratorError{fmt.Sprintf("node not found: %s", nodeID)}
		}

		e.mu.RLock()
		retries := e.retryCounts[nodeID]
		e.mu.RUnlock()

		if retries >= node.RetryLimit {
			defaultNext := e.workflow.DefaultTransitions[nodeID]
			if defaultNext == "" {
				return ErrMaxRetriesExceeded
			}
			e.mu.Lock()
			e.retryCounts[nodeID] = 0
			e.currentNode = defaultNext
			e.mu.Unlock()
			sendUpdate(nodeID, fmt.Sprintf("节点 %s 重试次数耗尽，强制继续", nodeID), nil)
			continue
		}

		inputData := map[string]any{
			"UserRequest":        e.userRequest,
			"PreviousOutput":     e.outputs[nodeID],
			"LastReviewComments": e.lastReviewComments,
		}
		renderedInput, err := workflow.RenderInput(node.InputTemplate, inputData)
		if err != nil {
			return err
		}

		output, score, err := e.executeNode(ctx, node, renderedInput)
		if err != nil {
			if isContextErr(err) {
				return err
			}
			e.mu.Lock()
			e.retryCounts[nodeID]++
			e.mu.Unlock()
			sendUpdate(nodeID, fmt.Sprintf("节点 %s 执行错误，准备重试 (%d/%d): %v", nodeID, retries+1, node.RetryLimit, err), nil)
			continue
		}

		e.mu.Lock()
		e.outputs[nodeID] = output
		e.mu.Unlock()

		nextNode := e.decideNextNode(ctx, nodeID, output, score, updateCh, sendUpdate)
		if nextNode == "" {
			continue
		}
		if nextNode == "done" {
			e.mu.Lock()
			e.currentNode = "done"
			e.mu.Unlock()
			return nil
		}

		e.mu.Lock()
		e.currentNode = nextNode
		e.mu.Unlock()
	}
}

func (e *Engine) executeNode(ctx context.Context, node *workflow.Node, input string) (string, *ReviewScore, error) {
	if node.ExecutionMode == workflow.ExecutionModeSubagent {
		if e.subagentMgr == nil {
			return "", nil, fmt.Errorf("subagent manager not available")
		}
		id, err := e.subagentMgr.CreateSubagent(node.Name, node.SubagentType, node.Prompt, input)
		if err != nil {
			return "", nil, err
		}

		resultCh, err := e.subagentMgr.GetResultChannel(id)
		if err != nil {
			return "", nil, err
		}

		select {
		case result := <-resultCh:
			if result.Err != nil {
				return "", nil, result.Err
			}
			score := parseReviewScore(result.Result)
			return result.Result, score, nil
		case <-ctx.Done():
			return "", nil, ctx.Err()
		}
	}

	if node.ExecutionMode == workflow.ExecutionModeMainAgent {
		if e.mainAgent == nil {
			return "", nil, fmt.Errorf("main agent not available")
		}
		return e.executeMainAgentNode(ctx, node, input)
	}

	return "", nil, fmt.Errorf("unsupported execution mode: %s", node.ExecutionMode)
}

func (e *Engine) executeMainAgentNode(ctx context.Context, node *workflow.Node, input string) (string, *ReviewScore, error) {
	viewCh := make(chan agent.MessageVO, 10)
	confirmCh := make(chan agent.ConfirmationAction)
	doneCh := make(chan error, 1)

	// 自动批准所有工具确认（与 subagent 行为保持一致）
	go func() {
		for {
			select {
			case _, ok := <-confirmCh:
				if !ok {
					return
				}
				select {
				case confirmCh <- agent.ConfirmAllow:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// 在后台运行主 agent
	go func() {
		err := e.mainAgent.RunStreaming(ctx, input, viewCh, confirmCh, nil)
		close(viewCh)
		close(confirmCh)
		doneCh <- err
	}()

	var resultBuilder []string
	for msg := range viewCh {
		e.mu.RLock()
		outCh := e.mainAgentViewCh
		e.mu.RUnlock()
		if outCh != nil {
			select {
			case outCh <- msg:
			default:
			}
		}
		if msg.Type == agent.MessageTypeContent && msg.Content != nil {
			resultBuilder = append(resultBuilder, *msg.Content)
		}
	}

	err := <-doneCh
	result := strings.Join(resultBuilder, "")

	// review 节点解析评分，其他节点返回 nil score
	var score *ReviewScore
	if node.OutputFormat == "review_score" {
		score = parseReviewScore(result)
	}

	return result, score, err
}

func (e *Engine) decideNextNode(ctx context.Context, nodeID, output string, score *ReviewScore, updateCh chan<- OrchestratorUpdate, sendUpdate func(string, string, *ReviewScore)) string {
	switch nodeID {
	case "plan":
		if questions := extractClarification(output); questions != "" {
			answer, err := e.waitForUserInput(ctx, updateCh,
				fmt.Sprintf("PlanAgent 在制定计划前需要澄清以下问题：\n%s\n\n请直接回复你的答案（可以简化回答）。", questions))
			if err != nil {
				return "done"
			}
			e.mu.Lock()
			e.userRequest += "\n[用户澄清]\n" + strings.TrimSpace(answer)
			e.retryCounts["plan"]++
			e.mu.Unlock()
			return ""
		}
		return e.workflow.DefaultTransitions["plan"]

	case "plan_review":
		if score != nil && score.Passed {
			answer, err := e.waitForUserInput(ctx, updateCh,
				fmt.Sprintf("计划已生成并通过初步审查（评分: %.0f），是否确认执行？\n\n计划内容:\n%s\n\n请输入 y/确认 执行，或输入修改意见重新制定计划。", score.TotalScore, e.outputs["plan"]))
			if err != nil {
				return "done"
			}
			ans := strings.ToLower(strings.TrimSpace(answer))
			if ans == "y" || ans == "yes" || ans == "确认" || ans == "好" || ans == "ok" {
				return e.workflow.DefaultTransitions["plan_review"]
			}
			e.mu.Lock()
			e.userRequest += "\n[用户对计划的修改意见]\n" + answer
			e.lastReviewComments = "用户确认时提出修改意见: " + answer
			e.retryCounts["plan"]++
			e.currentNode = "plan"
			e.mu.Unlock()
			sendUpdate("plan", "用户要求修改计划，返回重新制定", nil)
			return ""
		}

		e.mu.Lock()
		if score != nil {
			e.lastReviewComments = score.Comments
		}
		e.retryCounts["plan"]++
		e.currentNode = "plan"
		e.mu.Unlock()
		sendUpdate("plan", "计划审查未通过，返回修改", score)
		return ""

	case "code":
		return e.workflow.DefaultTransitions["code"]

	case "code_review":
		if score != nil && score.Passed {
			return e.workflow.DefaultTransitions["code_review"]
		}
		e.mu.Lock()
		if score != nil {
			e.lastReviewComments = score.Comments
		}
		e.retryCounts["code"]++
		e.currentNode = "code"
		e.mu.Unlock()
		sendUpdate("code", "代码审查未通过，返回修改", score)
		return ""

	case "final_review":
		if score != nil && score.Passed {
			return "done"
		}
		e.mu.Lock()
		e.outputs["plan"] = ""
		e.outputs["code"] = ""
		e.retryCounts["plan"] = 0
		e.retryCounts["code"] = 0
		e.lastReviewComments = ""
		if score != nil {
			e.lastReviewComments = "最终验收未通过: " + score.Comments
		}
		e.currentNode = "plan"
		e.mu.Unlock()
		sendUpdate("plan", "最终验收未通过，回退到计划阶段重新开始", score)
		return ""
	}

	return e.workflow.DefaultTransitions[nodeID]
}

func stateFromString(s string) OrchestrationState {
	switch s {
	case "plan":
		return StatePlanning
	case "plan_review":
		return StatePlanReviewing
	case "code":
		return StateCoding
	case "code_review":
		return StateCodeReviewing
	case "final_review":
		return StateFinalReview
	case "done":
		return StateDone
	default:
		return StatePending
	}
}

func parseReviewScore(output string) *ReviewScore {
	re := regexp.MustCompile(`总分[:：]\s*(\d+)`)
	matches := re.FindStringSubmatch(output)
	var score float64
	if len(matches) >= 2 {
		score, _ = strconv.ParseFloat(matches[1], 64)
	}

	passed := false
	if strings.Contains(output, "通过与否: 是") || strings.Contains(output, "通过与否:是") {
		passed = true
	}

	return &ReviewScore{
		TotalScore: score,
		Passed:     passed,
		Comments:   extractComments(output),
	}
}

func extractComments(output string) string {
	re := regexp.MustCompile(`(?s)((质疑点|问题列表|改进建议|验收结论|通过与否).*)`)
	match := re.FindString(output)
	if len(match) > 500 {
		return match[:500] + "..."
	}
	return match
}

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
