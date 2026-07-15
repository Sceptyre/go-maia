package llm

import (
	"encoding/json"
	"fmt"
)

// TaskResult represents the result of a subagent task
type TaskResult struct {
	TaskID   string
	Agent    string
	Summary  string
	Findings string
	Error    string
}

// TaskTool creates a tool that can spawn subagents
func TaskTool(codeHandler, webHandler func(ToolCall) (string, error)) Tool {
	return Tool{
		Type: "function",
		Function: ToolFunction{
			Name:        "task",
			Description: "Delegate a research task to a specialized subagent. Use 'code' for codebase analysis, 'web' for external research.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"agent": map[string]interface{}{
						"type":        "string",
						"description": "Agent type: 'code' for codebase research, 'web' for internet research",
						"enum":        []string{"code", "web"},
					},
					"task": map[string]interface{}{
						"type":        "string",
						"description": "The research task to perform",
					},
				},
				"required": []string{"agent", "task"},
			},
		},
	}
}

// TaskHandler creates a handler for the task tool
func TaskHandler(workDir string) func(ToolCall) (string, error) {
	// Create code agent
	codeTools := FileTools
	codeAgent := NewAgent(
		"code-researcher",
		"You are a code research specialist. Find code directly relevant to the task. Be concise.",
		codeTools,
		func(call ToolCall) (string, error) {
			fmt.Printf("    [code] 🔧 %s", call.Function.Name)
			var args map[string]string
			json.Unmarshal([]byte(call.Function.Arguments), &args)
			if path, ok := args["path"]; ok && path != "" {
				fmt.Printf(" %s", path)
			} else if query, ok := args["query"]; ok && query != "" {
				fmt.Printf(" %s", query)
			}
			fmt.Println()
			return HandleToolCall(call, workDir)
		},
	)

	// Create web agent
	webAgent := NewAgent(
		"web-researcher",
		"You are a web research specialist. Find documentation and examples for specific APIs/libraries. Be concise.",
		WebTools,
		func(call ToolCall) (string, error) {
			fmt.Printf("    [web] 🔧 %s", call.Function.Name)
			var args map[string]string
			json.Unmarshal([]byte(call.Function.Arguments), &args)
			if query, ok := args["query"]; ok && query != "" {
				fmt.Printf(" %s", query)
			} else if url, ok := args["url"]; ok && url != "" {
				fmt.Printf(" %s", url)
			}
			fmt.Println()
			return HandleWebToolCall(call)
		},
	)

	return func(call ToolCall) (string, error) {
		var args struct {
			Agent string `json:"agent"`
			Task  string `json:"task"`
		}
		if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
			return "", fmt.Errorf("failed to parse task arguments: %w", err)
		}

		fmt.Printf("\n  📋 Spawning %s agent:\n     %s\n", args.Agent, truncate(args.Task, 100))

		var result string
		var err error

		switch args.Agent {
		case "code":
			result, err = codeAgent.Run(args.Task)
		case "web":
			result, err = webAgent.Run(args.Task)
		default:
			return "", fmt.Errorf("unknown agent type: %s", args.Agent)
		}

		if err != nil {
			return "", fmt.Errorf("agent failed: %w", err)
		}

		return result, nil
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// OrchestratorSystemPrompt is the system prompt for the orchestrator
func OrchestratorSystemPrompt() string {
	return `You coordinate research for code changes using specialized subagents.

Available agents:
- code: Reads and analyzes codebase files
- web: Fetches documentation and examples from the internet

Task format: "do [specific action] with the intent to [goal/purpose]"

Each task must:
1. Start with a specific action verb and exact details (file paths, function names, grep patterns)
2. State the intent/purpose clearly
3. Include any context from prior agent results

Examples:
- "Read cmd/server.go and internal/auth/handler.go with the intent to understand how the current login flow works and what UserStore methods are available"
- "Search for 'bcrypt' and 'jwt' with the intent to find existing password hashing and token generation patterns to follow"
- "Fetch https://pkg.go.dev/github.com/golang-jwt/jwt/v5 with the intent to find the correct API for creating RS256 tokens"

Max 2-4 tasks. Use prior results to inform subsequent tasks.`
}

// BuildOrchestratorMessages creates the initial messages for the orchestrator
func BuildOrchestratorMessages(changeContent string) []Message {
	systemPrompt := OrchestratorSystemPrompt()

	userPrompt := fmt.Sprintf(`## Change Request

%s

---

Generate 2-4 tasks using this format:

"do [specific action with file paths, function names, grep patterns] with the intent to [what you'll learn and why it matters]"

Task examples:
- "Read cmd/server.go and find the setupRouter function with the intent to understand how routes are registered and where auth middleware should be added"
- "Grep for 'HandleLogin' and 'UserStore' with the intent to find existing auth patterns and the interface I need to implement"
- "Fetch the bcrypt package docs with the intent to find the correct function for hashing passwords in Go"

After each task, note what context from prior results informed this task.

Do NOT write vague tasks.`,
			changeContent)

	return []Message{
		NewMessage("system", systemPrompt),
		NewMessage("user", userPrompt),
	}
}

// RunOrchestrator runs the orchestrator with task delegation
func RunOrchestrator(changeContent, workDir string) (string, error) {
	client := NewClient()
	taskHandler := TaskHandler(workDir)

	// Create task tool
	taskTool := TaskTool(nil, nil)

	messages := BuildOrchestratorMessages(changeContent)

	// Run orchestrator with task tool
	response, _, err := client.GetResponseWithTools(messages, []Tool{taskTool}, taskHandler)
	if err != nil {
		return "", err
	}

	return response, nil
}

// RunOrchestratorWithReformat runs orchestrator then reformats
func RunOrchestratorWithReformat(changeContent, workDir string) (string, error) {
	client := NewClient()
	taskHandler := TaskHandler(workDir)
	taskTool := TaskTool(nil, nil)

	// Build initial messages
	messages := BuildOrchestratorMessages(changeContent)

	// Run orchestrator - it will make tasks and get results
	fmt.Println("\n▶ Running orchestrator...")
	response, allMessages, err := client.GetResponseWithTools(messages, []Tool{taskTool}, taskHandler)
	if err != nil {
		return "", err
	}

	// If orchestrator gave a good response, use it
	if len(response) > 100 {
		return response, nil
	}

	// Otherwise, ask for synthesis using the full conversation history
	fmt.Println("\n▶ Synthesizing research findings...")

	synthPrompt := "Based on the research above, synthesize all findings into a comprehensive research document. " +
		"Include all relevant files, patterns, external research, and conventions discovered. " +
		"Output a complete summary."

	allMessages = append(allMessages,
		NewMessage("assistant", response),
		NewMessage("user", synthPrompt),
	)

	synthesis, err := client.GetResponse(allMessages)
	if err != nil {
		return response, nil // Return what we have
	}

	return synthesis, nil
}
