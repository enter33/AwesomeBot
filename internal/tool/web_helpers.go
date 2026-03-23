package tool

import (
	"regexp"
	"strings"

	"github.com/enter33/AwesomeBot/internal/security"
)

// stripTags 移除 HTML 标签并解码实体
func stripTags(text string) string {
	// 移除 script 和 style 标签及其内容
	text = regexp.MustCompile(`(?i)<script[\s\S]*?</script>`).ReplaceAllString(text, "")
	text = regexp.MustCompile(`(?i)<style[\s\S]*?</style>`).ReplaceAllString(text, "")
	// 移除所有 HTML 标签
	text = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(text, "")
	// 解码 HTML 实体
	text = unescapeHTML(text)
	return strings.TrimSpace(text)
}

// unescapeHTML 解码 HTML 实体
func unescapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	return s
}

// normalize 规范化空白字符
func normalize(text string) string {
	// 将多个空格/tab 替换为单个空格
	text = regexp.MustCompile(`[ \t]+`).ReplaceAllString(text, " ")
	// 将 3 个以上连续换行替换为两个
	text = regexp.MustCompile(`\n{3,}`).ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}

// validateURL 验证 URL 基础格式
func validateURL(rawURL string) (bool, string) {
	return security.ValidateURLTarget(rawURL)
}

// validateURLSafe 验证 URL 并检查 SSRF
func validateURLSafe(rawURL string) (bool, string) {
	valid, err := security.ValidateURLTarget(rawURL)
	if !valid {
		return valid, err
	}
	return security.ValidateResolvedURL(rawURL)
}

// formatResults 格式化搜索结果
func formatResults(query string, items []SearchResultItem, n int) string {
	if len(items) == 0 {
		return "No results for: " + query
	}
	var builder strings.Builder
	builder.WriteString("Results for: ")
	builder.WriteString(query)
	builder.WriteString("\n")

	count := n
	if count > len(items) {
		count = len(items)
	}

	for i := 0; i < count; i++ {
		item := items[i]
		builder.WriteString("\n")
		builder.WriteString(string(rune('1' + i)))
		builder.WriteString(". ")
		builder.WriteString(normalize(stripTags(item.Title)))
		builder.WriteString("\n   ")
		builder.WriteString(item.URL)
		if item.Content != "" {
			builder.WriteString("\n   ")
			builder.WriteString(normalize(stripTags(item.Content)))
		}
	}
	return builder.String()
}

// SearchResultItem 搜索结果项
type SearchResultItem struct {
	Title   string
	URL     string
	Content string
}
