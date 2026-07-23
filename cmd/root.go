package cmd

import (
	"fmt"
	"os"

	"github.com/sceptyre/maia/internal/config"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "maia",
	Short: "AI-powered code change planning and execution assistant",
	Long: `Maia is an AI-powered CLI that helps you plan and execute code changes.

It analyzes your codebase, creates detailed implementation plans,
and executes them phase by phase using specialized AI agents.

Configuration:
  Config file: ~/.maia/config.json

  Keys:
    openai_api_key    - API key for LLM (supports {cmd:...} syntax)
    openai_base_url   - API base URL (default: https://api.openai.com/v1)
    model             - Model to use (default: gpt-4)
    brave_api_key     - Brave Search API key for web research

  Example config.json:
  {
    "openai_api_key": "{cmd:op read op://vault/mcp-api/token}",
    "openai_base_url": "https://api.openai.com/v1",
    "model": "gpt-4",
    "brave_api_key": "your-key"
  }

  Environment variables override config file values.

Workflow (from parent branch — never leave it):
  maia new "description"       # Create worktree + change.md
  maia list                    # Show active worktrees
  maia edit <slug>             # Edit change.md ($EDITOR or pipe)
  maia show change <slug>      # View change request
  maia diff <slug>             # View uncommitted changes
  maia commit <slug> "msg"     # Commit changes
  maia merge <slug>            # Merge back to main
  maia cleanup <slug>          # Remove worktree

  # Inside the worktree (when needed):
  maia init                    # AI research → research.md
  maia plan                    # AI plan → plan.md
  maia show plan               # Display the plan
  maia show research           # Display the research
  maia steer "feedback"        # Revise plan based on feedback
  maia apply                   # AI executes the plan

Steering:
  maia steer "use bcrypt not argon2"
  maia steer --research "also look at cmd/users.go for patterns"`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	// Load config at startup
	config.Load()
}
