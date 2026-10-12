package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/sceptyre/maia/internal/git"
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

		// Stage all changes except maia's own state dir - it must never
		// land in a commit or a pushed branch, even if the local exclude
		// file is missing.
		fmt.Printf("Staging changes in %s...\n", worktreePath)

		if addErr := stageWorktreeChanges(worktreePath); addErr != nil {
			fmt.Println(addErr)
			return addErr
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

// stageWorktreeChanges stages every change in the worktree except maia's
// own state dir, which must never enter a commit or a pushed branch. The
// local exclude is (re)written first so git add skips .maia even for
// worktrees that predate it; if that fails we refuse to stage rather than
// risk sweeping state files into the commit.
func stageWorktreeChanges(worktree string) error {
	if err := git.EnsureLocalExclude(worktree, state.StateDir); err != nil {
		return fmt.Errorf("exclude %s/ from git: %w", state.StateDir, err)
	}
	gitAdd := exec.Command("git", "add", "-A")
	gitAdd.Dir = worktree
	if out, err := gitAdd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func init() {
	rootCmd.AddCommand(commitCmd)
}
