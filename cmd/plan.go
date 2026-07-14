package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/oblongata/maia/internal/state"
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

		// Parse change info
		reqs := parseChangeRequirements(string(changeMD))

		fmt.Println("═══════════════════════════════════════════════════════════════")
		fmt.Println("  MAIA PLAN - Implementation Plan Generation")
		fmt.Println("═══════════════════════════════════════════════════════════════")
		fmt.Printf("\nChange Request: %s\n", reqs.Description)
		fmt.Println(strings.Repeat("─", 60))

		// Analyze inputs
		fmt.Println("\n▶ Analyzing inputs...")
		fmt.Printf("  ✓ Goals parsed from change.md\n")
		fmt.Printf("  ✓ Research loaded from research.md (%d bytes)\n", len(researchMD))

		// Synthesize plan
		fmt.Println("\n▶ Synthesizing implementation plan...")
		plan := synthesizePlan(reqs, string(researchMD))

		// Generate plan.md
		fmt.Println("\n▶ Generating plan document...")
		if err := writePlanMarkdown(plan, reqs); err != nil {
			return fmt.Errorf("failed to write plan: %w", err)
		}

		// Update status
		if err := state.SetStatus("planned"); err != nil {
			return err
		}

		// Summary
		fmt.Println(strings.Repeat("─", 60))
		fmt.Println("\n✓ Plan generated!")
		fmt.Printf("\n  Phases: %d\n", len(plan.Phases))
		fmt.Printf("  Artifacts: %d\n", countArtifacts(plan))
		fmt.Printf("  Risk: %s\n", plan.RiskLevel)
		fmt.Printf("\nOutput: .maia/.generated/plan.md\n")
		fmt.Println("\nReview the plan, then run: maia apply")

		return nil
	},
}

type Plan struct {
	Phases    []Phase
	RiskLevel string
	Summary   string
}

type Phase struct {
	Order       int
	Name        string
	Description string
	Artifacts   []Artifact
	DependsOn   []int
}

type Artifact struct {
	Path        string
	Action      string
	Description string
}

func synthesizePlan(reqs *Requirements, research string) Plan {
	plan := Plan{
		RiskLevel: assessRisk(reqs),
		Summary:   generateSummary(reqs),
	}

	// Phase 1: Foundation
	phase1 := Phase{
		Order:       1,
		Name:        "Foundation",
		Description: "Set up the basic structure, interfaces, and types needed for this change.",
		Artifacts:   []Artifact{},
	}

	for _, file := range reqs.Files {
		if file != "" {
			phase1.Artifacts = append(phase1.Artifacts, Artifact{
				Path:        file,
				Action:      "create",
				Description: fmt.Sprintf("Create %s with core interfaces", filepath.Base(file)),
			})
		}
	}

	if len(phase1.Artifacts) == 0 {
		phase1.Artifacts = append(phase1.Artifacts, Artifact{
			Path:        "(determined during implementation)",
			Action:      "create",
			Description: "Core types and interfaces",
		})
	}

	plan.Phases = append(plan.Phases, phase1)

	// Phase 2: Core Implementation
	phase2 := Phase{
		Order:       2,
		Name:        "Core Implementation",
		Description: "Implement the main functionality according to requirements.",
		DependsOn:   []int{1},
		Artifacts:   []Artifact{},
	}

	for i, req := range reqs.Requirements {
		if req != "" {
			phase2.Artifacts = append(phase2.Artifacts, Artifact{
				Path:        fmt.Sprintf("(implementation %d)", i+1),
				Action:      "modify",
				Description: req,
			})
		}
	}

	if len(phase2.Artifacts) == 0 {
		phase2.Artifacts = append(phase2.Artifacts, Artifact{
			Path:        "(implementation files)",
			Action:      "modify",
			Description: "Implement core functionality",
		})
	}

	plan.Phases = append(plan.Phases, phase2)

	// Phase 3: Integration
	phase3 := Phase{
		Order:       3,
		Name:        "Integration",
		Description: "Wire up the new code with existing systems.",
		DependsOn:   []int{2},
		Artifacts: []Artifact{
			{
				Path:        "(integration points)",
				Action:      "modify",
				Description: "Connect with existing codebase",
			},
		},
	}
	plan.Phases = append(plan.Phases, phase3)

	// Phase 4: Testing & Verification
	phase4 := Phase{
		Order:       4,
		Name:        "Testing & Verification",
		Description: "Add tests and verify the implementation works correctly.",
		DependsOn:   []int{3},
		Artifacts: []Artifact{
			{
				Path:        "*_test.go",
				Action:      "create",
				Description: "Unit tests for new functionality",
			},
		},
	}
	plan.Phases = append(plan.Phases, phase4)

	return plan
}

func assessRisk(reqs *Requirements) string {
	fileCount := len(reqs.Files)
	reqCount := len(reqs.Requirements)

	if fileCount > 5 || reqCount > 5 {
		return "high"
	}
	if fileCount > 2 || reqCount > 2 {
		return "medium"
	}
	return "low"
}

func generateSummary(reqs *Requirements) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("This plan implements: %s\n\n", reqs.Goal))

	if len(reqs.Requirements) > 0 {
		sb.WriteString("**Key requirements:**\n")
		for _, req := range reqs.Requirements {
			if req != "" {
				sb.WriteString(fmt.Sprintf("- %s\n", req))
			}
		}
		sb.WriteString("\n")
	}

	if len(reqs.Constraints) > 0 {
		sb.WriteString("**Constraints:**\n")
		for _, c := range reqs.Constraints {
			if c != "" {
				sb.WriteString(fmt.Sprintf("- %s\n", c))
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString("The plan is organized into phases to minimize risk and allow incremental verification.")

	return sb.String()
}

func writePlanMarkdown(plan Plan, reqs *Requirements) error {
	var sb strings.Builder

	// Frontmatter
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("name: %s\n", reqs.ID))
	sb.WriteString(fmt.Sprintf("description: %s\n", reqs.Description))
	sb.WriteString(fmt.Sprintf("status: planned\n"))
	sb.WriteString(fmt.Sprintf("risk: %s\n", plan.RiskLevel))
	sb.WriteString(fmt.Sprintf("phases: %d\n", len(plan.Phases)))
	sb.WriteString("---\n\n")

	// Header
	sb.WriteString(fmt.Sprintf("# Plan: %s\n\n", reqs.Description))
	sb.WriteString(fmt.Sprintf("%s\n\n", plan.Summary))

	// Phases
	for _, phase := range plan.Phases {
		sb.WriteString(fmt.Sprintf("## Phase %d: %s\n\n", phase.Order, phase.Name))
		sb.WriteString(fmt.Sprintf("%s\n\n", phase.Description))

		if len(phase.DependsOn) > 0 {
			sb.WriteString(fmt.Sprintf("**Depends on:** Phase %s\n\n", formatInts(phase.DependsOn)))
		}

		// Artifacts table
		sb.WriteString("| Artifact | Action | Description |\n")
		sb.WriteString("|----------|--------|-------------|\n")
		for _, art := range phase.Artifacts {
			sb.WriteString(fmt.Sprintf("| `%s` | %s | %s |\n", art.Path, art.Action, art.Description))
		}
		sb.WriteString("\n")
	}

	return os.WriteFile(filepath.Join(state.GetGeneratedDir(), "plan.md"), []byte(sb.String()), 0644)
}

func formatInts(ints []int) string {
	parts := make([]string, len(ints))
	for i, n := range ints {
		parts[i] = fmt.Sprintf("%d", n)
	}
	return strings.Join(parts, ", ")
}

func countArtifacts(plan Plan) int {
	count := 0
	for _, phase := range plan.Phases {
		count += len(phase.Artifacts)
	}
	return count
}

func init() {
	rootCmd.AddCommand(planCmd)
}
