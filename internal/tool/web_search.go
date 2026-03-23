package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/enter33/AwesomeBot/pkg/config"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

const configExample = `配置示例 (~/.awesome/web_search.json):
{
  "provider": "jina",
  "api_key": "your_jina_api_key",
  "max_results": 5
}

支持的 provider:
- jina: 使用 Jina AI 搜索（需要 JINA_API_KEY，可从 https://jina.ai/ 获取）
- duckduckgo: 免费搜索（国内可能无法访问）
- tavily: 需要 TAVILY_API_KEY
- searxng: 需要自建 SearXNG 服务，配置 base_url
- brave: 需要 BRAVE_API_KEY`

// WebSearchTool Web 搜索工具
type WebSearchTool struct {
	config config.WebSearchConfig
}

// NewWebSearchTool 创建 Web 搜索工具
func NewWebSearchTool(cfg config.WebSearchConfig) *WebSearchTool {
	return &WebSearchTool{config: cfg}
}

type WebSearchToolParam struct {
	Query string `json:"query"`
	Count *int   `json:"count,omitempty"`
}

func (t *WebSearchTool) ToolName() AgentTool {
	return AgentToolWebSearch
}

func (t *WebSearchTool) Info() openai.ChatCompletionToolUnionParam {
	return openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
		Name:        string(AgentToolWebSearch),
		Description: openai.String("Search the web. Returns titles, URLs, and snippets."),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Search query",
				},
				"count": map[string]any{
					"type":        "integer",
					"description": "Number of results (1-10)",
					"minimum":     1,
					"maximum":     10,
				},
			},
			"required": []string{"query"},
		},
	})
}

func (t *WebSearchTool) Execute(ctx context.Context, argumentsInJSON string) (string, error) {
	p := WebSearchToolParam{}
	if err := json.Unmarshal([]byte(argumentsInJSON), &p); err != nil {
		return "", err
	}

	provider := strings.ToLower(strings.TrimSpace(t.config.Provider))
	if provider == "" {
		return "", fmt.Errorf("web_search 未配置 provider，请配置后再使用\n\n%s", configExample)
	}

	count := 5
	if p.Count != nil {
		count = *p.Count
		if count < 1 {
			count = 1
		} else if count > 10 {
			count = 10
		}
	}

	switch provider {
	case "duckduckgo":
		return t.searchDuckDuckGo(ctx, p.Query, count)
	case "tavily":
		return t.searchTavily(ctx, p.Query, count)
	case "searxng":
		return t.searchSearXNG(ctx, p.Query, count)
	case "jina":
		return t.searchJina(ctx, p.Query, count)
	case "brave":
		return t.searchBrave(ctx, p.Query, count)
	default:
		return "", fmt.Errorf("unknown search provider: %s\n\n%s", provider, configExample)
	}
}

// searchDuckDuckGo 使用 DuckDuckGo HTML 搜索
func (t *WebSearchTool) searchDuckDuckGo(ctx context.Context, query string, n int) (string, error) {
	searchURL := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_7_2) AppleWebKit/537.36")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("DuckDuckGo 搜索失败（国内可能无法访问）: %v\n\n建议配置 jina provider: https://jina.ai/", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("DuckDuckGo 返回状态码 %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	items := parseDuckDuckGoResults(string(body), n)
	return formatResults(query, items, n), nil
}

// parseDuckDuckGoResults 解析 DuckDuckGo HTML 结果
func parseDuckDuckGoResults(html string, n int) []SearchResultItem {
	items := make([]SearchResultItem, 0)

	// 匹配结果条目: <a class="result__a" href="...">Title</a>
	// 和描述: <a class="result__snippet" href="...">Description</a>

	resultRegex := regexp.MustCompile(`<a class="result__a"[^>]*href="([^"]+)"[^>]*>([^<]+)</a>`)
	titleMatches := resultRegex.FindAllStringSubmatch(html, -1)

	snippetRegex := regexp.MustCompile(`<a class="result__snippet"[^>]*>([^<]+)</a>`)
	snippetMatches := snippetRegex.FindAllStringSubmatch(html, -1)

	count := n
	if count > len(titleMatches) {
		count = len(titleMatches)
	}

	for i := 0; i < count; i++ {
		item := SearchResultItem{}

		if i < len(titleMatches) {
			item.URL = titleMatches[i][1]
			item.Title = cleanHTMLTags(titleMatches[i][2])
		}

		if i < len(snippetMatches) {
			item.Content = cleanHTMLTags(snippetMatches[i][1])
		}

		items = append(items, item)
	}

	return items
}

// cleanHTMLTags 清理 HTML 标签
func cleanHTMLTags(s string) string {
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// searchTavily 使用 Tavily 搜索
func (t *WebSearchTool) searchTavily(ctx context.Context, query string, n int) (string, error) {
	apiKey := t.config.ApiKey
	if apiKey == "" {
		apiKey = os.Getenv("TAVILY_API_KEY")
	}
	if apiKey == "" {
		return "", fmt.Errorf("Tavily 搜索需要 API Key，请配置 TAVILY_API_KEY 或在配置文件中设置 api_key\n\n%s", configExample)
	}

	reqBody := map[string]any{"query": query, "max_results": n}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.tavily.com/search", strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Tavily 搜索失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Tavily API 返回状态码 %d", resp.StatusCode)
	}

	var result struct {
		Results []SearchResultItem `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return formatResults(query, result.Results, n), nil
}

// searchSearXNG 使用 SearXNG 搜索
func (t *WebSearchTool) searchSearXNG(ctx context.Context, query string, n int) (string, error) {
	baseURL := t.config.BaseURL
	if baseURL == "" {
		baseURL = os.Getenv("SEARXNG_BASE_URL")
	}
	if baseURL == "" {
		return "", fmt.Errorf("SearXNG 搜索需要配置 base_url\n\n%s", configExample)
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	endpoint := baseURL + "/search"

	valid, errMsg := validateURL(endpoint)
	if !valid {
		return "", fmt.Errorf("invalid SearXNG URL: %s", errMsg)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_7_2) AppleWebKit/537.36")
	q := req.URL.Query()
	q.Set("q", query)
	q.Set("format", "json")
	req.URL.RawQuery = q.Encode()

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("SearXNG 搜索失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("SearXNG 返回状态码 %d", resp.StatusCode)
	}

	var result struct {
		Results []SearchResultItem `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return formatResults(query, result.Results, n), nil
}

// searchJina 使用 Jina AI 搜索
func (t *WebSearchTool) searchJina(ctx context.Context, query string, n int) (string, error) {
	apiKey := t.config.ApiKey
	if apiKey == "" {
		apiKey = os.Getenv("JINA_API_KEY")
	}
	if apiKey == "" {
		return "", fmt.Errorf("Jina 搜索需要 API Key，请配置 JINA_API_KEY 或在配置文件中设置 api_key\n\n获取方式: https://jina.ai/\n\n%s", configExample)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", "https://s.jina.ai/"+url.QueryEscape(query), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Jina 搜索失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Jina AI 返回状态码 %d", resp.StatusCode)
	}

	var result struct {
		Data []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	items := make([]SearchResultItem, 0, len(result.Data))
	for _, d := range result.Data {
		content := d.Content
		if len(content) > 500 {
			content = content[:500]
		}
		items = append(items, SearchResultItem{
			Title:   d.Title,
			URL:     d.URL,
			Content: content,
		})
	}

	return formatResults(query, items, n), nil
}

// searchBrave 使用 Brave 搜索
func (t *WebSearchTool) searchBrave(ctx context.Context, query string, n int) (string, error) {
	apiKey := t.config.ApiKey
	if apiKey == "" {
		apiKey = os.Getenv("BRAVE_API_KEY")
	}
	if apiKey == "" {
		return "", fmt.Errorf("Brave 搜索需要 API Key，请配置 BRAVE_API_KEY 或在配置文件中设置 api_key\n\n%s", configExample)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.search.brave.com/res/v1/web/search", nil)
	if err != nil {
		return "", err
	}
	q := req.URL.Query()
	q.Set("q", query)
	q.Set("count", fmt.Sprintf("%d", n))
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", apiKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Brave 搜索失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Brave API 返回状态码 %d", resp.StatusCode)
	}

	var result struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	items := make([]SearchResultItem, 0, len(result.Web.Results))
	for _, r := range result.Web.Results {
		items = append(items, SearchResultItem{
			Title:   r.Title,
			URL:     r.URL,
			Content: r.Description,
		})
	}

	return formatResults(query, items, n), nil
}
