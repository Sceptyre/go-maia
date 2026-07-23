package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sceptyre/maia/internal/git"
	"github.com/sceptyre/maia/internal/state"
	"github.com/spf13/cobra"
)

var baseBranch string

var newCmd = &cobra.Command{
	Use:   "new [description]",
	Short: "Create a new change request",
	Long:  `Create an isolated worktree with a change.md template for your goals.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		description := args[0]

		// Ensure global worktrees directory exists
		if err := state.EnsureGlobalDir(); err != nil {
			return fmt.Errorf("failed to create worktrees directory: %w", err)
		}

		// Generate names
		slug := generateSlug(description)
		branch := fmt.Sprintf("maia/%s", slug)

		// Get repo-specific worktree directory
		repoDir, err := state.GetRepoWorktreeDir()
		if err != nil {
			return fmt.Errorf("failed to get repo name: %w", err)
		}

		// Ensure repo directory exists
		if err := os.MkdirAll(repoDir, 0755); err != nil {
			return fmt.Errorf("failed to create repo directory: %w", err)
		}

		worktreePath := filepath.Join(repoDir, slug)

		// Check if worktree already exists
		if _, err := os.Stat(worktreePath); err == nil {
			return fmt.Errorf("worktree already exists: %s\nUse a different description or run: maia cleanup %s", worktreePath, slug)
		}

		fmt.Printf("Creating change request: %s\n", description)
		fmt.Printf("Branch: %s\n", branch)
		fmt.Printf("Worktree: %s\n", worktreePath)

		// Create worktree
		if err := git.Create(worktreePath, branch, baseBranch); err != nil {
			return err
		}

		// Create .maia/.generated/ directory structure
		maiaDir := filepath.Join(worktreePath, state.StateDir)
		generatedDir := filepath.Join(maiaDir, state.GeneratedDir)
		if err := os.MkdirAll(generatedDir, 0755); err != nil {
			return fmt.Errorf("failed to create .maia directory: %w", err)
		}

		// Create change.md template in .maia/ (user file)
		changeMD := fmt.Sprintf(`---
name: %s
description: %s
status: new
---

`, slug, description)

		if err := os.WriteFile(filepath.Join(maiaDir, "change.md"), []byte(changeMD), 0644); err != nil {
			return fmt.Errorf("failed to create change.md: %w", err)
		}

		fmt.Printf("\n✓ Change request created\n")
		fmt.Printf("\nNext steps:\n")
		fmt.Printf("  maia edit %s          # Edit your change goals\n", slug)
		fmt.Printf("  maia show change %s   # View your change request\n", slug)
		fmt.Printf("  maia init             # Run AI research (in worktree)\n")
		fmt.Printf("  maia list             # See all active worktrees\n")
		return nil
	},
}

func generateSlug(description string) string {
	slug := strings.ToLower(description)
	slug = strings.ReplaceAll(slug, " ", "-")
	slug = strings.ReplaceAll(slug, "_", "-")
	var result []byte
	for _, c := range slug {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			result = append(result, byte(c))
		}
	}
	return string(result)
}

func runGitCommand(args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	return cmd.Output()
}

func init() {
	newCmd.Flags().StringVarP(&baseBranch, "base", "b", "", "Base branch to create worktree from (default: current branch)")
	rootCmd.AddCommand(newCmd)
}
