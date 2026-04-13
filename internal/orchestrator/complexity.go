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
