package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sceptyre/maia/internal/llm"
	"github.com/sceptyre/maia/internal/state"
	"github.com/spf13/cobra"
)

var steerCmd = &cobra.Command{
	Use:   "steer <feedback> [slug]",
	Short: "Adjust direction based on feedback",
	Long: `Provide feedback to adjust the plan or research.
This helps redirect AI efforts based on your review.

Without a slug, runs in the current worktree.
With a slug, runs from the parent branch against the specified worktree.

Examples:
  maia steer "too many phases"           # Steer current worktree
  maia steer "too many phases" my-slug   # Steer 'my-slug' from parent branch`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		feedback := args[0]

		var worktreePath string

		if len(args) > 1 {
			// Slug provided — resolve from parent repo
			resolved, err := state.ValidateWorktreeExists(args[1])
			if err != nil {
				return err
			}
			worktreePath = resolved
		} else {
			// No slug — assume CWD is a worktree
			if !state.HasWorktree() {
				return fmt.Errorf("not in a maia worktree. Provide a slug: maia steer <feedback> <slug>")
			}
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			worktreePath = cwd
		}

		// Verify worktree exists
		if !state.HasWorktreeIn(worktreePath) {
			return fmt.Errorf("not a maia worktree: %s", worktreePath)
		}

		client := llm.NewClient()
		if client.APIKey == "" {
			return fmt.Errorf("API key not configured. Set in ~/.maia/config.json or export OPENAI_API_KEY")
		}

		fmt.Println("═══════════════════════════════════════════════════════════════")
		fmt.Println("  MAIA STEER - Adjusting Direction")
		fmt.Println("═══════════════════════════════════════════════════════════════")
		fmt.Println(strings.Repeat("─", 60))
		fmt.Printf("\nFeedback: %s\n\n", feedback)

		// Check what stage we're at and steer accordingly
		hasPlan := state.HasPlanIn(worktreePath)
		hasResearch := state.HasResearchIn(worktreePath)

		if hasPlan {
			fmt.Println("▶ Adjusting plan based on feedback...\n")
			if err := steerPlan(client, feedback, worktreePath); err != nil {
				return fmt.Errorf("failed to steer plan: %w", err)
			}
			fmt.Println(strings.Repeat("─", 60))
			fmt.Println("\n✓ Plan adjusted!")
			fmt.Println("\nNext step: maia apply")
		} else if hasResearch {
			fmt.Println("▶ Adjusting research based on feedback...\n")
			if err := steerResearchDoc(client, feedback, worktreePath); err != nil {
				return fmt.Errorf("failed to steer research: %w", err)
			}
			fmt.Println(strings.Repeat("─", 60))
			fmt.Println("\n✓ Research adjusted!")
			fmt.Println("\nNext step: maia plan")
		} else {
			return fmt.Errorf("no research or plan found to steer. Run 'maia init' first")
		}

		return nil
	},
}

func steerPlan(client *llm.Client, feedback, worktreePath string) error {
	// Read current plan using absolute path
	planData, err := os.ReadFile(filepath.Join(worktreePath, state.GetGeneratedDir(), "plan.md"))
	if err != nil {
		return fmt.Errorf("failed to read plan: %w", err)
	}

	// Read change.md for context using absolute path
	changeData, err := os.ReadFile(filepath.Join(worktreePath, state.StateDir, "change.md"))
	if err != nil {
		return fmt.Errorf("failed to read change.md: %w", err)
	}

	response, err := client.GetResponse([]llm.Message{
		llm.NewMessage("system", "You are adjusting an implementation plan based on user feedback. Keep the plan focused and actionable. Output the complete updated plan."),
		llm.NewMessage("user", fmt.Sprintf("## Current Plan\n\n%s\n\n## Change Request\n\n%s\n\n## Feedback\n\n%s\n\nPlease adjust the plan based on this feedback. Output the complete updated plan.", string(planData), string(changeData), feedback)),
	})
	if err != nil {
		return err
	}

	// Write updated plan using absolute path
	if err := os.WriteFile(filepath.Join(worktreePath, state.GetGeneratedDir(), "plan.md"), []byte(response), 0644); err != nil {
		return fmt.Errorf("failed to write updated plan: %w", err)
	}

	return nil
}

func steerResearchDoc(client *llm.Client, feedback, worktreePath string) error {
	// Read current research using absolute path
	researchData, err := os.ReadFile(filepath.Join(worktreePath, state.GetGeneratedDir(), "research.md"))
	if err != nil {
		return fmt.Errorf("failed to read research: %w", err)
	}

	// Read change.md for context using absolute path
	changeData, err := os.ReadFile(filepath.Join(worktreePath, state.StateDir, "change.md"))
	if err != nil {
		return fmt.Errorf("failed to read change.md: %w", err)
	}

	response, err := client.GetResponse([]llm.Message{
		llm.NewMessage("system", "You are adjusting research findings based on user feedback. Expand or redirect the research as needed. Output the complete updated research document."),
		llm.NewMessage("user", fmt.Sprintf("## Current Research\n\n%s\n\n## Change Request\n\n%s\n\n## Feedback\n\n%s\n\nPlease adjust the research based on this feedback. Output the complete updated research document.", string(researchData), string(changeData), feedback)),
	})
	if err != nil {
		return err
	}

	// Write updated research using absolute path
	if err := os.WriteFile(filepath.Join(worktreePath, state.GetGeneratedDir(), "research.md"), []byte(response), 0644); err != nil {
		return fmt.Errorf("failed to write updated research: %w", err)
	}

	return nil
}

func init() {
	rootCmd.AddCommand(steerCmd)
}