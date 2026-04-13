package orchestrator

// OrchestrationState 编排状态
type OrchestrationState string

const (
	StatePending       OrchestrationState = "pending"
	StatePlanning      OrchestrationState = "planning"
	StatePlanReviewing OrchestrationState = "plan_reviewing"
	StateCoding        OrchestrationState = "coding"
	StateCodeReviewing OrchestrationState = "code_reviewing"
	StateFinalReview   OrchestrationState = "final_review"
	StateDone          OrchestrationState = "done"
	StatePaused        OrchestrationState = "paused"
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

// OrchestratorUpdate 编排器状态更新（发送给 TUI）
type OrchestratorUpdate struct {
	State      OrchestrationState
	Score      *ReviewScore
	Message    string
	PhaseLog   string
	IsFinal    bool
	Error      error
}
