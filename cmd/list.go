package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/oblongata/maia/internal/state"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List maia worktrees for current repo",
	Long:  `List all maia worktrees for the current repository.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		repoDir, err := state.GetRepoWorktreeDir()
		if err != nil {
			return err
		}

		worktrees, err := state.ListWorktrees()
		if err != nil {
			return err
		}

		repoName, _ := state.GetRepoName()
		fmt.Printf("Worktrees for %s:\n", repoName)
		fmt.Printf("Location: %s\n\n", repoDir)

		if len(worktrees) == 0 {
			fmt.Println("  No active worktrees. Create one with 'maia new <description>'")
			return nil
		}

		for _, wt := range worktrees {
			path := filepath.Join(repoDir, wt)

			// Check if .maia exists
			maiaPath := filepath.Join(path, state.StateDir)
			hasMaia := false
			if _, err := os.Stat(maiaPath); err == nil {
				hasMaia = true
			}

			status := ""
			if !hasMaia {
				status = " [no .maia]"
			}

			fmt.Printf("  %s%s\n", wt, status)
			fmt.Printf("    %s\n", path)
		}

		fmt.Println()
		fmt.Printf("Total: %d worktree(s)\n", len(worktrees))

		return nil
	},
}

func init() {
	rootCmd.AddCommand(listCmd)
}
