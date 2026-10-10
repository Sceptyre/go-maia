package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sceptyre/maia/internal/config"
)

// Client is an OpenAI-compatible API client
type Client struct {
	BaseURL    string
	APIKey     string
	Model      string
	MaxTokens  int
	Headers    map[string]string
	SessionID  string
	HTTPClient *http.Client
}

// Message represents a chat message
type Message struct {
	Role       string     `json:"role"`
	Content    *string    `json:"content"` // pointer to allow null
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// NewMessage creates a message with content
func NewMessage(role, content string) Message {
	return Message{Role: role, Content: &content}
}

// NewAssistantMessage creates an assistant message with optional content and tool calls
func NewAssistantMessage(content string, toolCalls []ToolCall) Message {
	msg := Message{Role: "assistant", ToolCalls: toolCalls}
	if content != "" {
		msg.Content = &content
	}
	return msg
}

func strPtr(s string) *string {
	return &s
}

// Tool represents a function tool
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"`
}

// ToolCall represents a function call from the model
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function ToolCallFunc `json:"function"`
}

type ToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ResponseMessage represents a message in an API response.
// Unlike Message (used for requests), Content is a plain string (API never sends null).
type ResponseMessage struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// ChatRequest is the request body for the chat completions API
type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Tools       []Tool    `json:"tools,omitempty"`
	ToolChoice  string    `json:"tool_choice,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
}

// ChatResponse is the response from the chat completions API
type ChatResponse struct {
	ID      string   `json:"id"`
	Choices []Choice `json:"choices"`
}

type Choice struct {
	Message      ResponseMessage `json:"message"`
	FinishReason string          `json:"finish_reason"` // "stop", "length", "tool_calls", etc.
}

// Truncated returns true if the response was cut off due to token limits.
func (c Choice) Truncated() bool {
	return c.FinishReason == "length"
}

// NewClient creates a new LLM client
func NewClient() *Client {
	cfg, _ := config.Load()
	if cfg == nil {
		cfg = &config.Config{}
	}

	baseURL := config.Get("openai_base_url")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	apiKey := config.Get("openai_api_key")

	model := config.Get("model")
	if model == "" {
		model = "gpt-4"
	}

	maxTokens := 16384
	if mt := config.Get("max_tokens"); mt != "" {
		if parsed, err := strconv.Atoi(mt); err == nil && parsed > 0 {
			maxTokens = parsed
		}
	}

	return &Client{
		BaseURL:   baseURL,
		APIKey:    apiKey,
		Model:     model,
		MaxTokens: maxTokens,
		Headers:   cfg.OpenAIHeaders,
		SessionID: currentSessionID(),
		HTTPClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

// Chat sends a chat completion request
func (c *Client) Chat(messages []Message, tools []Tool) (*ChatResponse, error) {
	return c.ChatWithMaxTokens(messages, tools, 0)
}

// ChatWithMaxTokens sends a chat completion request with an optional max token limit.
// If maxTokens is 0, the client's default MaxTokens is used.
func (c *Client) ChatWithMaxTokens(messages []Message, tools []Tool, maxTokens int) (*ChatResponse, error) {
	effectiveMax := c.MaxTokens
	if maxTokens > 0 {
		effectiveMax = maxTokens
	}

	request := ChatRequest{
		Model:       c.Model,
		Messages:    messages,
		Temperature: 0.7,
		MaxTokens:   effectiveMax,
	}

	// Only include tools if provided (some providers don't support them)
	if len(tools) > 0 {
		request.Tools = tools
	}

	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Build URL, avoiding double /chat/completions
	baseURL := strings.TrimRight(c.BaseURL, "/")
	if !strings.HasSuffix(baseURL, "/chat/completions") {
		baseURL = baseURL + "/chat/completions"
	}
	req, err := http.NewRequest("POST", baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	sessionID := c.SessionID
	if sessionID == "" {
		sessionID = currentSessionID()
	}
	seenHeaders := make(map[string]bool)
	for key, value := range c.Headers {
		canonical := http.CanonicalHeaderKey(key)
		if !validHeaderName(key) || seenHeaders[canonical] || strings.ContainsAny(value, "\r\n") {
			return nil, fmt.Errorf("invalid or duplicate custom HTTP header %q", key)
		}
		seenHeaders[canonical] = true
		req.Header.Set(key, expandSessionTemplate(value, sessionID))
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Debug: log request size
	fmt.Fprintf(os.Stderr, "  [debug] Request size: %d bytes, Messages: %d\n", len(body), len(messages))

	if resp.StatusCode != http.StatusOK {
		// Debug: log full error response
		fmt.Fprintf(os.Stderr, "  [debug] Error response: %s\n", string(respBody))
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &chatResp, nil
}

// GetResponse sends a request and returns just the text response
func (c *Client) GetResponse(messages []Message) (string, error) {
	return c.GetResponseWithMaxTokens(messages, 0)
}

// GetResponseWithMaxTokens sends a request and returns just the text response
// with an optional max token limit. If maxTokens is 0, the client default is used.
func (c *Client) GetResponseWithMaxTokens(messages []Message, maxTokens int) (string, error) {
	resp, err := c.ChatWithMaxTokens(messages, nil, maxTokens)
	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response from model")
	}

	if resp.Choices[0].Truncated() {
		return "", fmt.Errorf("response truncated: the model's output exceeded the token limit")
	}

	return resp.Choices[0].Message.Content, nil
}

// GetResponseWithTools sends a request with tools and handles tool calls.
// Tool calls are executed concurrently for improved performance.
// maxTokens limits the output token budget; 0 uses the client default.
func (c *Client) GetResponseWithTools(
	messages []Message,
	tools []Tool,
	toolHandler func(ToolCall) (string, error),
	maxTokens int,
) (string, []Message, error) {
	return c.GetResponseWithToolsContext(
		context.Background(), messages, tools, toolHandler, DefaultConcurrentConfig(), maxTokens)
}

// GetResponseWithToolsContext sends a request with tools and handles
// tool calls with context cancellation support and configurable concurrency.
// maxTokens limits the output token budget; 0 uses the client default.
func (c *Client) GetResponseWithToolsContext(
	ctx context.Context,
	messages []Message,
	tools []Tool,
	toolHandler func(ToolCall) (string, error),
	config ConcurrentConfig,
	maxTokens int,
) (string, []Message, error) {
	for i := 0; i < 100; i++ {
		// Check context before each LLM call.
		select {
		case <-ctx.Done():
			return "", messages, ctx.Err()
		default:
		}

		resp, err := c.ChatWithMaxTokens(messages, tools, maxTokens)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n⚠ API error: %v\n", err)
			if len(tools) > 0 {
				fmt.Fprintf(os.Stderr, "⚠ Retrying without tools...\n")
				result, err := c.GetResponse(messages)
				return result, messages, err
			}
			return "", messages, err
		}

		if len(resp.Choices) == 0 {
			return "", messages, fmt.Errorf("no response from model")
		}

		choice := resp.Choices[0]

		// No tool calls — return final answer.
		if len(choice.Message.ToolCalls) == 0 {
			if choice.Truncated() {
				return choice.Message.Content, messages,
					fmt.Errorf("response truncated at token limit (partial content returned)")
			}
			return choice.Message.Content, messages, nil
		}

		// Append assistant message with tool-call metadata.
		messages = append(messages,
			NewAssistantMessage(choice.Message.Content, choice.Message.ToolCalls))

		// Execute all tool calls concurrently.
		toolResults := concurrentToolExecutor(
			ctx, choice.Message.ToolCalls, toolHandler, config)

		// Append ordered results.
		messages = append(messages, toolResults...)

		fmt.Fprintf(os.Stderr,
			"  [debug] Executed %d tool calls concurrently\n", len(toolResults))
	}

	return "", messages, fmt.Errorf("exceeded max iterations")
}
