package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/enter33/AwesomeBot/internal/agent"
	"github.com/enter33/AwesomeBot/internal/logging"
	"github.com/enter33/AwesomeBot/internal/subagent"
)

type streamMsg struct {
	event agent.MessageVO
}

type streamClosedMsg struct{}

type streamDoneMsg struct {
	err error
}

// subagentCompletionMsg 子代理完成消息
type subagentCompletionMsg struct {
	subagentID   string
	subagentName string
	status       subagent.SubagentStatus
	result       string
	err          error
}

// subagentStreamMsg 子代理流消息
type subagentStreamMsg struct {
	subagentID   string
	subagentName string
	event        agent.MessageVO
}

type runState int

const (
	stateIdle runState = iota
	stateRunning
	stateAborting
	stateAwaitingConfirmation
	stateSubagentRunning
)

type activeStream struct {
	events    <-chan agent.MessageVO
	cancel    context.CancelFunc
	confirmCh chan agent.ConfirmationAction

	turnLogLen  int
	reasonBody  int
	contentBody int
	policyBody  int // 当前策略 log entry 的索引
	memoryBody  int // 当前记忆更新 log entry 的索引
}

// subagentOutput 子代理输出缓冲区
type subagentOutput struct {
	lines      []string
	maxLines   int
	lastLogIdx int // 在 m.logs 中的索引，-1 表示未添加
}

func newSubagentOutput(maxLines int) *subagentOutput {
	return &subagentOutput{
		lines:      make([]string, 0, maxLines),
		maxLines:   maxLines,
		lastLogIdx: -1,
	}
}

func (so *subagentOutput) add(line string) {
	so.lines = append(so.lines, line)
	if len(so.lines) > so.maxLines {
		so.lines = so.lines[len(so.lines)-so.maxLines:]
	}
}

func (so *subagentOutput) getContent() string {
	return strings.Join(so.lines, "\n")
}

type TuiViewModel struct {
	modelName string
	version   string
	agent     *agent.Agent

	input string
	logs  []LogEntry

	state  runState
	active *activeStream

	// 工具确认相关
	confirmOptions     []string
	selectedConfirmIdx int

	notice string

	width  int
	height int

	logsViewport viewport.Model

	// Subagent 相关
	subagentManager *subagent.Manager
	subagentOutputs map[string]*subagentOutput // 子代理输出缓冲区
}

var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	labelStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("69"))
	noticeStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	footerStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	borderStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	contentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
)

func NewModel(agent *agent.Agent, modelName, version string) *TuiViewModel {
	return NewModelWithSubagentManager(agent, nil, modelName, version)
}

func NewModelWithSubagentManager(agent *agent.Agent, subagentMgr *subagent.Manager, modelName, version string) *TuiViewModel {
	vp := viewport.New()
	vp.SoftWrap = true
	vp.MouseWheelEnabled = false

	return &TuiViewModel{
		modelName:          modelName,
		version:            version,
		agent:              agent,
		logs:               make([]LogEntry, 0),
		logsViewport:       vp,
		confirmOptions:     []string{"允许", "拒绝", "始终允许"},
		selectedConfirmIdx: 0,
		subagentManager:    subagentMgr,
		subagentOutputs:    make(map[string]*subagentOutput),
	}
}

func (m *TuiViewModel) Init() tea.Cmd {
	// 启动子代理消息监听
	return m.startSubagentListener()
}

func waitStreamEvent(ch <-chan agent.MessageVO) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return streamClosedMsg{}
		}
		return streamMsg{event: msg}
	}
}

func waitStreamDone(ch <-chan error) tea.Cmd {
	return func() tea.Msg {
		err, ok := <-ch
		if !ok {
			return streamDoneMsg{}
		}
		return streamDoneMsg{err: err}
	}
}

func (m *TuiViewModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.syncLogsViewportSize()
		m.refreshLogsViewportContentAfterResize()
		return m, nil
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			m.scrollUp(3)
		case tea.MouseWheelDown:
			m.scrollDown(3)
		}
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.PasteMsg:
		// Handle paste from clipboard
		if m.state == stateIdle {
			m.input += msg.Content
		}
		return m, nil
	case streamMsg:
		return m.handleStreamMsg(msg)
	case streamClosedMsg:
		// channel 已关闭，等待 streamDoneMsg 到来
		return m, nil
	case streamDoneMsg:
		return m.handleStreamDone(msg)
	case subagentStreamMsg:
		return m.handleSubagentStreamMsg(msg)
	case subagentCompletionMsg:
		return m.handleSubagentCompletion(msg)
	}
	return m, nil
}

func (m *TuiViewModel) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// subagent 运行时不允许输入，只允许 ctrl+c 终止 subagent
	if m.state == stateSubagentRunning {
		if msg.String() == "ctrl+c" {
			m.stopAllSubagents()
			m.state = stateIdle
			m.refreshLogsViewportContent()
		}
		return m, nil
	}

	switch msg.String() {
	case "ctrl+c":
		m.stopActiveStream()
		return m, tea.Quit
	case "up":
		if m.state == stateAwaitingConfirmation {
			m.selectedConfirmIdx = (m.selectedConfirmIdx - 1 + len(m.confirmOptions)) % len(m.confirmOptions)
			return m, nil
		}
		m.scrollUp(1)
		return m, nil
	case "down":
		if m.state == stateAwaitingConfirmation {
			m.selectedConfirmIdx = (m.selectedConfirmIdx + 1) % len(m.confirmOptions)
			return m, nil
		}
		m.scrollDown(1)
		return m, nil
	case "pgup":
		m.scrollUp(m.logsViewportHeight())
		return m, nil
	case "pgdown":
		m.scrollDown(m.logsViewportHeight())
		return m, nil
	case "home":
		m.logsViewport.GotoTop()
		return m, nil
	case "end":
		m.logsViewport.GotoBottom()
		return m, nil
	case "enter":
		if m.state == stateAwaitingConfirmation {
			return m.handleConfirmSelection()
		}
		return m.handleSubmit()
	case "esc":
		if m.state == stateAwaitingConfirmation {
			// 拒绝并退出
			if m.active != nil && m.active.confirmCh != nil {
				m.active.confirmCh <- agent.ConfirmReject
			}
			m.state = stateAborting
			return m, nil
		}
		m.abortCurrentTurn()
		return m, nil
	case "backspace":
		if len(m.input) > 0 {
			r := []rune(m.input)
			m.input = string(r[:len(r)-1])
		}
		return m, nil
	}

	if m.state != stateIdle {
		return m, nil
	}

	if key := msg.Key(); key.Text != "" {
		m.input += key.Text
	}
	return m, nil
}

func (m *TuiViewModel) handleSubmit() (tea.Model, tea.Cmd) {
	query := strings.TrimSpace(m.input)
	if query == "" {
		return m, nil
	}

	// 检查是否有 subagent 正在运行
	if m.subagentManager != nil {
		streams := m.subagentManager.ListStreams()
		hasRunning := false
		for _, s := range streams {
			if s.Status == subagent.StatusRunning {
				hasRunning = true
				break
			}
		}
		if hasRunning {
			m.notice = "有子代理正在运行，请等待完成后再输入。"
			m.refreshLogsViewportContent()
			return m, nil
		}
	}

	if m.state != stateIdle {
		return m, nil
	}

	m.input = ""
	if query == "/clear" {
		m.clearSession()
		return m, nil
	}

	return m.startNewTurn(query)
}

func (m *TuiViewModel) handleConfirmSelection() (tea.Model, tea.Cmd) {
	if m.active == nil || m.active.confirmCh == nil {
		return m, nil
	}

	var action agent.ConfirmationAction
	switch m.selectedConfirmIdx {
	case 0:
		action = agent.ConfirmAllow
	case 1:
		action = agent.ConfirmReject
	case 2:
		action = agent.ConfirmAlwaysAllow
	}

	go func() {
		m.active.confirmCh <- action
	}()

	m.state = stateRunning
	m.selectedConfirmIdx = 0
	return m, nil
}

func (m *TuiViewModel) handleStreamEvent(event agent.MessageVO) {
	// TokenUsage 消息在流结束时发送，即使 m.active 为 nil 也应该处理
	if event.Type == agent.MessageTypeTokenUsage {
		if event.TokenUsage != nil {
			m.logs = append(m.logs, NewTokenUsage(
				event.TokenUsage.PromptTokens,
				event.TokenUsage.CompletionTokens,
				event.TokenUsage.TotalTokens,
				event.TokenUsage.Speed,
				event.TokenUsage.Duration,
			))
			m.refreshLogsViewportContent()
		}
		return
	}

	if m.active == nil || m.state == stateAborting {
		return
	}

	switch event.Type {
	case agent.MessageTypeReasoning:
		if event.ReasoningContent == nil {
			return
		}
		if m.active.reasonBody == -1 {
			m.logs = append(m.logs, NewReasoning(*event.ReasoningContent))
			m.active.reasonBody = len(m.logs) - 1
		} else if m.active.reasonBody >= 0 && m.active.reasonBody < len(m.logs) {
			m.logs[m.active.reasonBody].AppendContent(*event.ReasoningContent)
		}
	case agent.MessageTypeContent:
		if event.Content == nil {
			return
		}
		// 如果内容为空，不创建回答块
		if *event.Content == "" {
			return
		}
		if m.active.contentBody == -1 {
			m.logs = append(m.logs, NewAnswer(*event.Content))
			m.active.contentBody = len(m.logs) - 1
		} else if m.active.contentBody >= 0 && m.active.contentBody < len(m.logs) {
			m.logs[m.active.contentBody].AppendContent(*event.Content)
		}
	case agent.MessageTypeToolCall:
		if event.ToolCall == nil {
			return
		}
		m.logs = append(m.logs, NewTool(fmt.Sprintf("%s(%s)", event.ToolCall.Name, event.ToolCall.Arguments)))
		m.resetOutputSection()
	case agent.MessageTypeError:
		if event.Content == nil {
			return
		}
		m.logs = append(m.logs, NewError(*event.Content))
		m.resetOutputSection()
	case agent.MessageTypeToolConfirm:
		if event.ToolConfirmationRequest == nil {
			return
		}
		m.logs = append(m.logs, NewToolConfirmation(event.ToolConfirmationRequest.ToolName, event.ToolConfirmationRequest.Arguments))
		m.state = stateAwaitingConfirmation
		m.selectedConfirmIdx = 0
	case agent.MessageTypePolicy:
		if event.Policy == nil {
			return
		}
		if event.Policy.Running {
			// 策略开始：添加新的 log entry
			m.logs = append(m.logs, NewPolicyRunning(event.Policy.Name))
			m.active.policyBody = len(m.logs) - 1
		} else {
			// 策略结束：更新对应的 log entry
			if m.active.policyBody >= 0 && m.active.policyBody < len(m.logs) {
				m.logs[m.active.policyBody].UpdatePolicyCompleted(event.Policy.Error == nil)
			}
			m.active.policyBody = -1
		}
		m.refreshLogsViewportContent()
	case agent.MessageTypeMemory:
		if event.Memory == nil {
			return
		}
		if event.Memory.Running {
			// 记忆更新开始：添加新的 log entry
			m.logs = append(m.logs, NewMemoryRunning())
			m.active.memoryBody = len(m.logs) - 1
		} else {
			// 记忆更新结束：更新对应的 log entry
			if m.active.memoryBody >= 0 && m.active.memoryBody < len(m.logs) {
				m.logs[m.active.memoryBody].UpdateMemoryCompleted(event.Memory.Error == nil)
			}
			m.active.memoryBody = -1
		}
		m.refreshLogsViewportContent()
	}
}

func (m *TuiViewModel) resetOutputSection() {
	if m.active == nil {
		return
	}
	m.active.reasonBody = -1
	m.active.contentBody = -1
	// 注意：不重置 policyBody 和 memoryBody，因为状态需要保留
}

func (m *TuiViewModel) handleStreamMsg(msg streamMsg) (tea.Model, tea.Cmd) {
	// TokenUsage 消息即使在 m.active 为 nil 时也应该处理
	if msg.event.Type == agent.MessageTypeTokenUsage {
		m.handleStreamEvent(msg.event)
		m.refreshLogsViewportContent()
		return m, nil
	}

	if m.active == nil || m.active.events == nil {
		return m, nil
	}
	m.handleStreamEvent(msg.event)
	m.refreshLogsViewportContent()
	return m, waitStreamEvent(m.active.events)
}

func (m *TuiViewModel) handleStreamDone(msg streamDoneMsg) (tea.Model, tea.Cmd) {
	m.stopActiveStream()
	m.state = stateIdle
	m.refreshLogsViewportContent()

	if msg.err != nil {
		m.logs = append(m.logs, NewError(msg.err.Error()))
	}
	m.logs = append(m.logs, NewBorder())

	return m, nil
}

// startSubagentListener 启动子代理消息监听协程
func (m *TuiViewModel) startSubagentListener() tea.Cmd {
	return func() tea.Msg {
		for {
			if m.subagentManager == nil {
				time.Sleep(100 * time.Millisecond)
				continue
			}

			// 先检查 completionCh
			completionCh := m.subagentManager.CompletionChan()
			select {
			case notification, ok := <-completionCh:
				if ok {
					// 获取 subagent name
					sub, found := m.subagentManager.GetSubagent(notification.SubagentID)
					name := notification.SubagentID
					if found {
						name = sub.Name()
					}
					return subagentCompletionMsg{
						subagentID:   notification.SubagentID,
						subagentName: name,
						status:       notification.Status,
						result:       notification.Result,
						err:          notification.Err,
					}
				}
			default:
			}

			streams := m.subagentManager.ListStreams()
			hasRunning := false
			for _, stream := range streams {
				// 检测子代理状态变化（从运行变为完成/失败）
				if stream.Status != subagent.StatusRunning {
					m.cleanupSubagentOutput(stream.ID, stream.Name, stream.Status)
					continue
				}
				hasRunning = true
				select {
				case msg, ok := <-stream.ViewCh:
					if !ok {
						continue
					}
					return subagentStreamMsg{
						subagentID:   stream.ID,
						subagentName: stream.Name,
						event:        msg,
					}
				default:
				}
			}

			// 更新子代理运行状态
			if hasRunning && m.state != stateSubagentRunning {
				m.state = stateSubagentRunning
			} else if !hasRunning && m.state == stateSubagentRunning {
				m.state = stateIdle
			}

			time.Sleep(10 * time.Millisecond)
		}
	}
}

// cleanupSubagentOutput 清理子代理输出并显示完成状态
func (m *TuiViewModel) cleanupSubagentOutput(subagentID, subagentName string, status subagent.SubagentStatus) {
	output, exists := m.subagentOutputs[subagentID]
	if !exists {
		return
	}

	// 更新最后一条日志为完成状态
	if output.lastLogIdx >= 0 && output.lastLogIdx < len(m.logs) {
		var statusText string
		switch status {
		case subagent.StatusCompleted:
			statusText = "✓ 已完成"
		case subagent.StatusFailed:
			statusText = "✗ 失败"
		case subagent.StatusStopped:
			statusText = "■ 已停止"
		default:
			statusText = "○ 结束"
		}
		// 在内容末尾添加状态
		m.logs[output.lastLogIdx].Content = output.getContent() + "\n" + subagentDoneStyle.Render(statusText)
	}

	// 清理缓冲区
	delete(m.subagentOutputs, subagentID)

	// 刷新视图
	m.refreshLogsViewportContent()
}

// handleSubagentCompletion 处理子代理完成通知
func (m *TuiViewModel) handleSubagentCompletion(msg subagentCompletionMsg) (tea.Model, tea.Cmd) {
	// 如果有结果，记录日志
	if msg.result != "" {
		logging.Info("Subagent %s completed with result: %s", msg.subagentName, truncateString(msg.result, 100))
	}

	// 如果有错误，记录错误
	if msg.err != nil {
		logging.Error("Subagent %s failed: %v", msg.subagentName, msg.err)
	}

	// 更新运行状态
	streams := m.subagentManager.ListStreams()
	hasRunning := false
	for _, stream := range streams {
		if stream.Status == subagent.StatusRunning {
			hasRunning = true
			break
		}
	}
	if !hasRunning && m.state == stateSubagentRunning {
		m.state = stateIdle
	}

	return m, m.startSubagentListener()
}

// handleSubagentStreamMsg 处理子代理流消息
func (m *TuiViewModel) handleSubagentStreamMsg(msg subagentStreamMsg) (tea.Model, tea.Cmd) {
	switch msg.event.Type {
	case agent.MessageTypeReasoning:
		// 子代理推理内容不显示在终端
		return m, m.startSubagentListener()
	case agent.MessageTypeContent:
		// 子代理回答内容不显示在终端
		return m, m.startSubagentListener()
	case agent.MessageTypeToolCall:
		if msg.event.ToolCall == nil {
			return m, m.startSubagentListener()
		}
		toolInfo := fmt.Sprintf("%s(%s)", msg.event.ToolCall.Name, msg.event.ToolCall.Arguments)
		m.appendSubagentOutput(msg.subagentID, msg.subagentName, "tool", toolInfo)
	case agent.MessageTypeError:
		if msg.event.Content == nil {
			return m, m.startSubagentListener()
		}
		m.appendSubagentOutput(msg.subagentID, msg.subagentName, "error", *msg.event.Content)
	case agent.MessageTypeTokenUsage:
		// Token 用量不显示在滚动区域，子代理完成后统一显示
		return m, m.startSubagentListener()
	case agent.MessageTypeToolConfirm:
		// 子代理的确认请求自动批准
		if msg.event.ToolConfirmationRequest != nil {
			m.autoConfirmSubagent(msg.subagentID)
		}
		return m, m.startSubagentListener()
	case agent.MessageTypePolicy, agent.MessageTypeMemory:
		// 这些类型不在子代理中显示
		return m, m.startSubagentListener()
	}

	m.refreshLogsViewportContent()
	return m, m.startSubagentListener()
}

// autoConfirmSubagent 自动批准子代理的确认请求
func (m *TuiViewModel) autoConfirmSubagent(subagentID string) {
	if m.subagentManager == nil {
		return
	}
	streams := m.subagentManager.ListStreams()
	for _, stream := range streams {
		if stream.ID == subagentID && stream.Status == subagent.StatusRunning {
			select {
			case stream.ConfirmCh <- agent.ConfirmAllow:
			default:
			}
			break
		}
	}
}

// appendSubagentOutput 追加子代理输出（限制3-5行滚动）
func (m *TuiViewModel) appendSubagentOutput(subagentID, subagentName, msgType, content string) {
	// 获取或创建子代理输出缓冲区
	output, exists := m.subagentOutputs[subagentID]
	if !exists {
		output = newSubagentOutput(5) // 最多保留5行
		m.subagentOutputs[subagentID] = output
	}

	// 格式化输出行
	var line string
	switch msgType {
	case "reasoning":
		line = reasonStyle.Render("[思考] " + truncateString(content, 100))
	case "content":
		line = contentStyle.Render("[回答] " + truncateString(content, 100))
	case "tool":
		line = toolStyle.Render("[工具] " + content)
	case "error":
		line = errorStyle.Render("[错误] " + content)
	default:
		line = contentStyle.Render(content)
	}

	output.add(line)

	// 更新或创建日志条目
	fullContent := output.getContent()
	if output.lastLogIdx >= 0 && output.lastLogIdx < len(m.logs) {
		// 更新现有条目
		m.logs[output.lastLogIdx].Content = fullContent
	} else {
		// 创建新条目
		entry := LogEntry{
			Title:        subagentName,
			Content:      fullContent,
			Style:        contentStyle,
			SubagentID:   subagentID,
			SubagentName: subagentName,
		}
		m.logs = append(m.logs, entry)
		output.lastLogIdx = len(m.logs) - 1
	}
}

// truncateString 截断字符串
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func (m *TuiViewModel) startNewTurn(query string) (tea.Model, tea.Cmd) {
	m.notice = ""
	turnStart := len(m.logs)
	m.logs = append(m.logs, NewContent(query))

	streamC := make(chan agent.MessageVO, 10) // 有缓冲 channel，避免发送阻塞
	confirmCh := make(chan agent.ConfirmationAction)
	doneC := make(chan error)
	ctx, cancel := context.WithCancel(context.Background())
	m.active = &activeStream{
		events:      streamC,
		cancel:      cancel,
		confirmCh:   confirmCh,
		turnLogLen:  turnStart,
		reasonBody:  -1,
		contentBody: -1,
		policyBody:  -1,
		memoryBody:  -1,
	}
	m.state = stateRunning
	m.refreshLogsViewportContent()

	go func() {
		err := m.agent.RunStreaming(ctx, query, streamC, confirmCh, nil)
		close(streamC)
		close(confirmCh)
		doneC <- err
		close(doneC)
	}()

	return m, tea.Batch(waitStreamEvent(streamC), waitStreamDone(doneC))
}

func (m *TuiViewModel) clearSession() {
	m.agent.ResetSession()
	m.logs = m.logs[:0]
	m.notice = "会话已清空（仅保留 system prompt）。"
	m.refreshLogsViewportContent()
}

func (m *TuiViewModel) abortCurrentTurn() {
	if m.state != stateRunning || m.active == nil || m.active.cancel == nil {
		return
	}
	m.state = stateAborting
	m.logs = append(m.logs, NewNotice("用户取消了 agent loop，消息已保留。"))
	m.refreshLogsViewportContent()
	m.active.cancel()
}

func (m *TuiViewModel) rollbackTurn() {
	if m.active == nil {
		return
	}
	if m.active.turnLogLen >= 0 && m.active.turnLogLen <= len(m.logs) {
		m.logs = m.logs[:m.active.turnLogLen]
	}
	m.refreshLogsViewportContent()
}

func (m *TuiViewModel) stopActiveStream() {
	if m.active == nil {
		return
	}
	if m.active.cancel != nil {
		m.active.cancel()
	}
	m.active = nil
}

// stopAllSubagents 停止所有运行中的 subagent
func (m *TuiViewModel) stopAllSubagents() {
	if m.subagentManager == nil {
		return
	}
	subagents := m.subagentManager.ListSubagents()
	for _, sub := range subagents {
		if sub.Status() == subagent.StatusRunning {
			_ = sub.Stop()
		}
	}
	m.notice = "已终止所有子代理。"
}

func (m *TuiViewModel) scrollUp(n int) {
	if n <= 0 {
		return
	}
	m.logsViewport.ScrollUp(n)
}

func (m *TuiViewModel) scrollDown(n int) {
	if n <= 0 {
		return
	}
	m.logsViewport.ScrollDown(n)
}

func (m *TuiViewModel) logsHeaderHeight() int {
	return 4
}

func (m *TuiViewModel) logsFooterHeight() int {
	h := 4
	if m.state != stateIdle {
		h++
	}
	if m.notice != "" {
		h++
	}
	return h
}

func (m *TuiViewModel) logsViewportHeight() int {
	if m.height <= 0 {
		return 1
	}
	h := m.height - m.logsHeaderHeight() - m.logsFooterHeight()
	if h < 1 {
		return 1
	}
	return h
}

func (m *TuiViewModel) syncLogsViewportSize() {
	w := m.width
	if w < 1 {
		w = 1
	}
	m.logsViewport.SetWidth(w)
	m.logsViewport.SetHeight(m.logsViewportHeight())
}

func (m *TuiViewModel) refreshLogsViewportContent() {
	atBottom := m.logsViewport.AtBottom()
	offset := m.logsViewport.YOffset()
	lines := make([]string, 0, len(m.logs))
	for _, entry := range m.logs {
		rendered := strings.TrimSpace(entry.Render())
		if rendered != "" {
			lines = append(lines, rendered)
		}
	}
	m.logsViewport.SetContent(strings.Join(lines, "\n"))
	if atBottom {
		m.logsViewport.GotoBottom()
	} else {
		m.logsViewport.SetYOffset(offset)
	}
}

// refreshLogsViewportContentAfterResize 在窗口大小变化后刷新 viewport 内容
// 窗口大小变化后，直接跳转到底部以确保内容可见
func (m *TuiViewModel) refreshLogsViewportContentAfterResize() {
	lines := make([]string, 0, len(m.logs))
	for _, entry := range m.logs {
		rendered := strings.TrimSpace(entry.Render())
		if rendered != "" {
			lines = append(lines, rendered)
		}
	}
	m.logsViewport.SetContent(strings.Join(lines, "\n"))
	m.logsViewport.GotoBottom()
}

func (m *TuiViewModel) View() tea.View {
	var b strings.Builder

	b.WriteString(titleStyle.Render("AwesomeBot TUI (Bubble Tea)"))
	b.WriteString("\n")
	b.WriteString(borderStyle.Render(strings.Repeat("─", 48)))
	b.WriteString("\n")
	b.WriteString(contentStyle.Render("欢迎使用 AwesomeBot！输入问题后回车。"))
	b.WriteString("\n")
	b.WriteString(labelStyle.Render("模型: "))
	b.WriteString(contentStyle.Render(m.modelName))
	b.WriteString(" | ")
	b.WriteString(labelStyle.Render("版本: "))
	b.WriteString(contentStyle.Render(m.version))
	b.WriteString("\n")

	// 显示 subagent 状态（只显示运行中的）
	if m.subagentManager != nil {
		subagents := m.subagentManager.ListSubagents()
		runningSubagents := make([]subagent.Subagent, 0)
		for _, s := range subagents {
			if s.Status() == subagent.StatusRunning {
				runningSubagents = append(runningSubagents, s)
			}
		}
		if len(runningSubagents) > 0 {
			b.WriteString(RenderSubagentList(runningSubagents))
			b.WriteString("\n")
		}
	}

	b.WriteString(m.logsViewport.View())

	// 如果在确认状态，渲染确认框
	if m.state == stateAwaitingConfirmation {
		b.WriteString("\n")
		b.WriteString(m.renderConfirmBox())
	}

	b.WriteString("\n")
	if m.state == stateSubagentRunning {
		b.WriteString(footerStyle.Render("子代理运行中，Ctrl+C 终止，输入暂不可用。"))
		b.WriteString("\n")
	} else if m.state != stateIdle && m.state != stateAwaitingConfirmation {
		b.WriteString(footerStyle.Render("模型响应中，输入暂不可用。"))
		b.WriteString("\n")
	}
	if m.state == stateAwaitingConfirmation {
		b.WriteString(footerStyle.Render("↑↓ 选择  Enter 确认  Esc 拒绝"))
		b.WriteString("\n")
	} else if m.state != stateSubagentRunning {
		b.WriteString(contentStyle.Render(">>> " + m.input))
		b.WriteString("\n")
	}
	b.WriteString(footerStyle.Render("快捷键: Ctrl+C 退出，Esc 取消当前流式"))
	b.WriteString("\n")
	b.WriteString(footerStyle.Render("命令: /clear 清空会话"))
	if m.notice != "" {
		b.WriteString("\n")
		b.WriteString(noticeStyle.Render(m.notice))
	}

	v := tea.NewView(b.String())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m *TuiViewModel) renderConfirmBox() string {
	var b strings.Builder
	maxWidth := 40
	if m.width > 0 && m.width < maxWidth {
		maxWidth = m.width - 4
	}

	for i, option := range m.confirmOptions {
		var line string
		if i == m.selectedConfirmIdx {
			line = fmt.Sprintf("  ▶ %s", option)
			line = confirmSelectedStyle.Render(line)
		} else {
			line = fmt.Sprintf("    %s", option)
			line = confirmOptionStyle.Render(line)
		}
		b.WriteString(line)
		if i < len(m.confirmOptions)-1 {
			b.WriteString("\n")
		}
	}

	return confirmBoxStyle.Width(maxWidth).Render(b.String())
}
