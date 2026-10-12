package web

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sceptyre/maia/internal/github"
	"github.com/sceptyre/maia/internal/state"
)

// issueView is a GitHub issue plus its import state.
type issueView struct {
	github.Issue
	ImportedSlug         string `json:"imported_slug,omitempty"`
	ImportedStatus       string `json:"imported_status,omitempty"`
	ImportedMaterialized bool   `json:"imported_materialized,omitempty"`
}

// issuesResponse is the payload for the issues screen.
type issuesResponse struct {
	Auth     github.Auth `json:"auth"`
	Repo     string      `json:"repo,omitempty"`
	LastScan string      `json:"last_scan,omitempty"`
	Issues   []issueView `json:"issues"`
	Queued   int         `json:"queued,omitempty"` // set by scan when auto-import queued imports
}

// importedIssues maps linked GitHub issues to durable CR records, even after cleanup.
func importedIssues(repoRoot string) map[int]string {
	out := map[int]string{}
	changes, err := state.SyncChangeRequests(repoRoot)
	if err != nil {
		return out
	}
	for _, cr := range changes {
		if cr.Issue > 0 {
			out[cr.Issue] = cr.Slug
		}
	}
	return out
}

// ghFor resolves project + repo slug + authenticated client.
func (s *Server) ghFor(w http.ResponseWriter, r *http.Request) (*Project, *github.Client, string, bool) {
	p, ok := s.projectFrom(w, r)
	if !ok {
		return nil, nil, "", false
	}
	if !p.Github.Enabled {
		writeErr(w, http.StatusBadRequest, "GitHub integration is disabled for this project")
		return nil, nil, "", false
	}
	repo, ok := p.RepoSlug()
	if !ok {
		writeErr(w, http.StatusBadRequest, "cannot determine GitHub repo — set mode to \"custom\" and provide owner/repo in project settings")
		return nil, nil, "", false
	}
	return p, github.New(), repo, true
}

// effectiveLabels returns the project's label filter; only an unset (nil)
// value falls back to the registry default — an explicit empty list means
// "all open issues".
func effectiveLabels(p *Project, reg *Registry) []string {
	if p.Github.Labels != nil {
		return p.Github.Labels
	}
	return reg.Defaults.Github.Labels
}

func (s *Server) handleListIssues(w http.ResponseWriter, r *http.Request) {
	p, client, repo, ok := s.ghFor(w, r)
	if !ok {
		return
	}
	reg := s.registry.Get()
	labels := effectiveLabels(p, reg)
	resp := issuesResponse{
		Auth:     client.Auth(),
		Repo:     repo,
		LastScan: p.Github.LastScan,
		Issues:   []issueView{},
	}
	if !resp.Auth.OK {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	issues, err := client.ListOpenIssues(repo, labels)
	if err != nil {
		resp.Auth = github.Auth{Error: err.Error()}
		writeJSON(w, http.StatusOK, resp)
		return
	}
	imported := map[int]string{}
	statuses := map[string]string{}
	materialized := map[string]bool{}
	changes, err := state.SyncChangeRequests(p.Path)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, cr := range changes {
		if cr.Issue > 0 {
			imported[cr.Issue] = cr.Slug
			info := changeRequestInfo(p, cr)
			statuses[cr.Slug] = info.Status
			materialized[cr.Slug] = info.Materialized
		}
	}
	for _, is := range issues {
		v := issueView{Issue: is}
		if slug, ok := imported[is.Number]; ok {
			v.ImportedSlug = slug
			v.ImportedStatus = statuses[slug]
			v.ImportedMaterialized = materialized[slug]
		}
		resp.Issues = append(resp.Issues, v)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleScanIssues(w http.ResponseWriter, r *http.Request) {
	p, client, repo, ok := s.ghFor(w, r)
	if !ok {
		return
	}
	// Store the attempted scan time regardless of outcome so a failing auth
	// does not hammer the API every poll minute.
	s.registry.Update(func(reg *Registry) error {
		if proj, ok := reg.Find(p.ID); ok {
			proj.Github.LastScan = time.Now().UTC().Format(time.RFC3339)
		}
		return nil
	})

	resp := issuesResponse{Auth: client.Auth(), Issues: []issueView{}}
	if !resp.Auth.OK {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	issues, err := client.ListOpenIssues(repo, effectiveLabels(p, s.registry.Get()))
	if err != nil {
		resp.Auth = github.Auth{Error: err.Error()}
		writeJSON(w, http.StatusOK, resp)
		return
	}
	imported := importedIssues(p.Path)
	for _, is := range issues {
		v := issueView{Issue: is}
		if slug, ok := imported[is.Number]; ok {
			v.ImportedSlug = slug
		}
		resp.Issues = append(resp.Issues, v)
	}
	if p.Github.ImportMode == "auto" {
		resp.Queued = s.queueImports(p, issues)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleImportIssue(w http.ResponseWriter, r *http.Request) {
	p, client, repo, ok := s.ghFor(w, r)
	if !ok {
		return
	}
	num, err := strconv.Atoi(r.PathValue("num"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad issue number")
		return
	}
	issues, err := client.ListOpenIssues(repo, effectiveLabels(p, s.registry.Get()))
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	var target *github.Issue
	for i := range issues {
		if issues[i].Number == num {
			target = &issues[i]
			break
		}
	}
	if target == nil {
		writeErr(w, http.StatusNotFound, fmt.Sprintf("issue #%d not found (or missing label %v)", num, effectiveLabels(p, s.registry.Get())))
		return
	}
	slug, created, err := s.ensureIssueChangeRequest(p, target)
	if err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	if !created {
		writeErr(w, http.StatusConflict, fmt.Sprintf("issue #%d already imported as %s", num, slug))
		return
	}
	job, err := s.startMaterializeJob(p, slug, fmt.Sprintf("materialize issue #%d", num))
	if err == errWorktreeCapacity {
		writeJSON(w, http.StatusCreated, map[string]any{"slug": slug, "materialized": false})
		return
	}
	if err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job.summary())
}

// queueImports records every detected issue and materializes worktrees up to the cap.
func (s *Server) queueImports(p *Project, issues []github.Issue) int {
	sort.Slice(issues, func(i, j int) bool { return issues[i].Number < issues[j].Number })
	queued := 0
	for _, is := range issues {
		slug, created, err := s.ensureIssueChangeRequest(p, &is)
		if err != nil {
			continue
		}
		if created {
			queued++
		}
		cr, found, err := state.GetChangeRequest(p.Path, slug)
		if err != nil || !found || cr.Worktree != nil {
			continue
		}
		if _, err := s.startMaterializeJob(p, slug, fmt.Sprintf("materialize issue #%d", is.Number)); err == nil && !created {
			queued++
		}
	}
	return queued
}

func (s *Server) ensureIssueChangeRequest(p *Project, is *github.Issue) (string, bool, error) {
	changes, err := state.SyncChangeRequests(p.Path)
	if err != nil {
		return "", false, err
	}
	for _, cr := range changes {
		if cr.Issue == is.Number {
			return cr.Slug, false, nil
		}
	}
	desc := fmt.Sprintf("%s #%d", is.Title, is.Number)
	slug := generateSlug(desc)
	if existing, found, err := state.GetChangeRequest(p.Path, slug); err != nil {
		return "", false, err
	} else if found {
		if existing.Issue == is.Number {
			return slug, false, nil
		}
		return "", false, fmt.Errorf("change request slug already exists: %s", slug)
	}
	base, err := resolveSourceBranch(p.Path, projectDefaultBranch(p.Path, p.BaseBranch))
	if err != nil {
		return "", false, fmt.Errorf("cannot import issue #%d: %w", is.Number, err)
	}
	contents := issueChangeMD(is, slug, base)
	if err := state.SaveChangeRequest(state.ChangeRequest{
		ProjectPath: p.Path, Slug: slug, Branch: "maia/" + slug,
		Description: is.Title, SourceBranch: base, DefaultBase: isProjectDefaultBranch(p.Path, p.BaseBranch, base),
		Status: "new", Issue: is.Number, Change: contents,
	}); err != nil {
		return "", false, err
	}
	return slug, true, nil
}

func (s *Server) startMaterializeJob(p *Project, slug, command string) (*Job, error) {
	s.worktreeMu.Lock()
	defer s.worktreeMu.Unlock()
	if !s.hasWorktreeRoom(p) {
		return nil, errWorktreeCapacity
	}
	return s.Jobs.Start(JobStart{
		ProjectID: p.ID, Slug: slug, Kind: "materialize", Command: command,
		Dir: p.Path, Argv: []string{"materialize", slug},
	})
}

func (s *Server) hasWorktreeRoom(p *Project) bool {
	max := s.registry.Get().Defaults.MaxActiveWorktrees
	if max <= 0 {
		return true
	}
	_, count, _ := projectCRStats(p.Path)
	count += s.reservedWorktreeCount(p)
	return count < max
}

// issueChangeMD renders the change.md template for a GitHub issue.
func issueChangeMD(is *github.Issue, slug, sourceBranch string) string {
	body := strings.TrimSpace(is.Body)
	if body == "" {
		body = "_No issue description provided._"
	}
	title := strings.ReplaceAll(is.Title, "\n", " ")
	// Quote so titles containing ": " cannot break the YAML frontmatter.
	if strings.Contains(title, ": ") || strings.HasPrefix(title, "#") {
		title = strconv.Quote(title)
	}
	return fmt.Sprintf(`---
name: %s
description: %s
status: new
source: github
source_branch: %s
issue: %d
url: %s
---

## Goal

%s

## Requirements

- Implements #%d (imported from GitHub — keep this reference)
`, slug, title, sourceBranch, is.Number, is.HTMLURL, body, is.Number)
}

// startPoller scans each project for labeled issues on its interval; auto
// mode materializes them up to the cap.
func (s *Server) startPoller() {
	go func() {
		for range time.Minute {
			reg := s.registry.Get()
			for i := range reg.Projects {
				p := reg.Projects[i]
				if !p.Github.Enabled || p.Github.Mode == "none" {
					continue
				}
				interval := p.Github.PollIntervalMin
				if interval <= 0 {
					interval = reg.Defaults.Github.PollIntervalMin
				}
				if interval <= 0 {
					interval = 15
				}
				last, _ := time.Parse(time.RFC3339, p.Github.LastScan)
				if time.Since(last) < time.Duration(interval)*time.Minute {
					continue
				}
				s.scanProject(&p)
			}
		}
	}()
}

// scanProject performs one detection cycle for a project (best-effort).
func (s *Server) scanProject(p *Project) {
	s.registry.Update(func(reg *Registry) error {
		if proj, ok := reg.Find(p.ID); ok {
			proj.Github.LastScan = time.Now().UTC().Format(time.RFC3339)
		}
		return nil
	})
	client := github.New()
	if !client.Auth().OK {
		return
	}
	repo, ok := p.RepoSlug()
	if !ok {
		return
	}
	issues, err := client.ListOpenIssues(repo, effectiveLabels(p, s.registry.Get()))
	if err != nil {
		return
	}
	if p.Github.ImportMode == "auto" {
		s.queueImports(p, issues)
	}
}
