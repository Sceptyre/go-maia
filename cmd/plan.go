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
		systemPrompt := "You are a planner, not a writer. You produce implementation plans —\n" +
			"documents that describe WHAT to change and HOW, not the files themselves.\n\n" +
			"Your output is a plan with phases. Each phase has:\n" +
			"1. A name and description of what it accomplishes\n" +
			"2. An artifact table listing files to create or modify\n" +
			"3. Code samples showing the changes for each file\n\n" +
			"Output format:\n\n" +
			"## Phase 1: <name>\n" +
			"<what this phase accomplishes>\n\n" +
			"| Artifact | Action | Description |\n" +
			"|----------|--------|-------------|\n" +
			"| path/to/file.go | create | <what this file does> |\n" +
			"| path/to/other.go | modify | <what changes> |\n\n" +
			"### path/to/file.go (create)\n" +
			"<code sample — the full file content>\n\n" +
			"### path/to/other.go (modify)\n" +
			"<code sample — only the new or changed code>\n\n" +
			"This is a PLAN. Do not write the actual files. Do not output the finished\n" +
			"README or finished code as a document. Describe each change as a task with\n" +
			"artifact table and code sample."

		userPrompt := fmt.Sprintf(`## Change Request

%s

## Research Findings

%s

---

Write the implementation plan for this change.

The plan is a document with phases, artifact tables, and code samples. It is NOT the finished file. It describes what files to create or modify, what changes to make in each, and shows the code to write.

Do not write the README. Write a plan for updating the README.`, changeContent, string(researchMD))

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
			"| " + bq + "path/to/new.go" + bq + " | create | <what this file does> |\n" +
			"| " + bq + "path/to/existing.go" + bq + " | modify | <what changes and WHERE> |\n\n" +
			"### " + bq + "path/to/new.go" + bq + " (create)\n\n" +
			"**<what this file does>**\n\n" +
			bq + "```\n" +
			"go\n" +
			"<full file content>\n" +
			bq + "```\n\n" +
			"### " + bq + "path/to/existing.go" + bq + " (modify: add/replace)\n\n" +
			"**<what this change does>**\n\n" +
			"Action type: add after `function X` / replace the existing `function Y`\n\n" +
			"Placement: <where in the file — be specific, e.g. 'add after the Init() function'>\n\n" +
			bq + "```\n" +
			"go\n" +
			"<code to insert or replacement code>\n" +
			bq + "```\n\n" +
			"<repeat for each phase>\n" +
			bq + "```\n\n" +
			"IMPORTANT:\n" +
			"- Keep phases small and focused. Each phase should be completable in one pass.\n" +
			"- If a phase has more than 3 artifacts, split it into multiple phases.\n" +
			"- For modify 'add': include only the NEW code to insert.\n" +
			"- For modify 'replace': include the old code to find AND the new code.\n" +
			"- Always specify exactly WHERE in the file the change goes.\n\n" +
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

		// Write plan.md
		if err := os.WriteFile(filepath.Join(state.GetGeneratedDir(), "plan.md"), []byte(response), 0644); err != nil {
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
