# maia

An AI-powered CLI that plans and executes code changes. Describe what you want, and maia researches your codebase, creates a phased implementation plan, and executes it.

## Install

```bash
go install github.com/sceptyre/maia@latest
```

## Setup

Create `~/.maia/config.json`:

```json
{
  "openai_api_key": "your-api-key",
  "openai_base_url": "https://api.openai.com/v1",
  "model": "gpt-4"
}
```

Or use command syntax for secrets:

```json
{
  "openai_api_key": "{cmd:op read op://vault/maia/api-key}"
}
```

Environment variables also work:

```bash
export OPENAI_API_KEY=your-key
```

## Quick Start

```bash
# Create a change request (creates worktree + change.md)
maia new "Add user authentication"

# Navigate to the worktree
cd ~/.maia/worktrees/<repo-name>/add-user-auth

# Write your goal in change.md
vim .maia/change.md

# Research codebase and web
maia init

# Generate implementation plan
maia plan

# Revise if needed
maia steer "use bcrypt not argon2"
maia steer "add a phase for database migrations"

# Execute the plan
maia apply

# Merge back to main
maia merge

# Cleanup worktree
maia cleanup
```

## Commands

| Command | Description |
|---------|-------------|
| `maia new "goal"` | Create isolated worktree + change.md |
| `maia list` | List active worktrees |
| `maia init` | Research codebase + web → research.md |
| `maia plan` | Generate implementation plan → plan.md |
| `maia steer "feedback"` | Revise plan based on feedback |
| `maia apply` | Execute the plan |
| `maia merge` | Merge worktree back to main |
| `maia cleanup` | Remove worktree |
| `maia config` | Show current configuration |

## Apply Options

```bash
maia apply              # Execute all phases
maia apply --phase 1    # Execute specific phase
maia apply --dry-run    # Preview without changes
```

## Steer Options

```bash
maia steer "use bcrypt not argon2"           # Revise plan
maia steer --research "also look at X"       # Revise research
```

## How It Works

1. **new** - Creates a git worktree in `~/.maia/worktrees/<repo>/` with a `change.md` template
2. **init** - Orchestrator spawns code and web agents to research the codebase and external concepts
3. **plan** - AI generates a phased implementation plan with specific artifacts and code samples
4. **steer** - Revise research or plan based on your feedback
5. **apply** - Orchestrator delegates implementation tasks phase by phase
6. **merge** - Merges worktree branch back to main
7. **cleanup** - Removes the worktree

## Worktree Structure

```
~/.maia/worktrees/<repo>/<slug>/
├── .maia/
│   ├── change.md          # Your goal (user-authored)
│   └── .generated/
│       ├── research.md    # AI research output
│       └── plan.md        # AI implementation plan
└── ... (code)
```
