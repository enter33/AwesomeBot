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
		Passed:     total >= threshold,
	}
}
