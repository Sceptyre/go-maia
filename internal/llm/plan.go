package llm

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// ---------- Types ----------

// PlanOutline is the structured output from the outline generation step.
// It contains only names, descriptions, and artifact tables — no code.
type PlanOutline struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Phases      []PhaseOutline `json:"phases"`
}

// PhaseOutline describes a single phase without code samples.
type PhaseOutline struct {
	Number      int             `json:"number"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Artifacts   []ArtifactBrief `json:"artifacts"`
}

// ArtifactBrief is a file-level summary for the outline.
type ArtifactBrief struct {
	Path        string `json:"path"`
	Action      string `json:"action"`      // "create" or "modify"
	Description string `json:"description"`
}

// PhaseContent is the full output of a single phase generation call.
type PhaseContent struct {
	Number   int
	Name     string
	Markdown string
}

// PlanConfig holds tunables for the plan generation pipeline.
type PlanConfig struct {
	// OutlineMaxTokens limits the outline LLM call. Should be small
	// since outlines contain no code. 0 = use client default.
	OutlineMaxTokens int
	// PhaseMaxTokens limits each per-phase LLM call. This is the key
	// knob: each phase must fit within this budget. 0 = use client default.
	PhaseMaxTokens int
	// MaxPhaseArtifacts caps artifacts per phase. If a phase in the
	// outline has more artifacts than this, the outline LLM is asked to split.
	MaxPhaseArtifacts int
}

// DefaultPlanConfig returns sensible defaults.
func DefaultPlanConfig() PlanConfig {
	return PlanConfig{
		OutlineMaxTokens:  16384,
		PhaseMaxTokens:    65536,
		MaxPhaseArtifacts: 2,
	}
}

// ---------- Step 1: Outline Generation ----------

// outlineSystemPrompt is the system prompt for the outline LLM call.
func outlineSystemPrompt() string {
	return `You are a planning coordinator. Your job is to analyze a change request and
research findings, then produce a structured JSON outline of implementation phases.

You do NOT write code. You only produce a JSON object describing the plan structure.

Each phase should be small and focused:
- Maximum 3 artifacts (files) per phase
- Each phase should be completable in one pass
- Phases should be ordered by dependency (foundational changes first)

Output ONLY valid JSON matching this schema:
{
  "name": "<short id from change.md frontmatter>",
  "description": "<one-line description>",
  "phases": [
    {
      "number": 1,
      "name": "<phase name>",
      "description": "<what this phase accomplishes>",
      "artifacts": [
        {
          "path": "path/to/file.go",
          "action": "create|modify",
          "description": "<what this file does or what changes>"
        }
      ]
    }
  ]
}`
}

// outlineUserPrompt builds the user prompt for outline generation.
func outlineUserPrompt(changeContent, researchContent string) string {
	return fmt.Sprintf(`## Change Request

%s

## Research Findings

%s

---

Analyze the change request and research findings.
Produce a JSON outline of implementation phases.

Rules:
- Split large changes into multiple small phases (max 3 artifacts each)
- Order phases by dependency (dependencies first)
- For "modify" actions, specify whether it's an "add" (new code insertion) or "replace" (swap existing code)
- Keep descriptions concise but specific

Output ONLY the JSON outline.`, changeContent, researchContent)
}

// GenerateOutline makes a single LLM call to produce the structured plan outline.
// The outline contains no code — only phase names, descriptions, and artifact tables.
func GenerateOutline(client *Client, changeContent, researchContent string, cfg PlanConfig) (*PlanOutline, error) {
	messages := []Message{
		NewMessage("system", outlineSystemPrompt()),
		NewMessage("user", outlineUserPrompt(changeContent, researchContent)),
	}

	fmt.Fprintf(os.Stderr, "  [plan] Generating outline...\n")

	raw, err := client.GetResponseWithMaxTokens(messages, cfg.OutlineMaxTokens)
	if err != nil {
		return nil, fmt.Errorf("outline generation failed: %w", err)
	}

	outline, err := parseOutline(raw)
	if err != nil {
		// Retry once with a more explicit prompt if JSON parsing fails
		fmt.Fprintf(os.Stderr, "  [plan] Outline parse failed, retrying with explicit JSON instruction...\n")
		raw, err = client.GetResponseWithMaxTokens(append(messages,
			NewMessage("assistant", raw),
			NewMessage("user", "Your previous response was not valid JSON. Output ONLY the JSON object, no markdown fences, no explanation.")),
			cfg.OutlineMaxTokens)
		if err != nil {
			return nil, fmt.Errorf("outline retry failed: %w", err)
		}
		outline, err = parseOutline(raw)
		if err != nil {
			return nil, fmt.Errorf("outline is not valid JSON after retry: %w\nRaw output:\n%s", err, raw)
		}
	}

	fmt.Fprintf(os.Stderr, "  [plan] Outline: %d phases, %d total artifacts\n",
		len(outline.Phases), countArtifacts(outline))

	return outline, nil
}

// parseOutline extracts JSON from the LLM response, handling markdown fences.
func parseOutline(raw string) (*PlanOutline, error) {
	raw = strings.TrimSpace(raw)

	// Strip markdown code fences if present
	if strings.HasPrefix(raw, "```") {
		lines := strings.Split(raw, "\n")
		start, end := 0, len(lines)
		for i, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				if start == 0 {
					start = i + 1 // skip opening fence
				} else {
					end = i // skip closing fence
				}
			}
		}
		raw = strings.TrimSpace(strings.Join(lines[start:end], "\n"))
	}

	var outline PlanOutline
	if err := json.Unmarshal([]byte(raw), &outline); err != nil {
		return nil, fmt.Errorf("JSON parse error: %w", err)
	}

	if len(outline.Phases) == 0 {
		return nil, fmt.Errorf("outline has no phases")
	}

	return &outline, nil
}

func countArtifacts(o *PlanOutline) int {
	total := 0
	for _, p := range o.Phases {
		total += len(p.Artifacts)
	}
	return total
}

// ---------- Step 2: Per-Phase Generation ----------

// phaseSystemPrompt is the system prompt for individual phase generation.
func phaseSystemPrompt() string {
	return `You are a planner, not a writer. You produce implementation plans —
documents that describe WHAT to change and HOW, not the files themselves.

You are generating ONE phase of an implementation plan. Your output must contain:

1. The phase header: ## Phase N: <name>
2. A description of what this phase accomplishes
3. An artifact table for this phase only
4. Code samples for each artifact

Output format:

## Phase N: <name>

<description>

| Artifact | Action | Description |
|----------|--------|-------------|
| path/to/file.go | create | <what this file does> |
| path/to/other.go | modify | <what changes> |

### path/to/file.go (create)

**<what this file does>**

` + "```" + `
go
<full file content>
` + "```" + `

### path/to/other.go (modify: add/replace)

**<what this change does>**

Action type: add after ` + "`" + `function X` + "`" + ` / replace the existing ` + "`" + `function Y` + "`" + `

Placement: <where in the file — be specific>

` + "```" + `
go
<code to insert or replacement code>
` + "```" + `

IMPORTANT:
- Include ALL artifacts listed for this phase
- For create: include the full file content
- For modify 'add': include only the NEW code to insert
- For modify 'replace': include the old code to find AND the new code
- Always specify exactly WHERE in the file the change goes
- This is a PLAN — describe changes, do not write actual files.`
}

// phaseUserPrompt builds the user prompt for a single phase.
func phaseUserPrompt(
	changeContent string,
	researchContent string,
	outline *PlanOutline,
	phaseIdx int,
	priorPhaseSummaries string,
) string {
	phase := outline.Phases[phaseIdx]

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Change Request\n\n%s\n\n", changeContent))
	sb.WriteString(fmt.Sprintf("## Research Findings\n\n%s\n\n", researchContent))

	// Provide the full outline as context
	sb.WriteString("## Plan Outline\n\n")
	sb.WriteString(fmt.Sprintf("**Plan**: %s — %s\n\n", outline.Name, outline.Description))
	for _, p := range outline.Phases {
		marker := "  "
		if p.Number == phase.Number {
			marker = "▶ "
		}
		sb.WriteString(fmt.Sprintf("%s**Phase %d**: %s\n", marker, p.Number, p.Name))
		sb.WriteString(fmt.Sprintf("   %s\n", p.Description))
		sb.WriteString(fmt.Sprintf("   Artifacts: %d\n\n", len(p.Artifacts)))
	}

	// Include prior phase summaries for dependency awareness
	if priorPhaseSummaries != "" {
		sb.WriteString("## Previously Completed Phases (summary)\n\n")
		sb.WriteString(priorPhaseSummaries)
		sb.WriteString("\n\n")
	}

	// Detailed artifact specification for this phase
	sb.WriteString(fmt.Sprintf("## Generate Phase %d: %s\n\n", phase.Number, phase.Name))
	sb.WriteString(fmt.Sprintf("**Description**: %s\n\n", phase.Description))
	sb.WriteString("**Artifacts to include**:\n\n")
	for _, a := range phase.Artifacts {
		sb.WriteString(fmt.Sprintf("- `%s` (%s): %s\n", a.Path, a.Action, a.Description))
	}
	sb.WriteString(fmt.Sprintf("\n---\n\nGenerate the complete plan content for Phase %d now.\n", phase.Number))

	return sb.String()
}

// GeneratePhase generates the full markdown content for a single phase.
// It receives the full plan context but only generates code for one phase.
func GeneratePhase(
	client *Client,
	changeContent string,
	researchContent string,
	outline *PlanOutline,
	phaseIdx int,
	priorPhaseSummaries string,
	toolHandler func(ToolCall) (string, error),
	cfg PlanConfig,
) (*PhaseContent, error) {
	phase := outline.Phases[phaseIdx]

	fmt.Fprintf(os.Stderr, "  [plan] Generating phase %d: %s...\n", phase.Number, phase.Name)

	messages := []Message{
		NewMessage("system", phaseSystemPrompt()),
		NewMessage("user", phaseUserPrompt(changeContent, researchContent, outline, phaseIdx, priorPhaseSummaries)),
	}

	markdown, _, err := client.GetResponseWithTools(messages, ReadOnlyTools, toolHandler, cfg.PhaseMaxTokens)
	if err != nil {
		return nil, fmt.Errorf("phase %d generation failed: %w", phase.Number, err)
	}

	return &PhaseContent{
		Number:   phase.Number,
		Name:     phase.Name,
		Markdown: markdown,
	}, nil
}

// buildPriorPhaseSummaries creates a compact summary of completed phases.
// Only includes phase names and artifact tables (no code) to keep context small.
func buildPriorPhaseSummaries(completed []*PhaseContent) string {
	if len(completed) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, c := range completed {
		sb.WriteString(fmt.Sprintf("### Phase %d: %s\n\n", c.Number, c.Name))

		// Extract just the artifact table from the markdown
		lines := strings.Split(c.Markdown, "\n")
		inTable := false
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "| Artifact") {
				inTable = true
			}
			if inTable {
				sb.WriteString(line + "\n")
				if strings.HasPrefix(trimmed, "|---") {
					continue
				}
				if trimmed == "" || !strings.HasPrefix(trimmed, "|") {
					inTable = false
					sb.WriteString("\n")
				}
			}
		}
	}

	return sb.String()
}

// ---------- Step 3: Assembly ----------

// assemblePlan stitches the outline frontmatter and phase contents into a
// single plan.md document. No LLM call needed — this is pure string assembly.
func assemblePlan(outline *PlanOutline, phases []*PhaseContent) string {
	var sb strings.Builder

	// Frontmatter
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("name: %s\n", outline.Name))
	sb.WriteString(fmt.Sprintf("description: %s\n", outline.Description))
	sb.WriteString("status: planned\n")
	sb.WriteString("risk: medium\n") // default; can be overridden by review
	sb.WriteString(fmt.Sprintf("phases: %d\n", len(phases)))
	sb.WriteString("---\n\n")

	// Title and summary
	sb.WriteString(fmt.Sprintf("# Plan: %s\n\n", outline.Description))
	sb.WriteString(fmt.Sprintf("%s\n\n", outline.Description))

	// Each phase, cleaned up to ensure consistent header format
	for _, p := range phases {
		content := cleanPhaseContent(p)
		sb.WriteString(content)
		sb.WriteString("\n\n")
	}

	return sb.String()
}

// cleanPhaseContent normalizes the phase markdown to ensure consistent formatting.
func cleanPhaseContent(p *PhaseContent) string {
	content := strings.TrimSpace(p.Markdown)

	lines := strings.Split(content, "\n")

	// Find the first "## Phase" line and ensure it uses our canonical numbering
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## Phase") || strings.HasPrefix(trimmed, "# Phase") {
			lines[i] = fmt.Sprintf("## Phase %d: %s", p.Number, p.Name)
			break
		}
	}

	return strings.Join(lines, "\n")
}

// ---------- Public Entry Point ----------

// GeneratePlan implements the full three-step plan generation pipeline.
//
// Step 1: GenerateOutline — produces a structured JSON outline (no code)
// Step 2: GeneratePhase × N — each phase generated independently with full context
// Step 3: AssemblePlan — stitch frontmatter + phases into final markdown
//
// This replaces the previous single-call approach that truncated on large plans.
func GeneratePlan(
	client *Client,
	changeContent string,
	researchContent string,
	toolHandler func(ToolCall) (string, error),
	cfg PlanConfig,
) (string, error) {
	// Step 1: Outline
	outline, err := GenerateOutline(client, changeContent, researchContent, cfg)
	if err != nil {
		return "", fmt.Errorf("step 1 (outline) failed: %w", err)
	}

	// Step 2: Generate each phase independently
	phases := make([]*PhaseContent, 0, len(outline.Phases))

	for i := range outline.Phases {
		priorSummaries := buildPriorPhaseSummaries(phases)

		phase, err := GeneratePhase(
			client, changeContent, researchContent,
			outline, i, priorSummaries,
			toolHandler, cfg,
		)
		if err != nil {
			return "", fmt.Errorf("step 2 (phase %d) failed: %w", i+1, err)
		}

		phases = append(phases, phase)
	}

	// Step 3: Assembly
	plan := assemblePlan(outline, phases)

	return plan, nil
}
