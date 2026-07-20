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

Use --phase N to execute only a specific phase.
Use --dry-run to preview without making changes.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !state.HasWorktree() {
			return fmt.Errorf("not in a maia worktree. Run 'maia new' first")
		}
		if !state.HasPlan() {
			return fmt.Errorf("plan.md not found. Run 'maia plan' first")
		}

		status, _ := state.GetStatus()
		if status != "planned" && status != "applying" {
			return fmt.Errorf("plan not ready (status: %s). Run 'maia plan' first", status)
		}

		planMD, err := os.ReadFile(filepath.Join(state.GetGeneratedDir(), "plan.md"))
		if err != nil {
			return fmt.Errorf("failed to read plan: %w", err)
		}

		fmt.Println("═══════════════════════════════════════════════════════════════")
		fmt.Println("  MAIA APPLY - Executing Implementation Plan")
		fmt.Println("═══════════════════════════════════════════════════════════════")
		fmt.Println(strings.Repeat("─", 60))

		state.SetStatus("applying")

		client := llm.NewClient()
		if client.APIKey == "" {
			return fmt.Errorf("API key not configured")
		}

		cwd, _ := os.Getwd()

		fmt.Println("\n▶ Starting implementation...\n")

		result, err := runApplyOrchestrator(client, string(planMD), cwd, phase, dryRun)
		if err != nil {
			state.SetStatus("error")
			return fmt.Errorf("implementation failed: %w", err)
		}

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
	systemPrompt := "You task an implementation agent to execute phases from a plan.\n\n" +
		"For each phase, generate ONE task that covers ALL artifacts in that phase.\n" +
		"Keep phases small — if a phase has more than 3 artifacts, split it.\n\n" +
		"Task format:\n" +
		"do implement phase <n>: <name>\n" +
		"intent: <what this phase accomplishes>\n\n" +
		"Then list every artifact:\n" +
		"- create <filepath>: <what this file does>\n" +
		"  ```go\n<full file content>\n```\n" +
		"- modify <filepath> (add): <what to add and WHERE>\n" +
		"  ```go\n<new code to insert>\n```\n" +
		"- modify <filepath> (replace): <what to replace>\n" +
		"  old:\n```go\n<old code>\n```\n" +
		"  new:\n```go\n<new code>\n```\n\n" +
		"Rules:\n" +
		"- Include every artifact from the phase's table.\n" +
		"- For create: include the full file code.\n" +
		"- For modify add: include only the new code and say where it goes.\n" +
		"- For modify replace: include both old and new code.\n" +
		"- Modify actions require reading the file first.\n\n" +
		"Wait for each phase to complete before generating the next."

	phaseDesc := "All phases"
	if targetPhase > 0 {
		phaseDesc = fmt.Sprintf("Phase %d only", targetPhase)
	}

	userPrompt := "## Implementation Plan\n\n" + planContent + "\n\n---\n\n" +
		"Execute this plan.\n\n" +
		"Target: " + phaseDesc + "\n\n" +
		"For each Phase:\n" +
		"1. Read the phase's artifact table and code blocks\n" +
		"2. Generate ONE task covering all artifacts in that phase\n" +
		"3. Wait for completion, then next phase\n\n" +
		"Start Phase 1 now."

	implementationAgent := llm.NewAgent(
		"implementer",
		`You implement code changes as instructed.

- create: write the provided code to the file.
- modify add: read the file, insert code at the specified location, write the complete updated file.
- modify replace: read the file, find the old code, replace it with the new code, write the complete updated file.
- modify update: write the provided complete file content.

Report what you did.`,

		llm.FileTools,
		func(call llm.ToolCall) (string, error) {
			fmt.Printf("    [impl] 🔧 %s", call.Function.Name)
			var args map[string]string
			json.Unmarshal([]byte(call.Function.Arguments), &args)
			if path, ok := args["path"]; ok && path != "" {
				fmt.Printf(" %s", path)
			}
			fmt.Println()

			if dryRun && (call.Function.Name == "write_file" || call.Function.Name == "edit_file") {
				return "Dry run — skipped", nil
			}

			return llm.HandleToolCall(call, workDir)
		},
	)

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

	taskTool := llm.Tool{
		Type: "function",
		Function: llm.ToolFunction{
			Name:        "task",
			Description: "Delegate a phase implementation to an agent.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"agent": map[string]interface{}{
						"type":        "string",
						"description": "Agent type: 'implementer'",
						"enum":        []string{"implementer"},
					},
					"task": map[string]interface{}{
						"type":        "string",
						"description": "The implementation task",
					},
				},
				"required": []string{"agent", "task"},
			},
		},
	}

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
