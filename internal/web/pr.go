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

	changes, err := state.SyncChangeRequests(p.Path)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	var cr *state.ChangeRequest
	for i := range changes {
		if changes[i].Slug == req.Slug {
			cr = &changes[i]
			break
		}
	}
	if cr == nil {
		writeErr(w, http.StatusNotFound, "change request not found")
		return
	}
	if cr.RebaseConflict {
		writeErr(w, http.StatusConflict, "resolve or abort the source rebase before creating a PR")
		return
	}

	base := cr.SourceBranch
	if base == "" {
		base = projectDefaultBranch(p.Path, p.BaseBranch)
	}
	base = strings.TrimPrefix(base, "origin/")
	if base == "" {
		base = "main"
	}
	baseRef := base
	if _, err := runGitIn(p.Path, "rev-parse", "--verify", base); err != nil {
		baseRef = "origin/" + base
	}

	branch := cr.Branch
	if branch == "" {
		branch = "maia/" + req.Slug
	}
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
	issueNum, crDesc := cr.Issue, cr.Description
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
	if err := state.UpdateChangePR(p.Path, cr.Slug, pr.HTMLURL); err != nil {
		writeErr(w, http.StatusInternalServerError, "draft PR was created but state update failed: "+err.Error())
		return
	}
	if cr.Worktree != nil {
		recordPR(filepath.Join(cr.Worktree.Path, state.StateDir, "change.md"), pr.HTMLURL)
	}
	writeJSON(w, http.StatusCreated, map[string]any{"pr": pr})
}

// recordPR stores `pr: <url>` in a materialized change.md file (best-effort).
func recordPR(path, url string) {
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
