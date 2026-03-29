package subagent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/enter33/AwesomeBot/internal/agent"
	"github.com/enter33/AwesomeBot/internal/logging"
)

const defaultIdleTimeout = 5 * time.Minute

// Instance 子代理实例
type Instance struct {
	id              string
	name            string
	typ             SubagentType
	status          SubagentStatus
	agentInstance   *agent.Agent
	mu              sync.RWMutex
	query           string       // 初始查询
	task            string       // 任务描述
	result          string       // 执行结果
	messageHistory  []string     // 消息历史
	stopCh          chan struct{}
	cancelTimeout   context.CancelFunc

	// 看门狗相关
	lastActivityTime time.Time
	idleTimeout     time.Duration

	// 通道供外部访问
	viewCh    chan agent.MessageVO
	confirmCh chan agent.ConfirmationAction

	// 回调相关
	statusCallbacks   []StatusCallback
	completionCh      chan CompletionNotification
	completionNotified bool
}

// NewInstance 创建子代理实例
func NewInstance(
	id string,
	name string,
	typ SubagentType,
	agentInstance *agent.Agent,
) *Instance {
	return &Instance{
		id:             id,
		name:           name,
		typ:            typ,
		status:         StatusCreated,
		agentInstance:  agentInstance,
		messageHistory: make([]string, 0),
		stopCh:         make(chan struct{}),
		idleTimeout:    defaultIdleTimeout,
	}
}

func (s *Instance) ID() string                    { return s.id }
func (s *Instance) Name() string                  { return s.name }
func (s *Instance) Type() SubagentType           { return s.typ }
func (s *Instance) Task() string                 { return s.task }
func (s *Instance) Result() string               { return s.result }
func (s *Instance) Status() SubagentStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

// RegisterStatusCallback 注册状态回调
func (s *Instance) RegisterStatusCallback(callback StatusCallback) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statusCallbacks = append(s.statusCallbacks, callback)
}

// SetCompletionCh 设置完成通知 channel
func (s *Instance) SetCompletionCh(ch chan CompletionNotification) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.completionCh = ch
}

// notifyCompletion 通知完成状态
func (s *Instance) notifyCompletion() {
	s.mu.Lock()
	if s.completionNotified {
		s.mu.Unlock()
		return
	}
	s.completionNotified = true

	status := s.status
	result := s.result
	var err error
	if status == StatusFailed {
		err = fmt.Errorf("subagent execution failed")
	}

	// 保存回调列表的副本，避免在锁内调用回调
	callbacks := make([]StatusCallback, len(s.statusCallbacks))
	copy(callbacks, s.statusCallbacks)
	completionCh := s.completionCh
	s.mu.Unlock()

	// 发送 completionCh (non-blocking)
	if completionCh != nil {
		notification := CompletionNotification{
			SubagentID: s.id,
			Status:     status,
			Result:     result,
			Err:        err,
		}
		select {
		case completionCh <- notification:
		default:
			logging.Warn("Subagent %s completionCh full, dropping notification", s.id)
		}
	}

	// 触发所有回调
	for _, callback := range callbacks {
		callback(s.id, status, result, err)
	}
}

func (s *Instance) Run(ctx context.Context, query string, viewCh chan agent.MessageVO, confirmCh chan agent.ConfirmationAction) error {
	s.mu.Lock()
	s.query = query
	s.viewCh = viewCh
	s.confirmCh = confirmCh
	s.status = StatusRunning
	s.lastActivityTime = time.Now()
	s.mu.Unlock()

	// 创建可取消的上下文用于看门狗
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.cancelTimeout = cancel
	s.mu.Unlock()

	// 看门狗 goroutine：监控活动状态
	go s.runWatchdog(ctx)

	logging.SubagentInfo(s.id, "starting (silent mode)")

	// 创建 activity channel 用于更新看门狗的 lastActivityTime
	activityCh := make(chan struct{}, 10)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-activityCh:
				s.UpdateActivityTime()
			}
		}
	}()

	// 静默执行：启动一个 goroutine 自动批准所有确认请求
	go func() {
		for {
			select {
			case _, ok := <-confirmCh:
				if !ok {
					return
				}
				// 自动批准所有请求
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

	// 在后台运行 agent
	go func() {
		// 创建中间 channel 收集输出
		internalViewCh := make(chan agent.MessageVO, 10)
		var resultBuilder []string

		// 启动 goroutine 转发消息并收集 content
		go func() {
			for msg := range internalViewCh {
				// 转发到外部 viewCh (non-blocking)
				select {
				case viewCh <- msg:
				default:
					logging.Warn("Subagent %s viewCh full, dropping message", s.id)
				}

				// 收集 content 类型的消息
				if msg.Type == agent.MessageTypeContent && msg.Content != nil {
					resultBuilder = append(resultBuilder, *msg.Content)
				}
			}
		}()

		err := s.agentInstance.RunStreaming(ctx, query, internalViewCh, confirmCh, func() {
			select {
			case activityCh <- struct{}{}:
			default:
			}
		})
		close(internalViewCh)

		s.mu.Lock()
		if s.status == StatusStopped {
			// 已被 Stop() 设置为 Stopped，不覆盖，不发送失败日志
		} else if err != nil {
			s.status = StatusFailed
			logging.SubagentError(s.id, "failed: %v", err)
		} else {
			s.status = StatusCompleted
			logging.SubagentInfo(s.id, "completed")
		}
		// 设置汇总结果
		if len(resultBuilder) > 0 {
			s.result = strings.Join(resultBuilder, "")
		}
		s.mu.Unlock()

		// 通知完成
		s.notifyCompletion()
	}()

	return nil
}

// runWatchdog 看门狗：监控活动状态，无输出超过 idleTimeout 则终止
func (s *Instance) runWatchdog(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			if s.status != StatusRunning {
				s.mu.Unlock()
				return
			}
			idleDuration := time.Since(s.lastActivityTime)
			if idleDuration > s.idleTimeout {
				logging.SubagentWarn(s.id, "idle timeout (no output for %v), terminating", idleDuration)
				if s.cancelTimeout != nil {
					s.cancelTimeout()
				}
				s.status = StatusFailed
				s.mu.Unlock()
				return
			}
			s.mu.Unlock()
		}
	}
}

// UpdateActivityTime 更新最后活动时间
func (s *Instance) UpdateActivityTime() {
	s.mu.Lock()
	s.lastActivityTime = time.Now()
	s.mu.Unlock()
}

// SetResult 设置执行结果
func (s *Instance) SetResult(result string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.result = result
}

func (s *Instance) SendMessage(ctx context.Context, msg string) error {
	s.mu.Lock()
	s.messageHistory = append(s.messageHistory, msg)
	s.mu.Unlock()

	// 如果正在运行，发送消息到当前上下文
	// 注意：这里简化处理，实际可能需要更复杂的机制
	logging.SubagentInfo(s.id, "received message: %s", msg)
	return nil
}

func (s *Instance) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.status == StatusRunning {
		// 先设置状态，确保其他协程能立即看到最新状态
		s.status = StatusStopped
		logging.SubagentInfo(s.id, "stopped")
		// 再取消 context，让 Run() goroutine 能检测到并退出
		if s.cancelTimeout != nil {
			s.cancelTimeout()
		}
	}
	return nil
}