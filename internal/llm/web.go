package llm

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sceptyre/maia/internal/config"
)

// WebTools are tools for web research
var WebTools = []Tool{
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "web_search",
			Description: "Search the web for information. Returns search results with titles, URLs, and snippets.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query",
					},
				},
				"required": []string{"query"},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "fetch_url",
			Description: "Fetch content from a URL. Returns the text content of the page.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url": map[string]interface{}{
						"type":        "string",
						"description": "URL to fetch",
					},
				},
				"required": []string{"url"},
			},
		},
	},
}

// HandleWebToolCall processes a web tool call
func HandleWebToolCall(call ToolCall) (string, error) {
	var args map[string]string
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return "", fmt.Errorf("failed to parse arguments: %w", err)
	}

	switch call.Function.Name {
	case "web_search":
		return handleWebSearch(args["query"])
	case "fetch_url":
		return handleFetchURL(args["url"])
	default:
		return "", fmt.Errorf("unknown web tool: %s", call.Function.Name)
	}
}

func handleWebSearch(query string) (string, error) {
	// Check for Brave Search API key
	apiKey := config.Get("brave_api_key")
	if apiKey == "" {
		return searchUsingDuckDuckGo(query)
	}

	return searchUsingBrave(query, apiKey)
}

func searchUsingBrave(query, apiKey string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	reqURL := fmt.Sprintf("https://api.search.brave.com/res/v1/web/search?q=%s&count=10", url.QueryEscape(query))
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
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

	if err := json.Unmarshal(body, &result); err != nil {
		return string(body), nil
	}

	var sb strings.Builder
	for i, r := range result.Web.Results {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, r.Title))
		sb.WriteString(fmt.Sprintf("   URL: %s\n", r.URL))
		sb.WriteString(fmt.Sprintf("   %s\n\n", r.Description))
	}

	return sb.String(), nil
}

func searchUsingDuckDuckGo(query string) (string, error) {
	// Simple DuckDuckGo lite search
	client := &http.Client{Timeout: 10 * time.Second}

	reqURL := fmt.Sprintf("https://lite.duckduckgo.com/lite/?q=%s", url.QueryEscape(query))
	resp, err := client.Get(reqURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Simple extraction of results
	content := string(body)
	var results []string

	// Find result links (simplified parsing)
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if strings.Contains(line, "<a rel=\"nofollow\" class=\"result-link\"") {
			start := strings.Index(line, "href=\"") + 6
			end := strings.Index(line[start:], "\"") + start
			if start > 5 && end > start {
				results = append(results, line[start:end])
			}
		}
	}

	if len(results) == 0 {
		return "No results found (web search limited - consider setting BRAVE_API_KEY)", nil
	}

	var sb strings.Builder
	sb.WriteString("Search results:\n\n")
	for i, r := range results[:min(5, len(results))] {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, r))
	}

	return sb.String(), nil
}

func handleFetchURL(rawURL string) (string, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	resp, err := client.Get(rawURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	content := string(body)

	// Basic HTML to text conversion
	content = stripHTML(content)

	// Truncate if too long
	if len(content) > 50000 {
		content = content[:50000] + "\n\n... (truncated)"
	}

	return content, nil
}

func stripHTML(html string) string {
	// Remove script and style tags
	html = removeTag(html, "script")
	html = removeTag(html, "style")

	// Remove all HTML tags
	var result strings.Builder
	inTag := false
	for _, r := range html {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			result.WriteRune(r)
		}
	}

	// Clean up whitespace
	content := result.String()
	content = strings.ReplaceAll(content, "\n\n\n", "\n\n")
	content = strings.TrimSpace(content)

	return content
}

func removeTag(html, tag string) string {
	start := strings.ToLower(html)
	for {
		idx := strings.Index(start, "<"+tag)
		if idx == -1 {
			break
		}
		endIdx := strings.Index(start[idx:], "</"+tag+">")
		if endIdx == -1 {
			break
		}
		end := idx + endIdx + len("</"+tag+">")
		html = html[:idx] + html[end:]
		start = strings.ToLower(html)
	}
	return html
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
