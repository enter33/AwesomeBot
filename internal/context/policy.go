package context

import "context"

// PolicyResult policy 执行结果
type PolicyResult struct {
	Messages      []messageWrap // 新的消息列表
	ContextTokens int           // 新的 context token 计数
	RemovedKeys   []string      // 需要从 storage 中删除的 offload keys
}

// Policy 策略接口
type Policy interface {
	Name() string
	ShouldApply(ctx context.Context, engine *Engine) bool
	// Apply 纯函数，可以读取 engine 状态，返回新的状态（不修改 engine 内部变量）
	Apply(ctx context.Context, engine *Engine) (PolicyResult, error)
	// CanApplyDuringToolLoop 返回是否允许在工具执行循环中执行
	// true: 可以在循环中执行（只替换内容，不改变消息结构，原子性安全）
	// false: 只在循环结束后执行（需要 LLM 调用或删除消息）
	CanApplyDuringToolLoop() bool
}
