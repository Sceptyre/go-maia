package llm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Tool definitions for file operations
var FileTools = []Tool{
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "read_file",
			Description: "Read the contents of a file. Use this to examine code and understand the codebase.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Path to the file to read",
					},
				},
				"required": []string{"path"},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "write_file",
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
			Name:        "list_files",
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
			Name:        "search_files",
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
			Name:        "grep_content",
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
	var args map[string]string
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return "", fmt.Errorf("failed to parse arguments: %w", err)
	}

	switch call.Function.Name {
	case "read_file":
		return handleReadFile(args["path"], workDir)
	case "write_file":
		return handleWriteFile(args["path"], args["content"], workDir)
	case "list_files":
		return handleListFiles(args["path"], workDir)
	case "search_files":
		return handleSearchFiles(args["pattern"], workDir)
	case "grep_content":
		return handleGrepContent(args["query"], args["path"], workDir)
	default:
		return "", fmt.Errorf("unknown tool: %s", call.Function.Name)
	}
}

func handleReadFile(path, workDir string) (string, error) {
	fullPath := resolvePath(path, workDir)

	// Safety check: ensure we're reading within the working directory
	if !isWithinDir(fullPath, workDir) {
		return "", fmt.Errorf("security: cannot read outside working directory: %s", path)
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}
	return string(data), nil
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
