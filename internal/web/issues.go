package web

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
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
	ImportedSlug   string `json:"imported_slug,omitempty"`
	ImportedStatus string `json:"imported_status,omitempty"`
}

// issuesResponse is the payload for the issues screen.
type issuesResponse struct {
	Auth     github.Auth `json:"auth"`
	Repo     string      `json:"repo,omitempty"`
	LastScan string      `json:"last_scan,omitempty"`
	Issues   []issueView `json:"issues"`
	Queued   int         `json:"queued,omitempty"` // set by scan when auto-import queued imports
}

// importedIssues maps GitHub issue number → CR slug via change.md frontmatter
// (the dedup record for imports; survives restarts, no side database).
func importedIssues(repoRoot string) map[int]string {
	out := map[int]string{}
	slugs, err := state.ListWorktreesIn(repoRoot)
	if err != nil {
		return out
	}
	for _, slug := range slugs {
		path := filepath.Join(state.RepoWorktreeDirFor(repoRoot), slug, state.StateDir, "change.md")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if n := frontmatterField(string(data), "issue"); n != "" {
			if num, err := strconv.Atoi(n); err == nil {
				out[num] = slug
			}
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
	imported := importedIssues(p.Path)
	statuses := map[string]string{}
	for _, slug := range imported {
		if info, err := readWorktreeInfo(p.Path, slug); err == nil {
			statuses[slug] = info.Status
		}
	}
	for _, is := range issues {
		v := issueView{Issue: is}
		if slug, ok := imported[is.Number]; ok {
			v.ImportedSlug = slug
			v.ImportedStatus = statuses[slug]
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
		resp.Queued = s.queueImports(p, issues, imported)
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
	if err := s.checkCapacity(w, p); err != nil {
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
	if slug, ok := importedIssues(p.Path)[num]; ok {
		writeErr(w, http.StatusConflict, fmt.Sprintf("issue #%d already imported as %s", num, slug))
		return
	}
	job, err := s.startImportJob(p, target)
	if err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job.summary())
}

// queueImports materializes up to the worktree cap, chaining on the global
// single-flight queue. Returns how many were queued.
func (s *Server) queueImports(p *Project, issues []github.Issue, imported map[int]string) int {
	sort.Slice(issues, func(i, j int) bool { return issues[i].Number < issues[j].Number })
	var pending []github.Issue
	for _, is := range issues {
		if _, done := imported[is.Number]; !done {
			pending = append(pending, is)
		}
	}
	if len(pending) == 0 {
		return 0
	}
	room := s.capacityRoom(p)
	if room <= 0 {
		return 0
	}
	if len(pending) > room {
		pending = pending[:room]
	}
	go func() {
		for _, is := range pending {
			for {
				if s.capacityRoom(p) <= 0 {
					return
				}
				// Re-check dedup: an earlier queued import may have landed.
				if _, done := importedIssues(p.Path)[is.Number]; done {
					break
				}
				job, err := s.startImportJob(p, &is)
				if err == nil {
					<-job.Done()
					break
				}
				if strings.Contains(err.Error(), "already running") {
					time.Sleep(1500 * time.Millisecond)
					continue
				}
				return // capacity or validation error: stop this round
			}
		}
	}()
	return len(pending)
}

// capacityRoom returns how many more CRs may be materialized (0 = unlimited
// when max_active_worktrees <= 0).
func (s *Server) capacityRoom(p *Project) int {
	max := s.registry.Get().Defaults.MaxActiveWorktrees
	if max <= 0 {
		return 1 << 30
	}
	count, _ := projectCRStats(p.Path)
	return max - count
}

// startImportJob runs `maia new` for an issue, then writes change.md with
// issue metadata in a post-step (runs only on success).
func (s *Server) startImportJob(p *Project, is *github.Issue) (*Job, error) {
	before := map[string]bool{}
	if slugs, err := state.ListWorktreesIn(p.Path); err == nil {
		for _, sl := range slugs {
			before[sl] = true
		}
	}
	desc := fmt.Sprintf("%s #%d", is.Title, is.Number)
	base, err := resolveSourceBranch(p.Path, projectDefaultBranch(p.Path, p.BaseBranch))
	if err != nil {
		return nil, fmt.Errorf("cannot import issue #%d: %w", is.Number, err)
	}

	post := func(j *Job) error {
		actual := ""
		if slugs, err := state.ListWorktreesIn(p.Path); err == nil {
			for _, sl := range slugs {
				if !before[sl] {
					actual = sl
					break
				}
			}
		}
		if actual == "" {
			return fmt.Errorf("worktree for issue #%d not found after maia new", is.Number)
		}
		path := filepath.Join(state.RepoWorktreeDirFor(p.Path), actual, state.StateDir, "change.md")
		if err := os.WriteFile(path, []byte(issueChangeMD(is, actual, base)), 0644); err != nil {
			return err
		}
		j.append("line", fmt.Sprintf("✓ change.md created for issue #%d → %s", is.Number, actual))
		return nil
	}

	return s.Jobs.Start(JobStart{
		ProjectID: p.ID,
		Slug:      "", // actual slug is discovered by the post-step
		Kind:      "import",
		Command:   fmt.Sprintf("import #%d", is.Number),
		Dir:       p.Path,
		Argv:      []string{"new", desc, "--base", base},
		Post:      post,
	})
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
`, slug, is.Title, sourceBranch, is.Number, is.HTMLURL, body, is.Number)
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
		s.queueImports(p, issues, importedIssues(p.Path))
	}
}
