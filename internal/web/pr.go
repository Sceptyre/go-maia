package web

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/sceptyre/maia/internal/github"
	"github.com/sceptyre/maia/internal/state"
)

// handleCreatePR opens a draft pull request for an applied change request.
// Auth comes from the standard chain (gh CLI → env → config), so a working
// gh login is enough — no extra setup.
func (s *Server) handleCreatePR(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projectFrom(w, r)
	if !ok {
		return
	}
	var req struct {
		Slug  string `json:"slug"`
		Title string `json:"title,omitempty"`
		Body  string `json:"body,omitempty"`
	}
	if err := decodeBody(r, &req); err != nil || !slugRe.MatchString(req.Slug) {
		writeErr(w, http.StatusBadRequest, "slug is required")
		return
	}

	wtDir := filepath.Join(state.RepoWorktreeDirFor(p.Path), req.Slug)
	if !state.HasWorktreeIn(wtDir) {
		writeErr(w, http.StatusNotFound, "change request not found")
		return
	}

	// Resolve base: explicit project setting, else origin default, else main.
	base := p.BaseBranch
	if base == "" {
		if out, err := runGitIn(p.Path, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
			ref := strings.TrimSpace(string(out)) // e.g. origin/main
			base = strings.TrimPrefix(ref, "origin/")
		} else {
			base = "main"
		}
	}
	baseRef := base
	if _, err := runGitIn(p.Path, "rev-parse", "--verify", base); err != nil {
		baseRef = "origin/" + base
	}

	branch := "maia/" + req.Slug
	out, err := runGitIn(p.Path, "rev-list", "--count", baseRef+".."+branch)
	if err != nil || strings.TrimSpace(string(out)) == "0" {
		writeErr(w, http.StatusBadRequest,
			fmt.Sprintf("no commits on %s yet vs %s — run Commit first", branch, base))
		return
	}

	repo, ok := p.RepoSlug()
	if !ok {
		writeErr(w, http.StatusBadRequest, "cannot determine GitHub repo — configure it in project settings")
		return
	}
	client := github.New()
	if auth := client.Auth(); !auth.OK {
		writeErr(w, http.StatusBadRequest, auth.Error)
		return
	}

	// Defaults from the CR's change.md.
	title := strings.TrimSpace(req.Title)
	body := strings.TrimSpace(req.Body)
	issueNum, crDesc := crMetadata(wtDir)
	if title == "" {
		if crDesc != "" {
			title = "maia: " + crDesc
		} else {
			title = "maia: " + req.Slug
		}
	}
	if body == "" {
		body = "Draft pull request created by maia."
		if issueNum > 0 {
			body += fmt.Sprintf("\n\nCloses #%d", issueNum)
		}
	}

	pr, err := client.CreateDraftPR(repo, title, body, branch, base)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "create draft PR: "+err.Error())
		return
	}
	if issueNum > 0 || pr.Number > 0 {
		recordPR(wtDir, pr.HTMLURL)
	}
	writeJSON(w, http.StatusCreated, map[string]any{"pr": pr})
}

// crMetadata reads issue number + description from change.md frontmatter.
func crMetadata(wtDir string) (issue int, description string) {
	data, err := os.ReadFile(filepath.Join(wtDir, state.StateDir, "change.md"))
	if err != nil {
		return 0, ""
	}
	content := string(data)
	if n := frontmatterField(content, "issue"); n != "" {
		fmt.Sscanf(n, "%d", &issue)
	}
	return issue, frontmatterField(content, "description")
}

// recordPR stores `pr: <url>` in change.md frontmatter (best-effort).
func recordPR(wtDir, url string) {
	path := filepath.Join(wtDir, state.StateDir, "change.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) < 3 || lines[0] != "---" {
		return
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "---") {
			end = i
			break
		}
	}
	if end < 0 {
		return
	}
	found := false
	for i := 1; i < end; i++ {
		if strings.HasPrefix(lines[i], "pr:") {
			lines[i] = "pr: " + url
			found = true
		}
	}
	if !found {
		fm := make([]string, 0, len(lines)+1)
		fm = append(fm, lines[:end]...)
		fm = append(fm, "pr: "+url)
		fm = append(fm, lines[end:]...)
		lines = fm
	}
	_ = os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
}
