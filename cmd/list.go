package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/sceptyre/maia/internal/state"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List change requests for the current repository",
	Long:  `List all durable change requests and whether each has a materialized worktree.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := state.RepositoryForWorktree(".")
		if err != nil {
			return err
		}
		changes, err := state.SyncChangeRequests(repoRoot)
		if err != nil {
			return err
		}
		repoDir := state.RepoWorktreeDirFor(repoRoot)
		fmt.Printf("Change requests for %s:\n", filepath.Base(repoRoot))
		fmt.Printf("Worktrees: %s\n\n", repoDir)
		if len(changes) == 0 {
			fmt.Println("  No change requests. Create one with 'maia new <description>'.")
			return nil
		}
		materialized := 0
		for _, cr := range changes {
			stateLabel := "[no worktree]"
			if cr.Worktree != nil {
				stateLabel = "[worktree]"
				materialized++
			}
			fmt.Printf("  %s — %s %s\n", cr.Slug, cr.Description, stateLabel)
			fmt.Printf("    status: %s · source: %s\n", cr.Status, cr.SourceBranch)
			if cr.Worktree != nil {
				fmt.Printf("    %s\n", cr.Worktree.Path)
			} else {
				fmt.Printf("    restore with: maia materialize %s\n", cr.Slug)
			}
		}
		fmt.Printf("\nTotal: %d change request(s), %d materialized worktree(s)\n", len(changes), materialized)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(listCmd)
}
