package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Worktree struct {
	Path   string
	Branch string
	Head   string
}

// List returns all worktrees in the current repo
func List() ([]Worktree, error) {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list worktrees: %w", err)
	}

	return parseWorktreeList(string(out)), nil
}

// Create adds a new worktree with a new branch.
// If baseBranch is empty, uses current branch (git default).
func Create(path, branch, baseBranch string) error {
	var cmd *exec.Cmd
	if baseBranch != "" {
		cmd = exec.Command("git", "worktree", "add", "-b", branch, path, baseBranch)
	} else {
		cmd = exec.Command("git", "worktree", "add", "-b", branch, path)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create worktree: %s\n%w", string(out), err)
	}
	return nil
}

// Remove deletes a worktree
func Remove(path string) error {
	cmd := exec.Command("git", "worktree", "remove", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to remove worktree: %s\n%w", string(out), err)
	}
	return nil
}

// GetMainDir returns the main worktree path (usually the bare/original repo)
func GetMainDir() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// EnsureLocalExclude appends the given patterns to the repository's local
// exclude file ($GIT_COMMON_DIR/info/exclude), which git shares across every
// worktree of the repo and never commits or pushes. Idempotent.
func EnsureLocalExclude(worktreeDir string, patterns ...string) error {
	out, err := exec.Command("git", "-C", worktreeDir, "rev-parse", "--git-common-dir").Output()
	if err != nil {
		return fmt.Errorf("resolve git common dir: %w", err)
	}
	common := strings.TrimSpace(string(out))
	if !filepath.IsAbs(common) {
		common = filepath.Join(worktreeDir, common)
	}
	path := filepath.Join(common, "info", "exclude")
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	have := map[string]bool{}
	for _, line := range strings.Split(string(existing), "\n") {
		have[strings.TrimSpace(line)] = true
	}
	var add []string
	for _, pat := range patterns {
		if pat != "" && !have[pat] {
			add = append(add, pat)
		}
	}
	if len(add) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	prefix := ""
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		prefix = "\n"
	}
	_, err = f.WriteString(prefix + strings.Join(add, "\n") + "\n")
	return err
}

func parseWorktreeList(output string) []Worktree {
	var worktrees []Worktree
	var current Worktree

	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			if current.Path != "" {
				worktrees = append(worktrees, current)
			}
			current = Worktree{Path: strings.TrimPrefix(line, "worktree ")}
		} else if strings.HasPrefix(line, "HEAD ") {
			current.Head = strings.TrimPrefix(line, "HEAD ")
		} else if strings.HasPrefix(line, "branch ") {
			current.Branch = strings.TrimPrefix(line, "branch refs/heads/")
		}
	}
	if current.Path != "" {
		worktrees = append(worktrees, current)
	}

	return worktrees
}
