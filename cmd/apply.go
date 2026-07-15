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
		"For each phase, generate ONE task:\n" +
		"implement phase <n>: <name> with the intent to <goal> using the following plan:\n" +
		"- Artifact: <file> (<action>) - <description>\n" +
		"- Code: <exact code from plan>\n\n" +
		"Include all artifacts and code from the phase. Be explicit. Never read files."

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

	implementationAgent := llm.NewAgent(
		"implementer",
		"You write code to files as instructed. Use the code provided. Match existing style. Report what you wrote.",
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
