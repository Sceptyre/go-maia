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

var materializeCmd = &cobra.Command{
	Use:   "materialize <slug>",
	Short: "Create or restore a change request worktree",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := state.RepositoryForWorktree(".")
		if err != nil {
			return err
		}
		if _, err := state.SyncChangeRequests(repoRoot); err != nil {
			return err
		}
		slug := args[0]
		cr, ok, err := state.GetChangeRequest(repoRoot, slug)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("change request not found: %s", slug)
		}
		if cr.Worktree != nil {
			if _, err := os.Stat(cr.Worktree.Path); err == nil {
				fmt.Printf("Change request %s already has a worktree\n", slug)
				return nil
			}
		}
		if cr.Branch == "" {
			cr.Branch = "maia/" + slug
		}
		if cr.SourceBranch == "" {
			return fmt.Errorf("change request %s has no source branch", slug)
		}

		path := filepath.Join(state.RepoWorktreeDirFor(repoRoot), slug)
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("worktree path already exists: %s", path)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		argsGit := []string{"worktree", "add"}
		if _, err := exec.Command("git", "-C", repoRoot, "show-ref", "--verify", "--quiet", "refs/heads/"+cr.Branch).Output(); err == nil {
			argsGit = append(argsGit, path, cr.Branch)
		} else {
			argsGit = append(argsGit, "-b", cr.Branch, path, cr.SourceBranch)
		}
		gitCmd := exec.Command("git", append([]string{"-C", repoRoot}, argsGit...)...)
		if out, err := gitCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("create worktree: %s", out)
		}

		maiaDir := filepath.Join(path, state.StateDir)
		if err := os.MkdirAll(filepath.Join(path, state.GetGeneratedDir()), 0755); err != nil {
			_ = git.Remove(path)
			return err
		}
		if err := os.WriteFile(filepath.Join(maiaDir, "change.md"), []byte(cr.Change), 0644); err != nil {
			_ = git.Remove(path)
			return err
		}
		for name, contents := range cr.Artifacts {
			if name != "research" && name != "plan" {
				continue
			}
			artifactPath := filepath.Join(path, state.GetGeneratedDir(), name+".md")
			if err := os.WriteFile(artifactPath, []byte(contents), 0644); err != nil {
				_ = git.Remove(path)
				return err
			}
		}
		if err := state.SetChangeWorktree(repoRoot, slug, &state.ChangeWorktree{Path: path, Branch: cr.Branch}); err != nil {
			_ = git.Remove(path)
			return err
		}
		fmt.Printf("Materialized %s at %s\n", slug, path)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(materializeCmd)
}
