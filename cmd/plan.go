package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sceptyre/maia/internal/llm"
	"github.com/sceptyre/maia/internal/state"
	"github.com/spf13/cobra"
)

var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Generate implementation plan",
	Long: `Uses change.md (goals) and research.md (codebase analysis) to generate
a detailed implementation plan with phases, artifacts, and code samples.

Output is saved to .maia/.generated/plan.md.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Verify we're in a maia worktree
		if !state.HasWorktree() {
			return fmt.Errorf("not in a maia worktree. Run 'maia new' first, then cd to the worktree")
		}

		// Check for required files
		if !state.HasChangeFile() {
			return fmt.Errorf("change.md not found. Run 'maia new' first")
		}
		if !state.HasResearch() {
			return fmt.Errorf("research.md not found. Run 'maia init' first")
		}

		// Read change.md
		changeMD, err := os.ReadFile(filepath.Join(state.StateDir, "change.md"))
		if err != nil {
			return fmt.Errorf("failed to read change.md: %w", err)
		}

		// Read research.md
		researchMD, err := os.ReadFile(filepath.Join(state.GetGeneratedDir(), "research.md"))
		if err != nil {
			return fmt.Errorf("failed to read research.md: %w", err)
		}

		// Clean the markdown
		changeContent := cleanMarkdown(string(changeMD))

		fmt.Println("═══════════════════════════════════════════════════════════════")
		fmt.Println("  MAIA PLAN - Implementation Plan Generation")
		fmt.Println("═══════════════════════════════════════════════════════════════")
		fmt.Println(strings.Repeat("─", 60))

		// Initialize LLM client
		client := llm.NewClient()
		if client.APIKey == "" {
			return fmt.Errorf("API key not configured. Set in ~/.maia/config.json or export OPENAI_API_KEY")
		}

		cwd, _ := os.Getwd()

		// Build the system prompt
		systemPrompt := "You create implementation plans from research.\n\n" +
			"Plan format:\n" +
			"- Phases with names and descriptions\n" +
			"- Artifact table per phase (file, action, description)\n" +
			"- Code samples for each artifact\n" +
			"- Risk assessment\n\n" +
			"Be specific. Include actual code."

		userPrompt := fmt.Sprintf(`## Original Request

%s

## Research

%s

---

Based on this research, create a detailed implementation plan.

The plan should include:
1. Multiple phases (logical groupings of work)
2. For each phase:
   - Clear description of what this phase accomplishes
   - Table of artifacts (files to create/modify) with action and description
   - Code samples showing specific changes (use before/after where helpful)
3. Dependencies between phases
4. Risk assessment

Produce a comprehensive implementation plan.`, changeContent, string(researchMD))

		fmt.Println("\n▶ Generating implementation plan...")
		fmt.Printf("  Model: %s\n\n", client.Model)

		// Create tool handler
		toolHandler := func(call llm.ToolCall) (string, error) {
			fmt.Printf("  🔧 %s", call.Function.Name)
			var args map[string]string
			json.Unmarshal([]byte(call.Function.Arguments), &args)
			if path, ok := args["path"]; ok && path != "" {
				fmt.Printf(" %s", path)
			}
			fmt.Println()
			return llm.HandleToolCall(call, cwd)
		}

		// Run the AI planning
		messages := []llm.Message{
			llm.NewMessage("system", systemPrompt),
			llm.NewMessage("user", userPrompt),
		}

		planning, _, err := client.GetResponseWithTools(messages, llm.FileTools, toolHandler)
		if err != nil {
			return fmt.Errorf("AI planning failed: %w", err)
		}

		// Follow-up: reformat into specific structure
		fmt.Println("\n▶ Formatting implementation plan...")

		bq := "`" // backtick
		reformatPrompt := "Now reformat your plan into this exact markdown structure. " +
			"Include all details but organize them into these sections:\n\n" +
			bq + "```\n" +
			"---\n" +
			"name: <id from change.md>\n" +
			"description: <description>\n" +
			"status: planned\n" +
			"risk: <low|medium|high>\n" +
			"phases: <number>\n" +
			"---\n\n" +
			"# Plan: <description>\n\n" +
			"<summary of what this plan accomplishes>\n\n" +
			"## Phase 1: <name>\n\n" +
			"<description>\n\n" +
			"| Artifact | Action | Description |\n" +
			"|----------|--------|-------------|\n" +
			"| " + bq + "path/to/file.go" + bq + " | create | <what this file does> |\n" +
			"| " + bq + "path/to/other.go" + bq + " | modify | <what changes> |\n\n" +
			"### " + bq + "path/to/file.go" + bq + "\n\n" +
			"**<what this change does>**\n\n" +
			bq + "```\n" +
			"go\n" +
			"<code sample>\n" +
			bq + "```\n\n" +
			"<repeat for each phase>\n" +
			bq + "```\n\n" +
			"Reformat your complete plan now:"

		messages = append(messages,
			llm.NewMessage("assistant", planning),
			llm.NewMessage("user", reformatPrompt),
		)

		response, err := client.GetResponse(messages)
		if err != nil {
			return fmt.Errorf("AI reformat failed: %w", err)
		}

		fmt.Println("\n▶ Writing plan document...")

		// Extract just the markdown
		planMD := extractMarkdown(response)

		// Write plan.md
		if err := os.WriteFile(filepath.Join(state.GetGeneratedDir(), "plan.md"), []byte(planMD), 0644); err != nil {
			return fmt.Errorf("failed to write plan: %w", err)
		}

		// Update status
		if err := state.SetStatus("planned"); err != nil {
			return err
		}

		fmt.Println(strings.Repeat("─", 60))
		fmt.Println("\n✓ Plan generated!")
		fmt.Printf("\nOutput: .maia/.generated/plan.md\n")
		fmt.Println("\nReview the plan, then run: maia apply")

		return nil
	},
}

func init() {
	rootCmd.AddCommand(planCmd)
}
