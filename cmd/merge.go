package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sceptyre/maia/internal/state"
	"github.com/spf13/cobra"
)

var mergeBaseBranch string

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
		var change *state.ChangeRequest
		mainDir, err := state.RepositoryForWorktree(".")
		if err != nil {
			return err
		}
		if len(args) > 0 {
			if found, ok, err := state.GetChangeRequest(mainDir, args[0]); err != nil {
				return err
			} else if ok {
				change = found
				if found.Worktree != nil {
					worktreePath = found.Worktree.Path
				}
			} else {
				resolved, err := state.ValidateWorktreeExists(args[0])
				if err != nil {
					return err
				}
				worktreePath = resolved
			}
		} else {
			if !state.HasWorktree() {
				return fmt.Errorf("not in a maia worktree. Provide a slug: maia merge <slug>")
			}
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			worktreePath = cwd
			root, err := state.RepositoryForWorktree(cwd)
			if err != nil {
				return err
			}
			mainDir = root
			if found, ok, err := state.GetChangeRequest(root, filepath.Base(cwd)); err == nil && ok {
				change = found
			}
		}

		branch := ""
		if change != nil {
			branch = change.Branch
		}
		if branch == "" && worktreePath != "" {
			out, err := exec.Command("git", "-C", worktreePath, "rev-parse", "--abbrev-ref", "HEAD").Output()
			if err != nil {
				return fmt.Errorf("failed to get branch for worktree: %w", err)
			}
			branch = strings.TrimSpace(string(out))
		}

		// Get the current branch in main repo
		getMainBranch := exec.Command("git", "-C", mainDir, "rev-parse", "--abbrev-ref", "HEAD")
		mainOut, err := getMainBranch.Output()
		if err != nil {
			return fmt.Errorf("failed to get main branch: %w", err)
		}
		mainBranch := strings.TrimSpace(string(mainOut))
		sourceBranch := mergeBaseBranch
		if sourceBranch == "" && change != nil {
			sourceBranch = change.SourceBranch
		}
		if sourceBranch == "" && worktreePath != "" {
			if data, err := os.ReadFile(filepath.Join(worktreePath, state.StateDir, "change.md")); err == nil {
				for _, line := range strings.Split(string(data), "\n") {
					if strings.HasPrefix(line, "source_branch:") {
						sourceBranch = strings.TrimSpace(strings.TrimPrefix(line, "source_branch:"))
						break
					}
				}
			}
		}
		mergeDir := mainDir
		var checkout []string
		if sourceBranch != "" {
			mainBranch = strings.TrimPrefix(sourceBranch, "origin/")
			if wt := worktreeForBranch(mainDir, mainBranch); wt != "" && filepath.Clean(wt) != filepath.Clean(worktreePath) {
				mergeDir = wt
			} else if strings.HasPrefix(sourceBranch, "origin/") {
				localRef := exec.Command("git", "-C", mainDir, "show-ref", "--verify", "--quiet", "refs/heads/"+mainBranch)
				if localRef.Run() != nil {
					checkout = []string{"git", "-C", mainDir, "checkout", "--track", "-b", mainBranch, sourceBranch}
				} else {
					checkout = []string{"git", "-C", mainDir, "checkout", mainBranch}
				}
			} else {
				checkout = []string{"git", "-C", mainDir, "checkout", sourceBranch}
			}
		}

		description := "change"
		if change != nil && change.Description != "" {
			description = change.Description
		} else if worktreePath != "" {
			changeMDPath := filepath.Join(worktreePath, state.StateDir, "change.md")
			if changeMD, err := os.ReadFile(changeMDPath); err == nil {
				for _, line := range strings.Split(string(changeMD), "\n") {
					if strings.HasPrefix(line, "description:") {
						description = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
						break
					}
				}
			}
		}

		fmt.Printf("Merging branch: %s\n", branch)
		fmt.Printf("Into: %s (%s)\n", mainBranch, mergeDir)

		commands := [][]string{}
		if len(checkout) > 0 {
			commands = append(commands, checkout)
		}
		commands = append(commands, []string{"git", "-C", mergeDir, "merge", branch, "--no-ff", "-m", fmt.Sprintf("maia: %s", description)})

		for _, c := range commands {
			mergeCmd := exec.Command(c[0], c[1:]...)
			if out, err := mergeCmd.CombinedOutput(); err != nil {
				fmt.Println(string(out))
				return err
			}
		}

		fmt.Println("\n✓ Changes merged to", mainBranch)
		if worktreePath != "" {
			fmt.Printf("  Run 'maia cleanup %s' to remove the worktree\n", filepath.Base(worktreePath))
		}

		return nil
	},
}

func init() {
	mergeCmd.Flags().StringVar(&mergeBaseBranch, "base", "", "Branch to merge the change request into")
	rootCmd.AddCommand(mergeCmd)
}

func worktreeForBranch(repo, branch string) string {
	out, err := exec.Command("git", "-C", repo, "worktree", "list", "--porcelain").Output()
	if err != nil {
		return ""
	}
	path := ""
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "worktree ") {
			path = strings.TrimPrefix(line, "worktree ")
		} else if strings.HasPrefix(line, "branch refs/heads/") && strings.TrimPrefix(line, "branch refs/heads/") == branch {
			return path
		}
	}
	return ""
}
