package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/oblongata/maia/internal/state"
	"github.com/spf13/cobra"
)

var (
	dryRun bool
	phase  int
	force  bool
)

var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Execute planned changes",
	Long: `Apply the planned changes phase by phase.
Without flags, executes the next pending phase.
With --phase N, executes a specific phase.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Verify we're in a maia worktree
		if !state.HasWorktree() {
			return fmt.Errorf("not in a maia worktree. Run 'maia new' first, then cd to the worktree")
		}

		// Check for plan
		if !state.HasPlan() {
			return fmt.Errorf("plan.md not found. Run 'maia plan' first")
		}

		// Check status
		status, _ := state.GetStatus()
		if status != "planned" && status != "applying" {
			return fmt.Errorf("plan not ready (status: %s). Run 'maia plan' first", status)
		}

		// Read plan.md
		plan, err := readPlan()
		if err != nil {
			return fmt.Errorf("failed to read plan: %w", err)
		}

		fmt.Println("═══════════════════════════════════════════════════════════════")
		fmt.Println("  MAIA APPLY - Executing Implementation Plan")
		fmt.Println("═══════════════════════════════════════════════════════════════")
		fmt.Printf("\nPhases: %d\n", len(plan.Phases))
		fmt.Println(strings.Repeat("─", 60))

		// Update status
		state.SetStatus("applying")

		// Determine which phase to execute
		var phasesToExecute []Phase
		if phase > 0 {
			for _, p := range plan.Phases {
				if p.Order == phase {
					phasesToExecute = append(phasesToExecute, p)
					break
				}
			}
			if len(phasesToExecute) == 0 {
				return fmt.Errorf("phase %d not found", phase)
			}
		} else {
			phasesToExecute = plan.Phases
		}

		// Execute phases
		for _, p := range phasesToExecute {
			if err := executePhase(p, dryRun, force); err != nil {
				state.SetStatus("error")
				return fmt.Errorf("phase %d failed: %w", p.Order, err)
			}
		}

		// Update status
		state.SetStatus("applied")

		fmt.Println(strings.Repeat("─", 60))
		fmt.Println("\n✓ All phases executed successfully!")
		fmt.Println("\nNext steps:")
		fmt.Println("  - Review changes: git diff")
		fmt.Println("  - Commit: git add . && git commit")
		fmt.Println("  - Merge back: maia merge")

		return nil
	},
}

func readPlan() (*Plan, error) {
	data, err := os.ReadFile(filepath.Join(state.GetGeneratedDir(), "plan.md"))
	if err != nil {
		return nil, err
	}

	// Simple markdown parser for plan
	plan := &Plan{}
	content := string(data)
	lines := strings.Split(content, "\n")

	var currentPhase *Phase
	inTable := false

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Parse phase headers
		if strings.HasPrefix(line, "## Phase ") {
			if currentPhase != nil {
				plan.Phases = append(plan.Phases, *currentPhase)
			}
			currentPhase = &Phase{}
			// Extract phase number and name
			parts := strings.SplitN(strings.TrimPrefix(line, "## Phase "), ": ", 2)
			if len(parts) == 2 {
				fmt.Sscanf(parts[0], "%d", &currentPhase.Order)
				currentPhase.Name = parts[1]
			}
			inTable = false
			continue
		}

		// Parse table rows
		if currentPhase != nil && strings.HasPrefix(line, "| `") {
			parts := strings.Split(line, "|")
			if len(parts) >= 4 {
				art := Artifact{
					Path:        strings.Trim(parts[1], " `"),
					Action:      strings.TrimSpace(parts[2]),
					Description: strings.TrimSpace(parts[3]),
				}
				currentPhase.Artifacts = append(currentPhase.Artifacts, art)
			}
		}

		// Detect table header
		if strings.HasPrefix(line, "| Artifact") {
			inTable = true
			continue
		}
		if inTable && strings.HasPrefix(line, "|--") {
			continue
		}
	}

	// Add last phase
	if currentPhase != nil {
		plan.Phases = append(plan.Phases, *currentPhase)
	}

	return plan, nil
}

func executePhase(phase Phase, dryRun, force bool) error {
	fmt.Printf("\n▶ Phase %d: %s\n", phase.Order, phase.Name)
	fmt.Printf("  %s\n", phase.Description)
	fmt.Println(strings.Repeat("─", 40))

	for _, artifact := range phase.Artifacts {
		if err := executeArtifact(artifact, dryRun, force); err != nil {
			return err
		}
	}

	fmt.Printf("\n✓ Phase %d complete\n", phase.Order)
	return nil
}

func executeArtifact(artifact Artifact, dryRun, force bool) error {
	fmt.Printf("\n  [%s] %s\n", strings.ToUpper(artifact.Action), artifact.Path)
	fmt.Printf("  %s\n", artifact.Description)

	if dryRun {
		fmt.Println("  (dry run - skipping)")
		return nil
	}

	// Check if file exists
	if _, err := os.Stat(artifact.Path); os.IsNotExist(err) {
		if artifact.Action == "modify" {
			fmt.Printf("  ⚠ File does not exist, will create\n")
		}
	} else if artifact.Action == "create" && !force {
		fmt.Printf("  ⚠ File already exists\n")
		if !promptContinue("  Overwrite?") {
			fmt.Println("  (skipped)")
			return nil
		}
	}

	fmt.Println("  ℹ Implementation required - use 'maia apply' with AI agents to execute")
	return nil
}

func promptContinue(message string) bool {
	fmt.Print(message + " [y/N] ")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	response := strings.ToLower(scanner.Text())
	return response == "y" || response == "yes"
}

func runGit(args ...string) error {
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Println(string(out))
	}
	return err
}

func init() {
	applyCmd.Flags().BoolVarP(&dryRun, "dry-run", "d", false, "Preview changes without applying")
	applyCmd.Flags().IntVarP(&phase, "phase", "p", 0, "Execute specific phase (0 = all)")
	applyCmd.Flags().BoolVarP(&force, "force", "f", false, "Overwrite existing files without prompting")
	rootCmd.AddCommand(applyCmd)
}
