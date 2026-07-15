package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sceptyre/maia/internal/llm"
	"github.com/sceptyre/maia/internal/state"
	"github.com/spf13/cobra"
)

var (
	dryRun bool
	phase  int
	force  bool
)

var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Execute planned changes",
	Long: `Execute the implementation plan phase by phase.

The orchestrator parses each phase and tasks an implementation agent for each artifact.

Use --phase N to execute only a specific phase.
Use --dry-run to preview without making changes.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Verify we're in a maia worktree
		if !state.HasWorktree() {
			return fmt.Errorf("not in a maia worktree. Run 'maia new' first, then cd to the worktree")
		}

		// Check for plan
		if !state.HasPlan() {
			return fmt.Errorf("plan.md not found. Run 'maia plan' first")
		}

		// Check status
		status, _ := state.GetStatus()
		if status != "planned" && status != "applying" {
			return fmt.Errorf("plan not ready (status: %s). Run 'maia plan' first", status)
		}

		// Read plan.md
		planMD, err := os.ReadFile(filepath.Join(state.GetGeneratedDir(), "plan.md"))
		if err != nil {
			return fmt.Errorf("failed to read plan: %w", err)
		}

		fmt.Println("═══════════════════════════════════════════════════════════════")
		fmt.Println("  MAIA APPLY - Executing Implementation Plan")
		fmt.Println("═══════════════════════════════════════════════════════════════")
		fmt.Println(strings.Repeat("─", 60))

		// Update status
		state.SetStatus("applying")

		// Initialize LLM client
		client := llm.NewClient()
		if client.APIKey == "" {
			return fmt.Errorf("API key not configured. Set in ~/.maia/config.json or export OPENAI_API_KEY")
		}

		cwd, _ := os.Getwd()

		// Run orchestrator
		fmt.Println("\n▶ Starting implementation...\n")

		result, err := runApplyOrchestrator(client, string(planMD), cwd, phase, dryRun)
		if err != nil {
			state.SetStatus("error")
			return fmt.Errorf("implementation failed: %w", err)
		}

		// Update status
		state.SetStatus("applied")

		fmt.Println(strings.Repeat("─", 60))
		fmt.Println("\n✓ Implementation complete!")
		fmt.Printf("\n%s\n", result)
		fmt.Println("\nNext steps:")
		fmt.Println("  Review: git diff")
		fmt.Println("  Commit: git add . && git commit")
		fmt.Println("  Merge:  maia merge")

		return nil
	},
}

func runApplyOrchestrator(client *llm.Client, planContent, workDir string, targetPhase int, dryRun bool) (string, error) {
	systemPrompt := "You are an implementation orchestrator.\n\n" +
		"You receive an implementation plan with phases. Read it and task the agent to implement each phase.\n\n" +
		"TASK FORMAT:\n" +
		"implement phase <number>: <phase name> with the intent to <goal> using the following plan:\n" +
		"<detailed list of artifacts, behavior, and code samples>\n\n" +
		"Each task must be VERBOSE and EXPLICIT:\n" +
		"- Include ALL artifacts from the phase\n" +
		"- For each artifact: file path, action (create/modify), what it does\n" +
		"- Include the COMPLETE code sample from the plan\n" +
		"- Include any dependencies or ordering between artifacts\n" +
		"- Include expected behavior and outcomes\n\n" +
		"EXAMPLE of a complete task:\n" +
		"implement phase 1: add auth with the intent to set up authentication infrastructure using the following plan:\n" +
		"\n" +
		"Artifact 1: pkg/auth/handler.go (create)\n" +
		"Purpose: HTTP handlers for authentication endpoints\n" +
		"Behavior:\n" +
		"- Handler struct holds userStore and tokenService dependencies\n" +
		"- Login method accepts JSON with username and password\n" +
		"- Validates credentials against user store\n" +
		"- Returns JWT token on success, 401 on failure\n" +
		"Code:\n" +
		"package auth\n\ntype Handler struct { ... }\n\nfunc (h *Handler) Login(w, r) { ... }\n\n" +
		"Artifact 2: pkg/auth/middleware.go (create)\n" +
		"Purpose: JWT authentication middleware for protected routes\n" +
		"Behavior:\n" +
		"- Extracts token from Authorization header\n" +
		"- Validates token signature and expiry\n" +
		"- Adds user to request context\n" +
		"- Returns 401 if invalid\n" +
		"Code:\n" +
		"package auth\n\nfunc AuthMiddleware(next) { ... }\n\n" +
		"Rules:\n" +
		"- Read each phase in the plan\n" +
		"- For EACH phase, generate ONE verbose task\n" +
		"- Include ALL artifacts with full details\n" +
		"- Include COMPLETE code samples from plan\n" +
		"- Execute phases in order\n" +
		"- NEVER ask to read files"

	phaseDesc := "All phases"
	if targetPhase > 0 {
		phaseDesc = fmt.Sprintf("Phase %d only", targetPhase)
	}

	userPrompt := "## Implementation Plan\n\n" + planContent + "\n\n---\n\n" +
		"Execute this plan.\n\n" +
		"Target: " + phaseDesc + "\n\n" +
		"For each Phase:\n" +
		"1. Find the Artifact table\n" +
		"2. For EACH artifact row, create a task using EXACTLY this format:\n" +
		"   do write [filepath] with the intent to [description], using this code: [code from plan]\n" +
		"3. Wait for completion, then next artifact\n\n" +
		"Start Phase 1 now."

	// Implementation agent - only writes files
	implementationAgent := llm.NewAgent(
		"implementer",
		"You implement code by writing files.\n"+
			"When given a task with code, write that code to the specified file.\n"+
			"Do not read files first - use the code provided in the task.\n"+
			"Report what file you wrote.",
		llm.FileTools,
		func(call llm.ToolCall) (string, error) {
			fmt.Printf("    [impl] 🔧 %s", call.Function.Name)
			var args map[string]string
			json.Unmarshal([]byte(call.Function.Arguments), &args)
			if path, ok := args["path"]; ok && path != "" {
				fmt.Printf(" %s", path)
			}
			fmt.Println()

			if dryRun && call.Function.Name == "write_file" {
				return "Dry run - not written", nil
			}

			return llm.HandleToolCall(call, workDir)
		},
	)

	// Task handler that delegates to implementation agent
	taskHandler := func(call llm.ToolCall) (string, error) {
		var args struct {
			Agent string `json:"agent"`
			Task  string `json:"task"`
		}
		if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
			return "", fmt.Errorf("failed to parse task: %w", err)
		}

		fmt.Printf("\n  📋 %s\n", truncateString(args.Task, 150))

		result, err := implementationAgent.Run(args.Task)
		if err != nil {
			return "", err
		}

		return result, nil
	}

	taskTool := llm.TaskTool(nil, nil)

	messages := []llm.Message{
		llm.NewMessage("system", systemPrompt),
		llm.NewMessage("user", userPrompt),
	}

	response, _, err := client.GetResponseWithTools(messages, []llm.Tool{taskTool}, taskHandler)
	return response, err
}

func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func init() {
	applyCmd.Flags().BoolVarP(&dryRun, "dry-run", "d", false, "Preview changes without applying")
	applyCmd.Flags().IntVarP(&phase, "phase", "p", 0, "Execute specific phase (0 = all)")
	applyCmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation prompts")
	rootCmd.AddCommand(applyCmd)
}
