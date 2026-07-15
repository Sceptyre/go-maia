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

The orchestrator parses each phase from the plan and tasks an implementation agent.

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
		fmt.Println("\n▶ Starting implementation orchestrator...\n")

		result, err := runApplyOrchestrator(client, string(planMD), cwd, phase, dryRun, force)
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

func runApplyOrchestrator(client *llm.Client, planContent, workDir string, targetPhase int, dryRun, force bool) (string, error) {
	systemPrompt := `You execute implementation plans by delegating to an implementation agent.

Parse the plan to extract phases and their artifacts.
For each artifact in each phase, create a specific implementation task.

Task format: "do [specific action] with the intent to [goal]"

Rules:
- Execute phases in order
- For each artifact: task the agent to create/modify that file
- Include the code samples from the plan in the task
- Do not research or explore - just execute
- If targetPhase is set, only execute that phase`

	userPrompt := fmt.Sprintf(`## Implementation Plan

%s

---

Execute this plan.

Target phase: %s
Dry run: %v
Force: %v

For each phase:
1. Extract the artifacts (files to create/modify)
2. For each artifact, create a task: "do write [filepath] with the intent to [description from plan], using this code: [code sample from plan]"
3. Execute each task with the implementation agent

Start executing phase by phase.`, planContent,
		func() string {
			if targetPhase > 0 {
				return fmt.Sprintf("Phase %d only", targetPhase)
			}
			return "All phases"
		}(),
		dryRun, force)

	// Create implementation agent
	implementationTools := llm.FileTools
	implementationAgent := llm.NewAgent(
		"implementer",
		`You implement code changes. Read files to understand context, then write/modify files as instructed.

Rules:
- Follow instructions exactly
- Match existing code style
- Report what you did`,
		implementationTools,
		func(call llm.ToolCall) (string, error) {
			fmt.Printf("    [impl] 🔧 %s", call.Function.Name)
			var args map[string]string
			json.Unmarshal([]byte(call.Function.Arguments), &args)
			if path, ok := args["path"]; ok && path != "" {
				fmt.Printf(" %s", path)
			}
			fmt.Println()

			if dryRun && call.Function.Name == "write_file" {
				return "Dry run - file not written", nil
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

	result, _, err := client.GetResponseWithTools(messages, []llm.Tool{taskTool}, taskHandler)
	return result, err
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
