package git

import (
	"fmt"
	"os/exec"
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

// Create adds a new worktree with a new branch
func Create(path, branch, baseBranch string) error {
	cmd := exec.Command("git", "worktree", "add", "-b", branch, path, baseBranch)
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
