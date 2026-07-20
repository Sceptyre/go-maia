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
	Use:   "show <plan|research>",
	Short: "Display plan or research document",
	Long: `Display the contents of a generated document in the console.

Shows the full content of plan.md or research.md including frontmatter.

Examples:
  maia show plan       # Display the implementation plan
  maia show research   # Display the research document`,
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{"plan", "research"},
	RunE: func(cmd *cobra.Command, args []string) error {
		// Verify we're in a maia worktree
		if !state.HasWorktree() {
			return fmt.Errorf("not in a maia worktree. Run 'maia new' first, then cd to the worktree")
		}

		docType := args[0]

		var filename string
		var exists bool

		switch docType {
		case "plan":
			filename = "plan.md"
			exists = state.HasPlan()
		case "research":
			filename = "research.md"
			exists = state.HasResearch()
		default:
			return fmt.Errorf("unknown document type: %s (use 'plan' or 'research')", docType)
		}

		if !exists {
			return fmt.Errorf("%s not found. Run 'maia %s' first", filename, docType)
		}

		content, err := os.ReadFile(filepath.Join(state.GetGeneratedDir(), filename))
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", filename, err)
		}

		fmt.Print(render.RenderMarkdown(string(content)))

		return nil
	},
}

func init() {
	rootCmd.AddCommand(showCmd)
}
