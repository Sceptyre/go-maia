package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/sceptyre/maia/internal/state"
	"github.com/spf13/cobra"
)

var diffCmd = &cobra.Command{
	Use:   "diff [slug]",
	Short: "Show git diff for a worktree",
	Long: `Show uncommitted changes in a worktree.

Without a slug, shows the diff for the current worktree.
With a slug, shows the diff for the specified worktree (works from the parent branch).

Examples:
  maia diff           # Show diff in current worktree
  maia diff my-slug   # Show diff for 'my-slug' worktree`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var worktreePath string

		if len(args) > 0 {
			resolved, err := state.ValidateWorktreeExists(args[0])
			if err != nil {
				return err
			}
			worktreePath = resolved
		} else {
			if !state.HasWorktree() {
				return fmt.Errorf("not in a maia worktree. Provide a slug: maia diff <slug>")
			}
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			worktreePath = cwd
		}

		// Run git diff (unstaged changes)
		gitDiff := exec.Command("git", "diff")
		gitDiff.Dir = worktreePath
		gitDiff.Stdout = os.Stdout
		gitDiff.Stderr = os.Stderr

		if err := gitDiff.Run(); err != nil {
			return fmt.Errorf("git diff failed: %w", err)
		}

		// Show staged changes
		gitCached := exec.Command("git", "diff", "--cached")
		gitCached.Dir = worktreePath
		stagedOut, err := gitCached.Output()
		if err == nil && len(stagedOut) > 0 {
			fmt.Println("\n── Staged changes ──")
			fmt.Print(string(stagedOut))
		}

		// Show untracked file count
		gitStatus := exec.Command("git", "status", "--porcelain")
		gitStatus.Dir = worktreePath
		statusOut, err := gitStatus.Output()
		if err == nil {
			lines := strings.Split(strings.TrimSpace(string(statusOut)), "\n")
			untracked := 0
			for _, line := range lines {
				if strings.HasPrefix(strings.TrimSpace(line), "??") {
					untracked++
				}
			}
			if untracked > 0 {
				fmt.Printf("\n%d untracked file(s) (use 'git add' to include)\n", untracked)
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(diffCmd)
}

