package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/oblongata/maia/internal/state"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Research and analyze codebase",
	Long: `Scans the codebase to discover:
  - Project structure and patterns
  - Relevant existing code for the change
  - Dependencies and relationships
  - Existing conventions to follow

Output is saved to .maia/.generated/research.md for use by 'maia plan'.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Verify we're in a maia worktree
		if !state.HasWorktree() {
			return fmt.Errorf("not in a maia worktree. Run 'maia new' first, then cd to the worktree")
		}

		// Check for change.md
		if !state.HasChangeFile() {
			return fmt.Errorf("change.md not found. Run 'maia new' first")
		}

		// Read change.md
		changeMD, err := os.ReadFile(filepath.Join(state.StateDir, "change.md"))
		if err != nil {
			return fmt.Errorf("failed to read change.md: %w", err)
		}

		// Parse change info from frontmatter
		reqs := parseChangeRequirements(string(changeMD))

		fmt.Println("═══════════════════════════════════════════════════════════════")
		fmt.Println("  MAIA INIT - Codebase Research & Discovery")
		fmt.Println("═══════════════════════════════════════════════════════════════")
		fmt.Printf("\nChange Request: %s\n", reqs.Description)
		fmt.Println(strings.Repeat("─", 60))

		// Scan project structure
		fmt.Println("\n▶ Scanning project structure...")
		cwd, _ := os.Getwd()
		project := scanProject(cwd)
		fmt.Printf("  ✓ Root: %s\n", cwd)
		fmt.Printf("  ✓ Languages: %v\n", project.Languages)
		fmt.Printf("  ✓ Total files: %d\n", project.FileCount)

		// Discover patterns
		fmt.Println("\n▶ Discovering patterns and conventions...")
		patterns := discoverPatterns(project)
		fmt.Printf("  ✓ Project layout: %s\n", patterns.Layout)
		fmt.Printf("  ✓ Naming conventions: %s\n", patterns.Naming)

		// Find relevant code
		fmt.Println("\n▶ Finding relevant code for this change...")
		relevant := findRelevantCode(project, reqs)
		fmt.Printf("  ✓ Related files found: %d\n", len(relevant.Files))

		// Analyze dependencies
		fmt.Println("\n▶ Analyzing dependencies...")
		deps := analyzeDependencies(project)
		fmt.Printf("  ✓ Dependencies: %d\n", deps.External)
		fmt.Printf("  ✓ Internal modules: %d\n", deps.Internal)

		// Write research.md
		fmt.Println("\n▶ Writing research output...")
		if err := writeResearch(project, reqs, patterns, relevant, deps); err != nil {
			return fmt.Errorf("failed to write research: %w", err)
		}

		// Update status
		if err := state.SetStatus("analyzed"); err != nil {
			return err
		}

		fmt.Println(strings.Repeat("─", 60))
		fmt.Println("\n✓ Research complete!")
		fmt.Printf("\nOutput: .maia/.generated/research.md\n")
		fmt.Println("\nNext step: maia plan")

		return nil
	},
}

type ProjectInfo struct {
	RootDir     string
	Languages   []string
	FileCount   int
	Directories []string
	Structure   map[string][]string
}

type Patterns struct {
	Layout        string
	Naming        string
	ErrorHandling string
}

type RelevantCode struct {
	Files    []RelevantFile
	Patterns []string
}

type RelevantFile struct {
	Path      string
	Purpose   string
	Relevance string
}

type Dependencies struct {
	External []string
	Internal []string
}

type Requirements struct {
	ID           string
	Description  string
	Goal         string
	Requirements []string
	Files        []string
	Constraints  []string
	Research     []string
	Notes        []string
}

func scanProject(rootDir string) *ProjectInfo {
	project := &ProjectInfo{
		RootDir:   rootDir,
		Structure: make(map[string][]string),
	}
	langMap := make(map[string]bool)

	filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		relPath, _ := filepath.Rel(rootDir, path)

		if info.IsDir() {
			name := info.Name()
			if name == ".git" || name == "vendor" || name == "node_modules" || name == ".maia" {
				return filepath.SkipDir
			}
			project.Directories = append(project.Directories, relPath)
			return nil
		}

		project.FileCount++

		ext := filepath.Ext(path)
		if lang, ok := extensionToLanguage[ext]; ok {
			langMap[lang] = true
		}

		dir := filepath.Dir(relPath)
		project.Structure[dir] = append(project.Structure[dir], info.Name())

		return nil
	})

	for lang := range langMap {
		project.Languages = append(project.Languages, lang)
	}
	return project
}

func discoverPatterns(project *ProjectInfo) *Patterns {
	patterns := &Patterns{}

	hasCmd := false
	hasInternal := false
	for _, dir := range project.Directories {
		switch {
		case strings.HasPrefix(dir, "cmd"):
			hasCmd = true
		case strings.HasPrefix(dir, "internal"):
			hasInternal = true
		}
	}

	switch {
	case hasCmd && hasInternal:
		patterns.Layout = "Standard Go layout (cmd/internal/pkg)"
	case hasCmd:
		patterns.Layout = "cmd-based layout"
	default:
		patterns.Layout = "Flat or custom layout"
	}

	patterns.Naming = "snake_case for files, PascalCase for types"
	patterns.ErrorHandling = "Error wrapping with context"

	return patterns
}

func findRelevantCode(project *ProjectInfo, reqs *Requirements) *RelevantCode {
	relevant := &RelevantCode{}

	for _, mentionedFile := range reqs.Files {
		if mentionedFile == "" {
			continue
		}
		filepath.Walk(project.RootDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if strings.Contains(path, mentionedFile) {
				relevant.Files = append(relevant.Files, RelevantFile{
					Path:      path,
					Purpose:   "Mentioned in requirements",
					Relevance: "Direct",
				})
			}
			return nil
		})
	}

	return relevant
}

func analyzeDependencies(project *ProjectInfo) *Dependencies {
	deps := &Dependencies{}

	goModPath := filepath.Join(project.RootDir, "go.mod")
	if _, err := os.Stat(goModPath); err == nil {
		deps.External = []string{"(parsed from go.mod)"}
	}

	pkgJSONPath := filepath.Join(project.RootDir, "package.json")
	if _, err := os.Stat(pkgJSONPath); err == nil {
		deps.External = append(deps.External, "(parsed from package.json)")
	}

	for _, dir := range project.Directories {
		if strings.HasPrefix(dir, "internal/") || strings.HasPrefix(dir, "pkg/") {
			parts := strings.Split(dir, "/")
			if len(parts) >= 2 {
				deps.Internal = append(deps.Internal, parts[1])
			}
		}
	}

	return deps
}

func parseChangeRequirements(content string) *Requirements {
	reqs := &Requirements{}

	lines := strings.Split(content, "\n")
	var currentSection string

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Parse frontmatter
		if strings.HasPrefix(line, "description:") {
			reqs.Description = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
			continue
		}
		if strings.HasPrefix(line, "name:") {
			reqs.ID = strings.TrimSpace(strings.TrimPrefix(line, "name:"))
			continue
		}

		// Parse headers
		if strings.HasPrefix(line, "## ") {
			currentSection = strings.ToLower(strings.TrimPrefix(line, "## "))
			continue
		}

		// Parse list items
		if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "- [ ] ") {
			item := strings.TrimPrefix(line, "- [ ] ")
			item = strings.TrimPrefix(item, "- ")
			if item == "" {
				continue
			}

			switch currentSection {
			case "goal":
				reqs.Goal = item
			case "requirements":
				reqs.Requirements = append(reqs.Requirements, item)
			case "files to modify":
				reqs.Files = append(reqs.Files, item)
			case "constraints":
				reqs.Constraints = append(reqs.Constraints, item)
			case "research":
				reqs.Research = append(reqs.Research, item)
			case "notes":
				reqs.Notes = append(reqs.Notes, item)
			}
		}
	}

	return reqs
}

func writeResearch(project *ProjectInfo, reqs *Requirements, patterns *Patterns, relevant *RelevantCode, deps *Dependencies) error {
	var sb strings.Builder

	// Frontmatter
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("request_id: %s\n", reqs.ID))
	sb.WriteString(fmt.Sprintf("generated: %s\n", "now"))
	sb.WriteString("---\n\n")

	// Header
	sb.WriteString("# Codebase Research\n\n")

	// Project Overview
	sb.WriteString("## Project Overview\n\n")
	sb.WriteString(fmt.Sprintf("- **Root:** %s\n", project.RootDir))
	sb.WriteString(fmt.Sprintf("- **Languages:** %s\n", strings.Join(project.Languages, ", ")))
	sb.WriteString(fmt.Sprintf("- **Files:** %d\n", project.FileCount))
	sb.WriteString(fmt.Sprintf("- **Layout:** %s\n\n", patterns.Layout))

	// Directory Structure
	sb.WriteString("## Directory Structure\n\n")
	sb.WriteString("```\n")
	for _, dir := range project.Directories {
		if !strings.Contains(dir, "/") || strings.Count(dir, "/") <= 1 {
			sb.WriteString(fmt.Sprintf("%s/\n", dir))
		}
	}
	sb.WriteString("```\n\n")

	// Patterns and Conventions
	sb.WriteString("## Patterns & Conventions\n\n")
	sb.WriteString(fmt.Sprintf("- **Naming:** %s\n", patterns.Naming))
	sb.WriteString(fmt.Sprintf("- **Error Handling:** %s\n\n", patterns.ErrorHandling))

	// Relevant Code
	sb.WriteString("## Relevant Code\n\n")
	if len(relevant.Files) > 0 {
		for _, f := range relevant.Files {
			sb.WriteString(fmt.Sprintf("### %s\n", f.Path))
			sb.WriteString(fmt.Sprintf("- **Purpose:** %s\n", f.Purpose))
			sb.WriteString(fmt.Sprintf("- **Relevance:** %s\n\n", f.Relevance))
		}
	} else {
		sb.WriteString("No directly relevant files identified.\n\n")
	}

	// Dependencies
	sb.WriteString("## Dependencies\n\n")
	if len(deps.External) > 0 {
		sb.WriteString("### External\n")
		for _, dep := range deps.External {
			sb.WriteString(fmt.Sprintf("- %s\n", dep))
		}
		sb.WriteString("\n")
	}
	if len(deps.Internal) > 0 {
		sb.WriteString("### Internal Modules\n")
		for _, mod := range deps.Internal {
			sb.WriteString(fmt.Sprintf("- %s\n", mod))
		}
		sb.WriteString("\n")
	}

	return os.WriteFile(filepath.Join(state.GetGeneratedDir(), "research.md"), []byte(sb.String()), 0644)
}

var extensionToLanguage = map[string]string{
	".go":   "go",
	".py":   "python",
	".js":   "javascript",
	".ts":   "typescript",
	".rs":   "rust",
	".java": "java",
	".rb":   "ruby",
	".c":    "c",
	".cpp":  "cpp",
	".h":    "c",
	".sh":   "bash",
	".yaml": "yaml",
	".yml":  "yaml",
	".json": "json",
	".toml": "toml",
	".md":   "markdown",
}

func init() {
	rootCmd.AddCommand(initCmd)
}
