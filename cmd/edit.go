package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/term"
	"github.com/sceptyre/maia/internal/state"
	"github.com/spf13/cobra"
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
		var changePath string

		if len(args) > 0 {
			// Slug provided — resolve from parent repo
			resolved, err := state.ValidateWorktreeExists(args[0])
			if err != nil {
				return err
			}
			changePath = filepath.Join(resolved, state.StateDir, "change.md")
		} else {
			// No slug — assume CWD is a worktree
			if !state.HasWorktree() {
				return fmt.Errorf("not in a maia worktree. Provide a slug: maia edit <slug>")
			}
			changePath = filepath.Join(state.StateDir, "change.md")
		}

		// Ensure the file exists
		if _, err := os.Stat(changePath); os.IsNotExist(err) {
			return fmt.Errorf("change.md not found at %s", changePath)
		}

		// Detect if stdin is a terminal
		if term.IsTerminal(int(os.Stdin.Fd())) {
			return editWithEditor(changePath)
		}
		return editWithStdin(changePath)
	},
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
