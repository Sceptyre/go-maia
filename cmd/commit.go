package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/sceptyre/maia/internal/state"
	"github.com/spf13/cobra"
)

var commitCmd = &cobra.Command{
	Use:   "commit <slug> <message>",
	Short: "Commit changes in a worktree",
	Long: `Stage all changes and create a git commit in the specified worktree.

This runs from the parent branch — you don't need to cd into the worktree.

Examples:
  maia commit my-slug "add login handler"
  maia commit fix-typo "fix typo in readme"`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		slug := args[0]
		message := args[1]

		worktreePath, err := state.ValidateWorktreeExists(slug)
		if err != nil {
			return err
		}

		// Stage all changes
		fmt.Printf("Staging changes in %s...\n", worktreePath)

		gitAdd := exec.Command("git", "add", "-A")
		gitAdd.Dir = worktreePath
		if out, addErr := gitAdd.CombinedOutput(); addErr != nil {
			fmt.Println(string(out))
			return fmt.Errorf("git add failed: %w", addErr)
		}

		// Check if there's anything to commit
		gitDiffQuiet := exec.Command("git", "diff", "--cached", "--quiet")
		gitDiffQuiet.Dir = worktreePath
		if err := gitDiffQuiet.Run(); err == nil {
			// Exit code 0 means nothing staged
			return fmt.Errorf("nothing to commit — no changes detected")
		}

		// Commit
		fmt.Printf("Committing: %s\n", message)

		gitCommit := exec.Command("git", "commit", "-m", message)
		gitCommit.Dir = worktreePath
		gitCommit.Stdout = os.Stdout
		gitCommit.Stderr = os.Stderr

		if err := gitCommit.Run(); err != nil {
			return fmt.Errorf("git commit failed: %w", err)
		}

		fmt.Println("\n✓ Changes committed")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(commitCmd)
}