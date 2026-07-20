package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sceptyre/maia/internal/llm"
	"github.com/sceptyre/maia/internal/render"
	"github.com/sceptyre/maia/internal/state"
	"github.com/spf13/cobra"
)

var (
	steerResearch bool
)

var steerCmd = &cobra.Command{
	Use:   "steer [feedback]",
	Short: "Revise the plan based on feedback",
	Long: `Refine or revise the implementation plan based on your feedback.

Provide specific feedback like:
- "Add a phase for database migrations before the handler changes"
- "Use bcrypt instead of argon2 for password hashing"
- "Make sure to handle the case where user already exists"
- "Follow the pattern in cmd/users.go for the new handler"

The plan will be updated with your guidance incorporated.

Use --research to revise the research document instead.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		feedback := args[0]

		// Verify we're in a maia worktree
		if !state.HasWorktree() {
			return fmt.Errorf("not in a maia worktree. Run 'maia new' first, then cd to the worktree")
		}

		// Check for change.md
		if !state.HasChangeFile() {
			return fmt.Errorf("change.md not found. Run 'maia new' first")
		}

		// Initialize LLM client
		client := llm.NewClient()
		if client.APIKey == "" {
			return fmt.Errorf("API key not configured. Set in ~/.maia/config.json or export OPENAI_API_KEY")
		}

		if steerResearch {
			return steerResearchDoc(client, feedback)
		}
		return steerPlan(client, feedback)
	},
}

func steerPlan(client *llm.Client, feedback string) error {
	// Check for plan
	if !state.HasPlan() {
		return fmt.Errorf("plan.md not found. Run 'maia plan' first")
	}

	// Read current plan
	planMD, err := os.ReadFile(filepath.Join(state.GetGeneratedDir(), "plan.md"))
	if err != nil {
		return fmt.Errorf("failed to read plan.md: %w", err)
	}

	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Println("  MAIA STEER - Revising Plan")
	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Println(strings.Repeat("─", 60))
	fmt.Printf("\nFeedback: %s\n\n", feedback)

	fmt.Println("▶ Revising plan with your feedback...")

	revisePrompt := fmt.Sprintf(`## Current Plan

%s

---

## Feedback

%s

---

Revise the plan incorporating this feedback.

Rules:
1. Keep what's good - only change what's needed based on feedback
2. Maintain the same markdown structure with frontmatter
3. Be specific about what changed and why
4. If feedback adds new requirements, add them to the appropriate phase
5. If feedback corrects something, fix it directly

Output the complete revised plan.`, string(planMD), feedback)

	messages := []llm.Message{
		llm.NewMessage("system", "You revise implementation plans based on user feedback. Keep the structure, incorporate the changes."),
		llm.NewMessage("user", revisePrompt),
	}

	response, err := client.GetResponse(messages)
	if err != nil {
		return fmt.Errorf("AI revision failed: %w", err)
	}

	fmt.Println("\n▶ Writing revised plan...")

	planMD = []byte(extractMarkdown(response))

	if err := os.WriteFile(filepath.Join(state.GetGeneratedDir(), "plan.md"), []byte(planMD), 0644); err != nil {
		return fmt.Errorf("failed to write plan: %w", err)
	}

	fmt.Println(strings.Repeat("─", 60))
	fmt.Println("\n✓ Plan revised!")
	fmt.Printf("\nFeedback: %s\n", render.Truncate(feedback, 80))
	fmt.Println("\nReview: cat .maia/.generated/plan.md")
	fmt.Println("Apply:  maia apply")
	fmt.Println("Steer:  maia steer 'more feedback'")

	return nil
}

func steerResearchDoc(client *llm.Client, feedback string) error {
	// Check for research
	if !state.HasResearch() {
		return fmt.Errorf("research.md not found. Run 'maia init' first")
	}

	// Read current research
	researchMD, err := os.ReadFile(filepath.Join(state.GetGeneratedDir(), "research.md"))
	if err != nil {
		return fmt.Errorf("failed to read research.md: %w", err)
	}

	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Println("  MAIA STEER - Revising Research")
	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Println(strings.Repeat("─", 60))
	fmt.Printf("\nFeedback: %s\n\n", feedback)

	fmt.Println("▶ Revising research with your feedback...")

	revisePrompt := fmt.Sprintf(`## Current Research

%s

---

## Feedback

%s

---

Revise the research incorporating this feedback.

Rules:
1. Keep what's useful - only change what's needed based on feedback
2. Maintain the same markdown structure with frontmatter
3. Add missing information requested in feedback
4. Remove irrelevant information if feedback indicates it's not needed
5. Be specific about what changed and why

Output the complete revised research document.`, string(researchMD), feedback)

	messages := []llm.Message{
		llm.NewMessage("system", "You revise research documents based on user feedback. Keep the structure, incorporate the changes."),
		llm.NewMessage("user", revisePrompt),
	}

	response, err := client.GetResponse(messages)
	if err != nil {
		return fmt.Errorf("AI revision failed: %w", err)
	}

	fmt.Println("\n▶ Writing revised research...")

	researchMD = []byte(extractMarkdown(response))

	if err := os.WriteFile(filepath.Join(state.GetGeneratedDir(), "research.md"), []byte(researchMD), 0644); err != nil {
		return fmt.Errorf("failed to write research: %w", err)
	}

	fmt.Println(strings.Repeat("─", 60))
	fmt.Println("\n✓ Research revised!")
	fmt.Printf("\nFeedback: %s\n", render.Truncate(feedback, 80))
	fmt.Println("\nReview: cat .maia/.generated/research.md")
	fmt.Println("Plan:   maia plan")
	fmt.Println("Steer:  maia steer --research 'more feedback'")

	return nil
}

func init() {
	steerCmd.Flags().BoolVar(&steerResearch, "research", false, "Revise research instead of plan")
	rootCmd.AddCommand(steerCmd)
}
