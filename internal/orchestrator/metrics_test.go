package orchestrator

import (
	"testing"

	"github.com/enter33/AwesomeBot/pkg/config"
)

func TestScoreCalculator(t *testing.T) {
	calc := NewScoreCalculator()

	dimensions := []DimensionConfig{
		{Name: "维度1", Weight: 0.5},
		{Name: "维度2", Weight: 0.5},
	}
	scores := []float64{80, 60}
	threshold := 70.0

	result := calc.CalculateWithThreshold(dimensions, scores, threshold)

	expected := 70.0
	if result.TotalScore != expected {
		t.Errorf("expected %f, got %f", expected, result.TotalScore)
	}

	if !result.Passed {
		t.Error("expected passed=true when score >= threshold")
	}

	// 测试不通过情况
	result2 := calc.CalculateWithThreshold(dimensions, scores, 80.0)
	if result2.Passed {
		t.Error("expected passed=false when score < threshold")
	}
}

func TestComplexityDetector(t *testing.T) {
	detector := NewComplexityDetector(2)

	// 测试简单任务
	simple := "读取文件 test.go"
	if detector.Detect(simple) != ComplexitySimple {
		t.Error("expected simple")
	}

	// 测试复杂任务（多个文件 + 关键词）
	complex := "重构 src/utils/helper.go src/models/user.go src/api/auth.go src/db/store.go"
	if detector.Detect(complex) != ComplexityComplex {
		t.Error("expected complex")
	}

	// 测试仅有关键词（阈值=2，只有1分，应该是简单）
	kwOnly := "修改配置文件"
	if detector.Detect(kwOnly) != ComplexitySimple {
		t.Error("expected simple for keyword-only with threshold=2")
	}
}

func TestComplexityDetectorThreshold1(t *testing.T) {
	// 阈值设为1，关键词即可触发复杂
	detector := NewComplexityDetector(1)
	kwOnly := "修改配置文件"
	if detector.Detect(kwOnly) != ComplexityComplex {
		t.Error("expected complex when threshold=1 and keyword present")
	}
}

func TestParseReviewScore(t *testing.T) {
	output := "总分: 85/100\n通过与否: 是\n质疑点: 无"
	score := parseReviewScore(output)
	if score.TotalScore != 85 {
		t.Errorf("expected score 85, got %f", score.TotalScore)
	}
	if !score.Passed {
		t.Error("expected passed=true")
	}

	// 测试不及格
	output2 := "总分: 60/100\n通过与否: 否"
	score2 := parseReviewScore(output2)
	if score2.Passed {
		t.Error("expected passed=false")
	}
}

func TestShouldOrchestrate(t *testing.T) {
	// enabled=true 时总是编排
	cfg := config.MultiAgentConfig{Enabled: true, ComplexityThreshold: 2}
	o := NewOrchestrator(cfg, nil, nil)
	if !o.ShouldOrchestrate("简单请求") {
		t.Error("expected ShouldOrchestrate=true when enabled")
	}

	// enabled=false 时按复杂度判断
	cfg2 := config.MultiAgentConfig{Enabled: false, ComplexityThreshold: 2}
	o2 := NewOrchestrator(cfg2, nil, nil)
	if o2.ShouldOrchestrate("读取文件") {
		t.Error("expected ShouldOrchestrate=false for simple task")
	}
	if !o2.ShouldOrchestrate("重构 a.go b.go c.go d.go") {
		t.Error("expected ShouldOrchestrate=true for complex task")
	}
}
