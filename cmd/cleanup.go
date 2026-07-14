package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/oblongata/maia/internal/git"
	"github.com/oblongata/maia/internal/state"
	"github.com/spf13/cobra"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup [slug]",
	Short: "Remove worktree and clean up",
	Long:  `Remove a maia worktree. If no slug provided, removes current worktree.`,
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var worktreePath string

		if len(args) > 0 {
			// Remove specified worktree
			repoDir, err := state.GetRepoWorktreeDir()
			if err != nil {
				return err
			}
			worktreePath = filepath.Join(repoDir, args[0])
		} else {
			// Remove current worktree
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}

			// Check if we're in a maia worktree
			if !state.HasWorktree() {
				return fmt.Errorf("not in a maia worktree. Provide a slug: maia cleanup <slug>")
			}

			// Get main dir
			mainDir, err := git.GetMainDir()
			if err != nil {
				return err
			}

			// Verify we're not in main
			if cwd == mainDir {
				return fmt.Errorf("cannot remove main worktree. Provide a slug: maia cleanup <slug>")
			}

			worktreePath = cwd
		}

		// Check if worktree exists
		if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
			return fmt.Errorf("worktree not found: %s", worktreePath)
		}

		fmt.Printf("Removing worktree: %s\n", worktreePath)

		// Remove worktree
		if err := git.Remove(worktreePath); err != nil {
			return err
		}

		fmt.Println("\n✓ Cleanup complete")
		fmt.Printf("  Removed: %s\n", worktreePath)

		// If we removed our current directory, change to main
		cwd, _ := os.Getwd()
		if cwd == worktreePath {
			mainDir, _ := git.GetMainDir()
			fmt.Printf("  Returning to: %s\n", mainDir)
			os.Chdir(mainDir)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(cleanupCmd)
}
