package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sceptyre/maia/internal/llm"
	"github.com/sceptyre/maia/internal/state"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init [slug]",
	Short: "Research and analyze codebase",
	Long: `Scans the codebase and web to discover:
  - Project structure and patterns
  - Relevant existing code for the change
  - External documentation and best practices
  - Dependencies and relationships
  - Existing conventions to follow

Output is saved to .maia/.generated/research.md for use by 'maia plan'.

Without a slug, runs in the current worktree.
With a slug, runs from the parent branch against the specified worktree.

Examples:
  maia init              # Research current worktree
  maia init my-slug      # Research 'my-slug' from parent branch`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var worktreePath string

		if len(args) > 0 {
			// Slug provided — resolve from parent repo
			resolved, err := state.ValidateWorktreeExists(args[0])
			if err != nil {
				return err
			}
			worktreePath = resolved
		} else {
			// No slug — assume CWD is a worktree
			if !state.HasWorktree() {
				return fmt.Errorf("not in a maia worktree. Provide a slug: maia init <slug>")
			}
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			worktreePath = cwd
		}

		// Verify worktree has required files
		if !state.HasWorktreeIn(worktreePath) {
			return fmt.Errorf("not a maia worktree: %s", worktreePath)
		}
		if !state.HasChangeFileIn(worktreePath) {
			return fmt.Errorf("change.md not found in worktree. Run 'maia new' first")
		}

		// Read change.md using absolute path
		changeMD, err := os.ReadFile(filepath.Join(worktreePath, state.StateDir, "change.md"))
		if err != nil {
			return fmt.Errorf("failed to read change.md: %w", err)
		}

		// Clean the markdown
		changeContent := cleanMarkdown(string(changeMD))

		fmt.Println("═══════════════════════════════════════════════════════════════")
		fmt.Println("  MAIA INIT - Codebase & Web Research")
		fmt.Println("═══════════════════════════════════════════════════════════════")
		fmt.Println(strings.Repeat("─", 60))

		// Initialize LLM client
		client := llm.NewClient()
		if client.APIKey == "" {
			return fmt.Errorf("API key not configured. Set in ~/.maia/config.json or export OPENAI_API_KEY")
		}

		fmt.Printf("\n▶ Orchestrating research...")
		fmt.Printf("\n  Model: %s", client.Model)
		fmt.Printf("\n  Working directory: %s\n", worktreePath)

		// Run orchestrator with subagents — worktreePath is the working directory
		fmt.Println("\n▶ Starting research agents...\n")

		research, err := llm.RunOrchestratorWithReformat(changeContent, worktreePath)
		if err != nil {
			return fmt.Errorf("AI research failed: %w", err)
		}

		// Final reformat if needed
		if !strings.Contains(research, "## ") {
			fmt.Println("\n▶ Formatting research document...")
			client := llm.NewClient()
			research, _ = client.GetResponse([]llm.Message{
				llm.NewMessage("user", "Format this research into structured markdown with sections: Relevant Files, Code Patterns, External Research, Key Observations, Risks. Include frontmatter."),
				llm.NewMessage("assistant", research),
			})
		}

		fmt.Println("\n▶ Writing research output...")

		// Extract just the markdown
		researchMD := extractMarkdown(research)

		// Write research.md using absolute path
		if err := os.WriteFile(filepath.Join(worktreePath, state.GetGeneratedDir(), "research.md"), []byte(researchMD), 0644); err != nil {
			return fmt.Errorf("failed to write research: %w", err)
		}

		// Update status using path-aware helper
		if err := state.SetStatusIn(worktreePath, "analyzed"); err != nil {
			return err
		}

		fmt.Println(strings.Repeat("─", 60))
		fmt.Println("\n✓ Research complete!")
		fmt.Printf("\nOutput: %s\n", filepath.Join(worktreePath, state.GetGeneratedDir(), "research.md"))
		fmt.Println("\nNext step: maia plan")

		return nil
	},
}

// cleanMarkdown strips frontmatter and removes markdown comments
func cleanMarkdown(content string) string {
	// Strip frontmatter
	if strings.HasPrefix(content, "---") {
		lines := strings.Split(content, "\n")
		inFrontmatter := false
		var result []string

		for _, line := range lines {
			if strings.HasPrefix(line, "---") {
				if inFrontmatter {
					inFrontmatter = false
					continue
				}
				inFrontmatter = true
				continue
			}
			if !inFrontmatter {
				result = append(result, line)
			}
		}
		content = strings.Join(result, "\n")
	}

	// Remove markdown comments <!-- ... -->
	commentRegex := regexp.MustCompile(`<!--[\s\S]*?-->`)
	content = commentRegex.ReplaceAllString(content, "")

	return strings.TrimSpace(content)
}

func extractMarkdown(content string) string {
	// Try to extract markdown between ``` markers
	if strings.Contains(content, "```markdown") {
		start := strings.Index(content, "```markdown") + len("```markdown")
		end := strings.Index(content[start:], "```")
		if end != -1 {
			return strings.TrimSpace(content[start : start+end])
		}
	}

	// Try to extract content after first ---
	if strings.Contains(content, "---") {
		idx := strings.Index(content, "---")
		return strings.TrimSpace(content[idx:])
	}

	return content
}

func init() {
	rootCmd.AddCommand(initCmd)
}
