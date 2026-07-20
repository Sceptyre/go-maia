package llm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Tool name constants — single source of truth for all tool name strings.
const (
	ToolNameReadFile    = "read_file"
	ToolNameWriteFile   = "write_file"
	ToolNameEditFile    = "edit_file"
	ToolNameListFiles   = "list_files"
	ToolNameSearchFiles = "search_files"
	ToolNameGrepContent = "grep_content"
)

// --- Typed argument structs for each tool ---

type readFileArgs struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"` // 1-based, optional (0 = beginning)
	EndLine   int    `json:"end_line"`   // 1-based, inclusive, optional (0 = end of file)
}

type editFileArgs struct {
	Path      string `json:"path"`
	Search    string `json:"search"`      // text-match mode: exact string to find
	Replace   string `json:"replace"`     // text-match mode: replacement string
	StartLine int    `json:"start_line"`  // range mode: 1-based start (mutually exclusive with Search)
	EndLine   int    `json:"end_line"`    // range mode: 1-based inclusive end
	NewText   string `json:"new_text"`    // range mode: replacement content
}

type writeFileArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type listFilesArgs struct {
	Path string `json:"path"`
}

type searchFilesArgs struct {
	Pattern string `json:"pattern"`
}

type grepContentArgs struct {
	Query string `json:"query"`
	Path  string `json:"path"`
}

// ReadOnlyTools are tools for research (no write access)
var ReadOnlyTools = []Tool{
	{
		Type: "function",
		Function: ToolFunction{
			Name:        ToolNameReadFile,
			Description: "Read the contents of a file. Use start_line/end_line to read a specific range (1-based, inclusive). Output includes line-number prefixes for precise editing.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Path to the file to read",
					},
					"start_line": map[string]interface{}{
						"type":        "integer",
						"description": "First line to read (1-based, inclusive). Default: 1",
					},
					"end_line": map[string]interface{}{
						"type":        "integer",
						"description": "Last line to read (1-based, inclusive). Default: end of file",
					},
				},
				"required": []string{"path"},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        ToolNameListFiles,
			Description: "List files in a directory.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Path to the directory",
					},
				},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        ToolNameSearchFiles,
			Description: "Search for files matching a pattern.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pattern": map[string]interface{}{
						"type":        "string",
						"description": "Glob pattern to match",
					},
				},
				"required": []string{"pattern"},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        ToolNameGrepContent,
			Description: "Search for content within files.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Text to search for",
					},
				},
				"required": []string{"query"},
			},
		},
	},
}

// FileTools are tools for reading and writing files
var FileTools = []Tool{
	{
		Type: "function",
		Function: ToolFunction{
			Name:        ToolNameReadFile,
			Description: "Read the contents of a file. Use start_line/end_line to read a specific range (1-based, inclusive). Output includes line-number prefixes for precise editing.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Path to the file to read",
					},
					"start_line": map[string]interface{}{
						"type":        "integer",
						"description": "First line to read (1-based, inclusive). Default: 1",
					},
					"end_line": map[string]interface{}{
						"type":        "integer",
						"description": "Last line to read (1-based, inclusive). Default: end of file",
					},
				},
				"required": []string{"path"},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        ToolNameEditFile,
			Description: "Edit a file using search/replace text matching or line-range replacement. Search/replace mode: provide 'search' (exact text to find) and 'replace' (new text). Line-range mode: provide 'start_line', 'end_line', and 'new_text'.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Path to the file to edit",
					},
					"search": map[string]interface{}{
						"type":        "string",
						"description": "Exact text to find (search/replace mode)",
					},
					"replace": map[string]interface{}{
						"type":        "string",
						"description": "Replacement text (search/replace mode)",
					},
					"start_line": map[string]interface{}{
						"type":        "integer",
						"description": "First line to replace (line-range mode, 1-based inclusive)",
					},
					"end_line": map[string]interface{}{
						"type":        "integer",
						"description": "Last line to replace (line-range mode, 1-based inclusive)",
					},
					"new_text": map[string]interface{}{
						"type":        "string",
						"description": "Replacement content (line-range mode)",
					},
				},
				"required": []string{"path"},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        ToolNameWriteFile,
			Description: "Write content to a file. Creates the file if it doesn't exist, overwrites if it does.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Path to the file to write",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "Content to write to the file",
					},
				},
				"required": []string{"path", "content"},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        ToolNameListFiles,
			Description: "List files in a directory. Returns file names and whether they are directories.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Path to the directory (default: current directory)",
					},
				},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        ToolNameSearchFiles,
			Description: "Search for files matching a pattern. Returns matching file paths.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pattern": map[string]interface{}{
						"type":        "string",
						"description": "Glob pattern to match files (e.g., '**/*.go', 'cmd/*.go')",
					},
				},
				"required": []string{"pattern"},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        ToolNameGrepContent,
			Description: "Search for content within files. Returns matching lines with file paths.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Text or regex pattern to search for",
					},
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Directory to search in (default: current directory)",
					},
				},
				"required": []string{"query"},
			},
		},
	},
}

// ToolCallResult represents the result of a tool call
type ToolCallResult struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

// HandleToolCall processes a tool call and returns the result
func HandleToolCall(call ToolCall, workDir string) (string, error) {
	switch call.Function.Name {
	case ToolNameReadFile:
		var a readFileArgs
		if err := json.Unmarshal([]byte(call.Function.Arguments), &a); err != nil {
			return "", fmt.Errorf("failed to parse arguments: %w", err)
		}
		return handleReadFile(a, workDir)
	case ToolNameEditFile:
		var a editFileArgs
		if err := json.Unmarshal([]byte(call.Function.Arguments), &a); err != nil {
			return "", fmt.Errorf("failed to parse arguments: %w", err)
		}
		return handleEditFile(a, workDir)
	case ToolNameWriteFile:
		var a writeFileArgs
		if err := json.Unmarshal([]byte(call.Function.Arguments), &a); err != nil {
			return "", fmt.Errorf("failed to parse arguments: %w", err)
		}
		return handleWriteFile(a.Path, a.Content, workDir)
	case ToolNameListFiles:
		var a listFilesArgs
		if err := json.Unmarshal([]byte(call.Function.Arguments), &a); err != nil {
			return "", fmt.Errorf("failed to parse arguments: %w", err)
		}
		return handleListFiles(a.Path, workDir)
	case ToolNameSearchFiles:
		var a searchFilesArgs
		if err := json.Unmarshal([]byte(call.Function.Arguments), &a); err != nil {
			return "", fmt.Errorf("failed to parse arguments: %w", err)
		}
		return handleSearchFiles(a.Pattern, workDir)
	case ToolNameGrepContent:
		var a grepContentArgs
		if err := json.Unmarshal([]byte(call.Function.Arguments), &a); err != nil {
			return "", fmt.Errorf("failed to parse arguments: %w", err)
		}
		return handleGrepContent(a.Query, a.Path, workDir)
	default:
		return "", fmt.Errorf("unknown tool: %s", call.Function.Name)
	}
}

func handleReadFile(a readFileArgs, workDir string) (string, error) {
	fullPath := resolvePath(a.Path, workDir)

	if !isWithinDir(fullPath, workDir) {
		return "", fmt.Errorf("security: cannot read outside working directory: %s", a.Path)
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	totalLines := len(lines)

	start := a.StartLine
	end := a.EndLine
	if start <= 0 {
		start = 1
	}
	if end <= 0 || end > totalLines {
		end = totalLines
	}
	if start > totalLines {
		return "", fmt.Errorf("start_line %d exceeds file length %d", start, totalLines)
	}
	if start > end {
		return "", fmt.Errorf("start_line %d is after end_line %d", start, end)
	}

	selected := lines[start-1 : end]
	var sb strings.Builder
	for i, line := range selected {
		sb.WriteString(fmt.Sprintf("%d\t%s\n", start+i, line))
	}
	return sb.String(), nil
}

func handleWriteFile(path, content, workDir string) (string, error) {
	fullPath := resolvePath(path, workDir)

	// Safety check: ensure we're writing within the working directory
	if !isWithinDir(fullPath, workDir) {
		return "", fmt.Errorf("security: cannot write outside working directory: %s", path)
	}

	// Ensure directory exists
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	return fmt.Sprintf("Successfully wrote to %s", path), nil
}

func handleEditFile(a editFileArgs, workDir string) (string, error) {
	fullPath := resolvePath(a.Path, workDir)

	if !isWithinDir(fullPath, workDir) {
		return "", fmt.Errorf("security: cannot write outside working directory: %s", a.Path)
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}
	content := string(data)

	// Mode 1: Text-match (search/replace)
	if a.Search != "" {
		if !strings.Contains(content, a.Search) {
			return "", fmt.Errorf("search text not found in %s — re-read the file and try again with the exact text", a.Path)
		}

		count := strings.Count(content, a.Search)
		if count > 1 {
			return "", fmt.Errorf("search text found %d times in %s — provide more context to make it unique", count, a.Path)
		}

		newContent := strings.Replace(content, a.Search, a.Replace, 1)

		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", fmt.Errorf("failed to create directory: %w", err)
		}
		if err := os.WriteFile(fullPath, []byte(newContent), 0644); err != nil {
			return "", fmt.Errorf("failed to write file: %w", err)
		}
		return fmt.Sprintf("Successfully edited %s (search/replace)", a.Path), nil
	}

	// Mode 2: Line-range replacement
	if a.StartLine > 0 && a.EndLine > 0 && a.NewText != "" {
		lines := strings.Split(content, "\n")
		totalLines := len(lines)

		if a.StartLine > totalLines {
			return "", fmt.Errorf("start_line %d exceeds file length %d", a.StartLine, totalLines)
		}
		if a.EndLine > totalLines {
			return "", fmt.Errorf("end_line %d exceeds file length %d", a.EndLine, totalLines)
		}
		if a.StartLine > a.EndLine {
			return "", fmt.Errorf("start_line %d is after end_line %d", a.StartLine, a.EndLine)
		}

		newLines := make([]string, 0, len(lines))
		newLines = append(newLines, lines[:a.StartLine-1]...)
		newLines = append(newLines, a.NewText)
		newLines = append(newLines, lines[a.EndLine:]...)

		newContent := strings.Join(newLines, "\n")

		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", fmt.Errorf("failed to create directory: %w", err)
		}
		if err := os.WriteFile(fullPath, []byte(newContent), 0644); err != nil {
			return "", fmt.Errorf("failed to write file: %w", err)
		}
		return fmt.Sprintf("Successfully edited %s (lines %d–%d)", a.Path, a.StartLine, a.EndLine), nil
	}

	return "", fmt.Errorf("edit_file requires either search/replace or start_line+end_line+new_text")
}

func handleListFiles(path, workDir string) (string, error) {
	if path == "" {
		path = "."
	}
	fullPath := resolvePath(path, workDir)

	// Safety check: ensure we're listing within the working directory
	if !isWithinDir(fullPath, workDir) {
		return "", fmt.Errorf("security: cannot list outside working directory: %s", path)
	}

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to list directory: %w", err)
	}

	var result []string
	for _, entry := range entries {
		if entry.IsDir() {
			result = append(result, entry.Name()+"/")
		} else {
			result = append(result, entry.Name())
		}
	}

	return strings.Join(result, "\n"), nil
}

func handleSearchFiles(pattern, workDir string) (string, error) {
	if pattern == "" {
		pattern = "*"
	}

	var matches []string
	err := filepath.Walk(workDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		// Safety check: stay within working directory
		if !isWithinDir(path, workDir) {
			return filepath.SkipDir
		}

		if info.IsDir() {
			name := info.Name()
			if name == ".git" || name == "vendor" || name == "node_modules" || name == ".maia" {
				return filepath.SkipDir
			}
			return nil
		}

		relPath, _ := filepath.Rel(workDir, path)
		matched, _ := filepath.Match(pattern, filepath.Base(path))
		if matched {
			matches = append(matches, relPath)
		}
		return nil
	})

	if err != nil {
		return "", fmt.Errorf("search failed: %w", err)
	}

	if len(matches) == 0 {
		return "No files found matching pattern: " + pattern, nil
	}

	return strings.Join(matches, "\n"), nil
}

func handleGrepContent(query, searchPath, workDir string) (string, error) {
	if searchPath == "" {
		searchPath = "."
	}
	fullPath := resolvePath(searchPath, workDir)

	// Safety check: ensure we're searching within the working directory
	if !isWithinDir(fullPath, workDir) {
		return "", fmt.Errorf("security: cannot search outside working directory: %s", searchPath)
	}

	var matches []string
	err := filepath.Walk(fullPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		// Safety check: stay within working directory
		if !isWithinDir(path, workDir) {
			return filepath.SkipDir
		}

		if info.IsDir() {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		content := string(data)
		if strings.Contains(content, query) {
			relPath, _ := filepath.Rel(workDir, path)
			lines := strings.Split(content, "\n")
			for i, line := range lines {
				if strings.Contains(line, query) {
					matches = append(matches, fmt.Sprintf("%s:%d: %s", relPath, i+1, strings.TrimSpace(line)))
				}
			}
		}
		return nil
	})

	if err != nil {
		return "", fmt.Errorf("search failed: %w", err)
	}

	if len(matches) == 0 {
		return "No matches found for: " + query, nil
	}

	if len(matches) > 50 {
		matches = matches[:50]
		matches = append(matches, "... (truncated)")
	}

	return strings.Join(matches, "\n"), nil
}

func resolvePath(path, workDir string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(workDir, path)
}

// isWithinDir checks if a path is within the specified directory
func isWithinDir(path, dir string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	return strings.HasPrefix(absPath, absDir)
}
