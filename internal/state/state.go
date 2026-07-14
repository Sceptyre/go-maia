package state

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const StateDir = ".maia"
const GeneratedDir = ".generated"

// GetGeneratedDir returns .maia/.generated/
func GetGeneratedDir() string {
	return filepath.Join(StateDir, GeneratedDir)
}

// GetWorktreesDir returns ~/.maia/worktrees/
func GetWorktreesDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".maia", "worktrees")
}

// GetRepoWorktreeDir returns ~/.maia/worktrees/<repo>/
func GetRepoWorktreeDir() (string, error) {
	repoName, err := GetRepoName()
	if err != nil {
		return "", err
	}
	return filepath.Join(GetWorktreesDir(), repoName), nil
}

// GetRepoName returns the name of the current repository's root directory
func GetRepoName() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not in a git repository")
	}
	rootPath := strings.TrimSpace(string(out))
	return filepath.Base(rootPath), nil
}

// EnsureDirs creates the .maia/.generated/ directory structure
func EnsureDirs() error {
	return os.MkdirAll(GetGeneratedDir(), 0755)
}

// EnsureGlobalDir creates ~/.maia/worktrees/
func EnsureGlobalDir() error {
	return os.MkdirAll(GetWorktreesDir(), 0755)
}

// GetStatus reads the status from plan.md or change.md frontmatter
func GetStatus() (string, error) {
	// Check plan.md first
	if data, err := os.ReadFile(filepath.Join(GetGeneratedDir(), "plan.md")); err == nil {
		if status := extractFrontmatterField(string(data), "status"); status != "" {
			return status, nil
		}
	}

	// Fall back to change.md
	if data, err := os.ReadFile(filepath.Join(StateDir, "change.md")); err == nil {
		if status := extractFrontmatterField(string(data), "status"); status != "" {
			return status, nil
		}
	}

	return "new", nil
}

// SetStatus updates the status in plan.md (or change.md if plan doesn't exist)
func SetStatus(status string) error {
	// Try to update plan.md first
	planPath := filepath.Join(GetGeneratedDir(), "plan.md")
	if _, err := os.Stat(planPath); err == nil {
		return updateFrontmatterField(planPath, "status", status)
	}

	// Fall back to change.md
	changePath := filepath.Join(StateDir, "change.md")
	if _, err := os.Stat(changePath); err == nil {
		return updateFrontmatterField(changePath, "status", status)
	}

	return fmt.Errorf("no plan.md or change.md found")
}

// HasWorktree checks if .maia directory exists (we're in a maia worktree)
func HasWorktree() bool {
	_, err := os.Stat(StateDir)
	return err == nil
}

// HasChangeFile checks if change.md exists
func HasChangeFile() bool {
	_, err := os.Stat(filepath.Join(StateDir, "change.md"))
	return err == nil
}

// HasResearch checks if research.md exists
func HasResearch() bool {
	_, err := os.Stat(filepath.Join(GetGeneratedDir(), "research.md"))
	return err == nil
}

// HasPlan checks if plan.md exists
func HasPlan() bool {
	_, err := os.Stat(filepath.Join(GetGeneratedDir(), "plan.md"))
	return err == nil
}

func extractFrontmatterField(content, field string) string {
	// Simple frontmatter parser
	if !strings.HasPrefix(content, "---") {
		return ""
	}

	lines := strings.Split(content, "\n")
	inFrontmatter := false

	for _, line := range lines {
		if strings.HasPrefix(line, "---") {
			if inFrontmatter {
				break
			}
			inFrontmatter = true
			continue
		}

		if inFrontmatter {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 && strings.TrimSpace(parts[0]) == field {
				return strings.TrimSpace(parts[1])
			}
		}
	}

	return ""
}

func updateFrontmatterField(path, field, value string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	content := string(data)
	lines := strings.Split(content, "\n")
	newLines := make([]string, 0, len(lines))
	inFrontmatter := false
	fieldFound := false

	for _, line := range lines {
		if strings.HasPrefix(line, "---") {
			if inFrontmatter {
				newLines = append(newLines, line)
				inFrontmatter = false
				continue
			}
			inFrontmatter = true
			newLines = append(newLines, line)
			continue
		}

		if inFrontmatter {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 && strings.TrimSpace(parts[0]) == field {
				newLines = append(newLines, fmt.Sprintf("%s: %s", strings.TrimSpace(parts[0]), value))
				fieldFound = true
				continue
			}
		}

		newLines = append(newLines, line)
	}

	// If field wasn't found, add it after first ---
	if !fieldFound && inFrontmatter {
		// This shouldn't happen with proper frontmatter, but handle it
		for i, line := range newLines {
			if strings.HasPrefix(line, "---") && i > 0 {
				newLines = append(newLines[:i+1], append([]string{fmt.Sprintf("%s: %s", field, value)}, newLines[i+1:]...)...)
				break
			}
		}
	}

	return os.WriteFile(path, []byte(strings.Join(newLines, "\n")), 0644)
}

// ListWorktrees returns all maia worktrees for the current repo
func ListWorktrees() ([]string, error) {
	repoDir, err := GetRepoWorktreeDir()
	if err != nil {
		return nil, err
	}
	return listWorktreesInDir(repoDir)
}

func listWorktreesInDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}

	var worktrees []string
	for _, entry := range entries {
		if entry.IsDir() {
			worktrees = append(worktrees, entry.Name())
		}
	}
	return worktrees, nil
}

