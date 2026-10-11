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

var continueBaseUpdate bool
var abortBaseUpdate bool

var updateBaseCmd = &cobra.Command{
	Use:   "update-base <slug>",
	Short: "Rebase a change request onto its latest source branch",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := state.RepositoryForWorktree(".")
		if err != nil {
			return err
		}
		if _, err := state.SyncChangeRequests(repoRoot); err != nil {
			return err
		}
		cr, found, err := state.GetChangeRequest(repoRoot, args[0])
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("change request not found: %s", args[0])
		}
		if cr.Worktree == nil {
			return fmt.Errorf("change request %s has no worktree; run maia materialize %s first", cr.Slug, cr.Slug)
		}
		if continueBaseUpdate && abortBaseUpdate {
			return fmt.Errorf("choose either --continue or --abort")
		}
		if continueBaseUpdate {
			if err := runGitUpdate(cr.Worktree.Path, []string{"rebase", "--continue"}, true); err != nil {
				return err
			}
			return state.UpdateChangeRebaseConflict(repoRoot, cr.Slug, false)
		}
		if abortBaseUpdate {
			if err := runGitUpdate(cr.Worktree.Path, []string{"rebase", "--abort"}, false); err != nil {
				return err
			}
			return state.UpdateChangeRebaseConflict(repoRoot, cr.Slug, false)
		}

		base := cr.SourceBranch
		if base == "" {
			if out, err := exec.Command("git", "-C", repoRoot, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD").Output(); err == nil {
				base = strings.TrimSpace(string(out))
			}
		}
		if base == "" {
			return fmt.Errorf("change request %s has no source branch", cr.Slug)
		}
		if _, err := exec.Command("git", "-C", repoRoot, "check-ref-format", "--branch", base).Output(); err != nil {
			return fmt.Errorf("invalid source branch %q", base)
		}

		baseRef := base
		remote, remoteBranch := "", ""
		if strings.HasPrefix(base, "origin/") {
			remote, remoteBranch = "origin", strings.TrimPrefix(base, "origin/")
		} else if out, err := exec.Command("git", "-C", repoRoot, "rev-parse", "--abbrev-ref", base+"@{upstream}").Output(); err == nil {
			upstream := strings.TrimSpace(string(out))
			parts := strings.SplitN(upstream, "/", 2)
			if len(parts) == 2 {
				remote, remoteBranch = parts[0], parts[1]
			}
		} else if _, err := exec.Command("git", "-C", repoRoot, "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+base).Output(); err == nil {
			remote, remoteBranch = "origin", base
		}
		if remote != "" {
			if err := runGitUpdate(repoRoot, []string{"fetch", remote, remoteBranch}, false); err != nil {
				return fmt.Errorf("fetch source branch: %w", err)
			}
			baseRef = remote + "/" + remoteBranch
		}

		fmt.Printf("Rebasing %s onto %s…\n", cr.Branch, baseRef)
		if err := runGitUpdate(cr.Worktree.Path, []string{"rebase", baseRef}, false); err != nil {
			if rebaseInProgress(cr.Worktree.Path) {
				_ = state.UpdateChangeRebaseConflict(repoRoot, cr.Slug, true)
			}
			return fmt.Errorf("update-base failed: %w; resolve conflicts in %s, then run `maia update-base %s --continue` or `--abort`", err, cr.Worktree.Path, cr.Slug)
		}
		if err := state.UpdateChangeRebaseConflict(repoRoot, cr.Slug, false); err != nil {
			return err
		}
		fmt.Printf("✓ %s is based on the latest %s\n", cr.Slug, cr.SourceBranch)
		return nil
	},
}

func runGitUpdate(dir string, args []string, acceptExistingMessage bool) error {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if acceptExistingMessage {
		cmd.Env = append(os.Environ(), "GIT_EDITOR=true")
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		if text := strings.TrimSpace(string(out)); text != "" {
			return fmt.Errorf("%s: %w", text, err)
		}
		return err
	}
	return nil
}

func rebaseInProgress(worktreePath string) bool {
	for _, gitPath := range []string{"rebase-merge", "rebase-apply"} {
		out, err := exec.Command("git", "-C", worktreePath, "rev-parse", "--git-path", gitPath).Output()
		if err != nil {
			continue
		}
		path := strings.TrimSpace(string(out))
		if !filepath.IsAbs(path) {
			path = filepath.Join(worktreePath, path)
		}
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

func init() {
	updateBaseCmd.Flags().BoolVar(&continueBaseUpdate, "continue", false, "Continue a rebase after resolving conflicts")
	updateBaseCmd.Flags().BoolVar(&abortBaseUpdate, "abort", false, "Abort an in-progress rebase")
	rootCmd.AddCommand(updateBaseCmd)
}
