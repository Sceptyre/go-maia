package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/oblongata/maia/internal/git"
	"github.com/oblongata/maia/internal/state"
	"github.com/spf13/cobra"
)

var mergeCmd = &cobra.Command{
	Use:   "merge",
	Short: "Merge changes back to main",
	Long:  `Merge the worktree branch back into the base branch and clean up.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Check if we're in a maia worktree
		if !state.HasWorktree() {
			return fmt.Errorf("not in a maia worktree. Run 'maia new' first, then cd to the worktree")
		}

		// Get main directory
		mainDir, err := git.GetMainDir()
		if err != nil {
			return err
		}

		// Get current branch
	 getCurrentBranch := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
		out, err := getCurrentBranch.Output()
		if err != nil {
			return fmt.Errorf("failed to get current branch: %w", err)
		}
		branch := strings.TrimSpace(string(out))

		// Get description from change.md
		description := "change"
		changeMD, err := os.ReadFile(".maia/change.md")
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
		fmt.Printf("Into: %s (main worktree)\n", mainDir)

		// Checkout main and merge
		commands := [][]string{
			{"git", "checkout", baseBranch},
			{"git", "merge", branch, "--no-ff", "-m", fmt.Sprintf("maia: %s", description)},
		}

		for _, args := range commands {
			cmd := exec.Command(args[0], args[1:]...)
			cmd.Dir = mainDir
			if out, err := cmd.CombinedOutput(); err != nil {
				fmt.Println(string(out))
				return fmt.Errorf("merge failed: %w", err)
			}
		}

		fmt.Println("\n✓ Changes merged to", baseBranch)
		fmt.Printf("  Run 'maia cleanup' to remove the worktree\n")

		return nil
	},
}

func init() {
	mergeCmd.Flags().StringVarP(&baseBranch, "base", "b", "main", "Base branch to merge into")
	rootCmd.AddCommand(mergeCmd)
}
