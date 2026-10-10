package web

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
)

var markdown = goldmark.New(
	goldmark.WithExtensions(
		extension.Table,
		extension.Strikethrough,
		extension.Linkify,
		extension.TaskList,
	),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
)

// RenderMarkdown converts markdown to HTML. Raw HTML in the source stays
// escaped: generated documents must not be able to inject scripts.
// YAML frontmatter is metadata and is not rendered.
func RenderMarkdown(src string) (string, error) {
	var buf bytes.Buffer
	if err := markdown.Convert([]byte(stripFrontmatter(src)), &buf); err != nil {
		return "", fmt.Errorf("markdown render failed: %w", err)
	}
	return buf.String(), nil
}

func stripFrontmatter(src string) string {
	if !strings.HasPrefix(src, "---\n") {
		return src
	}
	lines := strings.Split(src, "\n")
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\r") == "---" {
			return strings.Join(lines[i+1:], "\n")
		}
	}
	return src
}
