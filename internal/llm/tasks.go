package llm

import (
	"encoding/json"
	"fmt"

	"github.com/sceptyre/maia/internal/render"
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
			Description: "Delegate a research task to a specialized subagent.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"agent": map[string]interface{}{
						"type":        "string",
						"description": "Agent type: 'code' or 'web'",
						"enum":        []string{"code", "web"},
					},
					"task": map[string]interface{}{
						"type":        "string",
						"description": "The task to perform",
					},
				},
				"required": []string{"agent", "task"},
			},
		},
	}
}

// TaskHandler creates a handler for the task tool
func TaskHandler(workDir string) func(ToolCall) (string, error) {
	codeAgent := NewAgent(
		"code-researcher",
		"You scan codebases and report what exists. Return file paths and key code snippets. Describe patterns, conventions, and relationships you observe. Do not suggest changes or implementations.",
		ReadOnlyTools,
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

	webAgent := NewAgent(
		"web-researcher",
		"You find documentation and examples. Return key information with source URLs.",
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
			return "", fmt.Errorf("failed to parse task: %w", err)
		}

		fmt.Printf("\n  📋 %s\n", render.Truncate(args.Task, 120))

		var result string
		var err error

		switch args.Agent {
		case "code":
			result, err = codeAgent.Run(args.Task)
		case "web":
			result, err = webAgent.Run(args.Task)
		default:
			return "", fmt.Errorf("unknown agent: %s", args.Agent)
		}

		if err != nil {
			return "", err
		}

		return result, nil
	}
}

// OrchestratorSystemPrompt returns the system prompt for research orchestrator
func OrchestratorSystemPrompt() string {
	return "You coordinate code and web research.\n\n" +
		"Your job is to discover what exists — not to plan what to build.\n\n" +
		"Task format: \"do <action> with the intent to <goal>\"\n\n" +
		"Rules:\n" +
		"- Generate 2-4 specific tasks\n" +
		"- Each task: specific action + clear intent\n" +
		"- Use prior results to inform next tasks\n\n" +
		"Research output must be structured with these sections:\n" +
		"## Relevant Files\nList files related to the change. For each: path, purpose, key functions/types.\n" +
		"## Code Patterns\nExisting conventions: error handling, naming, project structure, testing patterns.\n" +
		"## External Research\nRelevant docs, APIs, libraries, or examples found on the web. Include source URLs.\n" +
		"## Key Observations\nDependencies, constraints, or relationships that affect implementation.\n" +
		"## Risks\nUncertainties, missing information, or areas that need clarification."
}

// BuildOrchestratorMessages creates the initial messages for the orchestrator
func BuildOrchestratorMessages(changeContent string) []Message {
	userPrompt := "## Change Request\n\n" + changeContent + "\n\n---\n\n" +
		"Discover what exists in this codebase related to the change.\n" +
		"Generate 2-4 tasks:\n" +
		"1. Code task to find relevant files and patterns\n" +
		"2. Web task if external APIs or libraries are involved\n\n" +
		"Each task: \"do <action> with the intent to <goal>\""

	return []Message{
		NewMessage("system", OrchestratorSystemPrompt()),
		NewMessage("user", userPrompt),
	}
}

// RunOrchestrator runs the orchestrator with task delegation
func RunOrchestrator(changeContent, workDir string) (string, error) {
	client := NewClient()
	taskHandler := TaskHandler(workDir)
	taskTool := TaskTool(nil, nil)
	messages := BuildOrchestratorMessages(changeContent)

	response, _, err := client.GetResponseWithTools(messages, []Tool{taskTool}, taskHandler)
	return response, err
}

// RunOrchestratorWithReformat runs orchestrator then synthesizes
func RunOrchestratorWithReformat(changeContent, workDir string) (string, error) {
	client := NewClient()
	taskHandler := TaskHandler(workDir)
	taskTool := TaskTool(nil, nil)
	messages := BuildOrchestratorMessages(changeContent)

	response, allMessages, err := client.GetResponseWithTools(messages, []Tool{taskTool}, taskHandler)
	if err != nil {
		return "", err
	}

	if len(response) > 100 {
		return response, nil
	}

	synthPrompt := "Synthesize all research findings into the output template:\n" +
		"## Relevant Files\n## Code Patterns\n## External Research\n## Key Observations\n## Risks\n\n" +
		"Fill in each section with the findings from the tasks."

	allMessages = append(allMessages,
		NewMessage("assistant", response),
		NewMessage("user", synthPrompt),
	)

	synthesis, _ := client.GetResponse(allMessages)
	if synthesis != "" {
		return synthesis, nil
	}

	return response, nil
}
