package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sceptyre/maia/internal/git"
	"github.com/sceptyre/maia/internal/state"
	"github.com/spf13/cobra"
)

var mergeCmd = &cobra.Command{
	Use:   "merge [slug]",
	Short: "Merge changes back to main",
	Long: `Merge the worktree branch back into the base branch.

With a slug, merges the specified worktree from the parent branch.
Without a slug, assumes you're currently in the worktree (legacy behavior).

Examples:
  maia merge my-slug   # Merge 'my-slug' worktree from parent branch
  maia merge           # Merge current worktree (must be in worktree)

This is a destructive operation — commit all changes before merging.`,
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
			// No slug — assume CWD is the worktree (legacy behavior)
			if !state.HasWorktree() {
				return fmt.Errorf("not in a maia worktree. Provide a slug: maia merge <slug>")
			}
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			worktreePath = cwd
		}

		mainDir, err := git.GetMainDir()
		if err != nil {
			return err
		}

		// Get current branch of the worktree using git -C
		getBranch := exec.Command("git", "-C", worktreePath, "rev-parse", "--abbrev-ref", "HEAD")
		out, err := getBranch.Output()
		if err != nil {
			return fmt.Errorf("failed to get branch for worktree: %w", err)
		}
		branch := strings.TrimSpace(string(out))

		// Get the current branch in main repo
		getMainBranch := exec.Command("git", "-C", mainDir, "rev-parse", "--abbrev-ref", "HEAD")
		mainOut, err := getMainBranch.Output()
		if err != nil {
			return fmt.Errorf("failed to get main branch: %w", err)
		}
		mainBranch := strings.TrimSpace(string(mainOut))

		// Get description from change.md in the worktree
		description := "change"
		changeMDPath := filepath.Join(worktreePath, state.StateDir, "change.md")
		changeMD, err := os.ReadFile(changeMDPath)
		if err == nil {
			lines := strings.Split(string(changeMD), "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "description:") {
					description = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
					break
				}
			}
		}

		fmt.Printf("Merging branch: %s\n", branch)
		fmt.Printf("Into: %s (%s)\n", mainBranch, mainDir)

		commands := [][]string{
			{"git", "-C", mainDir, "checkout", mainBranch},
			{"git", "-C", mainDir, "merge", branch, "--no-ff", "-m", fmt.Sprintf("maia: %s", description)},
		}

		for _, c := range commands {
			mergeCmd := exec.Command(c[0], c[1:]...)
			if out, err := mergeCmd.CombinedOutput(); err != nil {
				fmt.Println(string(out))
				return err
			}
		}

		fmt.Println("\n✓ Changes merged to", mainBranch)
		fmt.Printf("  Run 'maia cleanup %s' to remove the worktree\n", filepath.Base(worktreePath))

		return nil
	},
}

func init() {
	rootCmd.AddCommand(mergeCmd)
}
