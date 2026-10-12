package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sceptyre/maia/internal/state"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var editCmd = &cobra.Command{
	Use:   "edit [slug]",
	Short: "Edit the change.md for a worktree",
	Long: `Edit the .maia/change.md file for a given worktree.

Without a slug, edits the change.md in the current worktree.
With a slug, edits the change.md in the specified worktree (from the parent branch).

Interactive mode (TTY):
  Opens $EDITOR (falls back to $VISUAL, then vi).

Pipe mode (non-interactive):
  Reads content from stdin and writes it to change.md.
  If stdin content has no frontmatter, existing frontmatter is preserved.

Examples:
  maia edit my-slug              # Open change.md in $EDITOR
  maia edit                      # Edit current worktree's change.md
  echo "New description" | maia edit my-slug  # Pipe content in`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			repoRoot, err := state.RepositoryForWorktree(".")
			if err == nil {
				if _, err := state.SyncChangeRequests(repoRoot); err != nil {
					return err
				}
				if cr, found, err := state.GetChangeRequest(repoRoot, args[0]); err != nil {
					return err
				} else if found {
					if cr.Worktree == nil {
						return editDetachedChange(repoRoot, cr)
					}
					return editMaterializedChange(repoRoot, cr.Worktree.Path)
				}
			}
			resolved, err := state.ValidateWorktreeExists(args[0])
			if err != nil {
				return err
			}
			return editMaterializedChange("", resolved)
		}
		if !state.HasWorktree() {
			return fmt.Errorf("not in a maia worktree. Provide a slug: maia edit <slug>")
		}
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		repoRoot, err := state.RepositoryForWorktree(cwd)
		if err != nil {
			return err
		}
		return editMaterializedChange(repoRoot, cwd)
	},
}

func editMaterializedChange(repoRoot, worktreePath string) error {
	changePath := filepath.Join(worktreePath, state.StateDir, "change.md")
	if _, err := os.Stat(changePath); err != nil {
		return fmt.Errorf("change.md not found at %s", changePath)
	}
	var err error
	if term.IsTerminal(int(os.Stdin.Fd())) {
		err = editWithEditor(changePath)
	} else {
		err = editWithStdin(changePath)
	}
	if err != nil {
		return err
	}
	if repoRoot != "" {
		_, err = state.SyncChangeRequests(repoRoot)
	}
	return err
}

func editDetachedChange(repoRoot string, cr *state.ChangeRequest) error {
	file, err := os.CreateTemp("", "maia-change-*.md")
	if err != nil {
		return err
	}
	path := file.Name()
	defer os.Remove(path)
	if _, err := file.WriteString(cr.Change); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if term.IsTerminal(int(os.Stdin.Fd())) {
		err = editWithEditor(path)
	} else {
		err = editWithStdin(path)
	}
	if err != nil {
		return err
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return state.UpdateChangeDocument(repoRoot, cr.Slug, string(contents))
}

// editWithEditor opens the user's $EDITOR with the file.
func editWithEditor(path string) error {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "vi"
	}

	// Parse editor command (handles "code --wait" etc.)
	fields := strings.Fields(editor)
	editorCmd := exec.Command(fields[0], fields[1:]...)
	editorCmd.Args = append(editorCmd.Args, path)
	editorCmd.Stdin = os.Stdin
	editorCmd.Stdout = os.Stdout
	editorCmd.Stderr = os.Stderr

	if err := editorCmd.Run(); err != nil {
		return fmt.Errorf("editor failed: %w", err)
	}

	fmt.Println("✓ change.md updated")
	return nil
}

// editWithStdin reads from stdin and writes to the file.
func editWithStdin(path string) error {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("failed to read stdin: %w", err)
	}

	if len(data) == 0 {
		return fmt.Errorf("no input received from stdin")
	}

	content := string(data)

	// If the input doesn't contain frontmatter, preserve existing frontmatter
	if !strings.HasPrefix(content, "---") {
		existing, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read existing change.md: %w", err)
		}
		frontmatter, _ := splitFrontmatter(string(existing))
		if frontmatter != "" {
			content = frontmatter + "\n\n" + content
		}
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write change.md: %w", err)
	}

	fmt.Println("✓ change.md updated from stdin")
	return nil
}

// splitFrontmatter separates the YAML frontmatter block from the body.
func splitFrontmatter(content string) (string, string) {
	if !strings.HasPrefix(content, "---") {
		return "", content
	}
	lines := strings.Split(content, "\n")
	end := -1
	for i, line := range lines {
		if i > 0 && strings.HasPrefix(line, "---") {
			end = i
			break
		}
	}
	if end == -1 {
		return "", content
	}
	frontmatter := strings.Join(lines[:end+1], "\n")
	body := strings.TrimLeft(strings.Join(lines[end+1:], "\n"), "\n")
	return frontmatter, body
}

func init() {
	rootCmd.AddCommand(editCmd)
}
