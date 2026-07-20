package render

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/glamour"
	"golang.org/x/term"
)

// isTerminal returns true if the given file descriptor is connected to a terminal.
func isTerminal(fd uintptr) bool {
	return term.IsTerminal(int(fd))
}

// IsTTY returns true if stdout is a terminal (not piped/redirected).
func IsTTY() bool {
	return isTerminal(os.Stdout.Fd())
}

// RenderMarkdown renders a markdown string for terminal display.
// It uses glamour when outputting to a TTY, and falls back to raw text
// when output is piped or redirected.
func RenderMarkdown(content string) string {
	content = strings.TrimSuffix(content, "\n")

	if !IsTTY() {
		return content
	}

	rendered, err := glamour.Render(content, "auto")
	if err != nil {
		fmt.Fprintf(os.Stderr, "  [debug] markdown render failed: %v\n", err)
		return content
	}

	return rendered
}

// Truncate shortens a string to max characters, appending "..." if truncated.
func Truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
