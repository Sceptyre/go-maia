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

var planCmd = &cobra.Command{
	Use:   "plan [slug]",
	Short: "Generate implementation plan",
	Long: `Uses change.md (goals) and research.md (codebase analysis) to generate
a detailed implementation plan with phases, artifacts, and code samples.

Output is saved to .maia/.generated/plan.md.

Without a slug, runs in the current worktree.
With a slug, runs from the parent branch against the specified worktree.

Examples:
  maia plan              # Plan current worktree
  maia plan my-slug      # Plan 'my-slug' from parent branch`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var worktreePath string

		if len(args) > 0 {
			// Slug provided — resolve from parent repo
			resolved, err := state.ValidateWorktreeExists(args[0])
			if err != nil {
				return err
			}
			worktreePath = resolved
		} else {
			// No slug — assume CWD is a worktree
			if !state.HasWorktree() {
				return fmt.Errorf("not in a maia worktree. Provide a slug: maia plan <slug>")
			}
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			worktreePath = cwd
		}

		// Check for required files using path-aware helpers
		if !state.HasWorktreeIn(worktreePath) {
			return fmt.Errorf("not a maia worktree: %s", worktreePath)
		}
		if !state.HasChangeFileIn(worktreePath) {
			return fmt.Errorf("change.md not found. Run 'maia new' first")
		}
		if !state.HasResearchIn(worktreePath) {
			return fmt.Errorf("research.md not found. Run 'maia init' first")
		}

		// Read change.md using absolute path
		changeMD, err := os.ReadFile(filepath.Join(worktreePath, state.StateDir, "change.md"))
		if err != nil {
			return fmt.Errorf("failed to read change.md: %w", err)
		}

		// Read research.md using absolute path
		researchMD, err := os.ReadFile(filepath.Join(worktreePath, state.GetGeneratedDir(), "research.md"))
		if err != nil {
			return fmt.Errorf("failed to read research.md: %w", err)
		}

		// Clean the markdown
		changeContent := cleanMarkdown(string(changeMD))

		fmt.Println("═══════════════════════════════════════════════════════════════")
		fmt.Println("  MAIA PLAN - Implementation Plan Generation")
		fmt.Println("═══════════════════════════════════════════════════════════════")
		fmt.Println(strings.Repeat("─", 60))

		// Initialize LLM client
		llm.UseSessionIDFor(worktreePath, "")
		client := llm.NewClient()
		if client.APIKey == "" {
			return fmt.Errorf("API key not configured. Set in ~/.maia/config.json or export OPENAI_API_KEY")
		}

		fmt.Printf("\n▶ Generating implementation plan...\n")
		fmt.Printf("  Model: %s\n\n", client.Model)

		// Create tool handler — use worktreePath as the working directory
		toolHandler := func(call llm.ToolCall) (string, error) {
			fmt.Printf("  🔧 %s", call.Function.Name)
			var args map[string]string
			_ = json.Unmarshal([]byte(call.Function.Arguments), &args)
			if path, ok := args["path"]; ok && path != "" {
				fmt.Printf(" %s", path)
			}
			fmt.Println()
			return llm.HandleToolCall(call, worktreePath)
		}

		// Run the three-step plan generation pipeline
		plan, err := llm.GeneratePlan(
			client,
			changeContent,
			string(researchMD),
			toolHandler,
			llm.DefaultPlanConfig(),
		)
		if err != nil {
			return fmt.Errorf("plan generation failed: %w", err)
		}

		fmt.Println("\n▶ Writing plan document...")

		// Write plan.md using absolute path
		if err := os.WriteFile(filepath.Join(worktreePath, state.GetGeneratedDir(), "plan.md"), []byte(plan), 0644); err != nil {
			return fmt.Errorf("failed to write plan: %w", err)
		}

		// Update status using path-aware helper
		if err := state.SetStatusIn(worktreePath, "planned"); err != nil {
			return err
		}

		fmt.Println(strings.Repeat("─", 60))
		fmt.Println("\n✓ Plan generated!")
		fmt.Printf("\nOutput: %s\n", filepath.Join(worktreePath, state.GetGeneratedDir(), "plan.md"))
		fmt.Println("\nReview the plan, then run: maia apply")

		return nil
	},
}

func init() {
	rootCmd.AddCommand(planCmd)
}
