package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/sceptyre/maia/internal/git"
	"github.com/sceptyre/maia/internal/state"
	"github.com/spf13/cobra"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup [slug]",
	Short: "Detach a change request worktree",
	Long:  `Remove a maia worktree while retaining its change request and saved documents. If no slug is provided, removes the current worktree.`,
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var worktreePath string

		if len(args) > 0 {
			// Remove specified worktree
			resolved, err := state.ValidateWorktreeExists(args[0])
			if err != nil {
				return err
			}
			worktreePath = resolved
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

		repoRoot, err := state.RepositoryForWorktree(worktreePath)
		if err != nil {
			return err
		}
		if _, err := state.SyncChangeRequests(repoRoot); err != nil {
			return fmt.Errorf("save change request before cleanup: %w", err)
		}
		slug := filepath.Base(worktreePath)
		cr, found, err := state.GetChangeRequest(repoRoot, slug)
		if err != nil {
			return err
		}
		cwd, _ := os.Getwd()
		returnToRepo := filepath.Clean(cwd) == filepath.Clean(worktreePath)
		restore := func() {}
		if found {
			restore, err = stashUntrackedChangeFiles(worktreePath, cr)
			if err != nil {
				return err
			}
		}
		fmt.Printf("Removing worktree: %s\n", worktreePath)

		// Remove the checkout while retaining the change request record.
		if err := git.Remove(worktreePath); err != nil {
			restore()
			return err
		}
		if found {
			if err := state.DetachChangeWorktree(repoRoot, slug); err != nil {
				return fmt.Errorf("detach change request worktree: %w", err)
			}
		}

		fmt.Println("\n✓ Cleanup complete")
		fmt.Printf("  Removed: %s\n", worktreePath)

		if returnToRepo {
			fmt.Printf("  Returning to: %s\n", repoRoot)
			if err := os.Chdir(repoRoot); err != nil {
				return err
			}
		}

		return nil
	},
}

func stashUntrackedChangeFiles(worktree string, cr *state.ChangeRequest) (func(), error) {
	files := map[string]string{".maia/change.md": cr.Change}
	for name, contents := range cr.Artifacts {
		if name == "research" || name == "plan" {
			files[filepath.Join(state.GetGeneratedDir(), name+".md")] = contents
		}
	}
	var removed []string
	for relative := range files {
		path := filepath.Join(worktree, relative)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		tracked := exec.Command("git", "-C", worktree, "ls-files", "--error-unmatch", relative)
		if err := tracked.Run(); err == nil {
			continue
		} else if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 1 {
			return nil, err
		}
		if err := os.Remove(path); err != nil {
			for _, prior := range removed {
				_ = restoreChangeFile(worktree, prior, files[prior])
			}
			return nil, err
		}
		removed = append(removed, relative)
	}
	return func() {
		for _, relative := range removed {
			_ = restoreChangeFile(worktree, relative, files[relative])
		}
	}, nil
}

func restoreChangeFile(worktree, relative, contents string) error {
	path := filepath.Join(worktree, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(contents), 0644)
}

func init() {
	rootCmd.AddCommand(cleanupCmd)
}
