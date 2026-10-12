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
    openai_headers    - Custom HTTP headers; {maia_session} is a stable per-CR ID
    model             - Model to use (default: gpt-4)
    brave_api_key     - Brave Search API key for web research

  Example config.json:
  {
    "openai_api_key": "{cmd:op read op://vault/mcp-api/token}",
    "openai_base_url": "https://api.openai.com/v1",
    "openai_headers": {"x-opencode-session": "{maia_session}"},
    "model": "gpt-4",
    "brave_api_key": "your-key"
  }

  Environment variables override config file values.

Workflow (from parent branch — never leave it):
  maia new "description"       # Create CR + materialize worktree
  maia list                    # Show all CRs and materialized worktrees
  maia materialize <slug>      # Restore a cleaned-up CR worktree
  maia update-base <slug>      # Rebase a CR onto its latest source branch
  maia update-base <slug> --continue  # Continue after resolving conflicts
  maia update-base <slug> --abort     # Abort a conflicted rebase
  maia resolve-conflicts <slug> # Ask the configured model to propose conflict resolutions

  maia edit <slug>             # Edit change.md ($EDITOR or pipe)
  maia show change <slug>      # View change request
  maia diff <slug>             # View uncommitted changes
  maia commit <slug> "msg"     # Commit changes
  maia merge <slug>            # Merge into the CR's source branch
  maia cleanup <slug>          # Detach worktree; retain CR and saved documents

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
