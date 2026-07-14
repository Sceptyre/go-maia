package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "maia",
	Short: "Code change planning and research assistant",
	Long: `Maia helps you plan and execute code changes with confidence.

Worktrees are stored at ~/.maia/worktrees/<repo>/

Workflow:
  maia new "description"   # Create isolated worktree + change.md
  
  # In the worktree:
  # 1. Edit .maia/change.md with your goals
  maia init                # Research: scan codebase, discover patterns
  maia plan                # Plan: generate implementation plan
  maia apply               # Execute: implement the plan phase by phase
  maia merge               # Merge back to main
  maia cleanup             # Remove worktree
  
  maia list                # Show active worktrees`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
