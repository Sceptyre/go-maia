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
	Use:   "init",
	Short: "Research and analyze codebase",
	Long: `Scans the codebase and web to discover:
  - Project structure and patterns
  - Relevant existing code for the change
  - External documentation and best practices
  - Dependencies and relationships
  - Existing conventions to follow

Output is saved to .maia/.generated/research.md for use by 'maia plan'.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Verify we're in a maia worktree
		if !state.HasWorktree() {
			return fmt.Errorf("not in a maia worktree. Run 'maia new' first, then cd to the worktree")
		}

		// Check for change.md
		if !state.HasChangeFile() {
			return fmt.Errorf("change.md not found. Run 'maia new' first")
		}

		// Read change.md
		changeMD, err := os.ReadFile(filepath.Join(state.StateDir, "change.md"))
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

		cwd, _ := os.Getwd()

		fmt.Printf("\n▶ Orchestrating research...")
		fmt.Printf("\n  Model: %s", client.Model)
		fmt.Printf("\n  Working directory: %s\n", cwd)

		// Run orchestrator with subagents
		fmt.Println("\n▶ Starting research agents...\n")

		research, err := llm.RunOrchestratorWithReformat(changeContent, cwd)
		if err != nil {
			return fmt.Errorf("AI research failed: %w", err)
		}

		// Final reformat if needed
		if !strings.Contains(research, "## ") {
			fmt.Println("\n▶ Formatting research document...")
			client := llm.NewClient()
			research, _ = client.GetResponse([]llm.Message{
				llm.NewMessage("user", "Format this research into structured markdown with sections: Relevant Files, Code Patterns, External Research, Key Conventions, Risks. Include frontmatter."),
				llm.NewMessage("assistant", research),
			})
		}

		fmt.Println("\n▶ Writing research output...")

		// Extract just the markdown
		researchMD := extractMarkdown(research)

		// Write research.md
		if err := os.WriteFile(filepath.Join(state.GetGeneratedDir(), "research.md"), []byte(researchMD), 0644); err != nil {
			return fmt.Errorf("failed to write research: %w", err)
		}

		// Update status
		if err := state.SetStatus("analyzed"); err != nil {
			return err
		}

		fmt.Println(strings.Repeat("─", 60))
		fmt.Println("\n✓ Research complete!")
		fmt.Printf("\nOutput: .maia/.generated/research.md\n")
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
