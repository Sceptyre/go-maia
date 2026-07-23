package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sceptyre/maia/internal/render"
	"github.com/sceptyre/maia/internal/state"
	"github.com/spf13/cobra"
)

var showCmd = &cobra.Command{
	Use:   "show <plan|research|change> [slug]",
	Short: "Display plan, research, or change document",
	Long: `Display the contents of a document in the console.

Shows the full content of plan.md, research.md, or change.md including frontmatter.

With a slug, reads from the specified worktree (works from the parent branch).
Without a slug, reads from the current worktree directory.

Examples:
  maia show plan              # Display the plan in current worktree
  maia show research my-slug  # Display research for 'my-slug' worktree
  maia show change my-slug    # Display change.md for 'my-slug' worktree
  maia show change            # Display change.md in current worktree`,
	Args: cobra.RangeArgs(1, 2),
	ValidArgs: []string{"plan", "research", "change"},
	RunE: func(cmd *cobra.Command, args []string) error {
		docType := args[0]
		var slug string
		if len(args) > 1 {
			slug = args[1]
		}

		// Resolve the base directory
		var baseDir string

		if slug != "" {
			// Slug provided — resolve from parent repo
			resolved, err := state.ValidateWorktreeExists(slug)
			if err != nil {
				return err
			}
			baseDir = resolved
		} else {
			// No slug — check if we're in a maia worktree
			if !state.HasWorktree() {
				return fmt.Errorf("not in a maia worktree. Provide a slug: maia show <type> <slug>")
			}
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			baseDir = cwd
		}

		var filePath string
		var exists bool

		switch docType {
		case "plan":
			filePath = filepath.Join(baseDir, state.GetGeneratedDir(), "plan.md")
			exists = state.HasPlan() || slug != "" && fileExists(filePath)
		case "research":
			filePath = filepath.Join(baseDir, state.GetGeneratedDir(), "research.md")
			exists = state.HasResearch() || slug != "" && fileExists(filePath)
		case "change":
			filePath = filepath.Join(baseDir, state.StateDir, "change.md")
			exists = fileExists(filePath)
		default:
			return fmt.Errorf("unknown document type: %s (use 'plan', 'research', or 'change')", docType)
		}

		if !exists {
			return fmt.Errorf("%s not found in worktree", docType)
		}

		content, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", docType, err)
		}

		fmt.Print(render.RenderMarkdown(string(content)))

		return nil
	},
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func init() {
	rootCmd.AddCommand(showCmd)
}
