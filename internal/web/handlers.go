package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sceptyre/maia/internal/state"
)

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

type apiError struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiError{Error: msg})
}

func decodeBody(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

// --- State ---

type configInfo struct {
	Model     string `json:"model"`
	BaseURL   string `json:"base_url"`
	HasAPIKey bool   `json:"has_api_key"`
	HasBrave  bool   `json:"has_brave_key"`
	MaxTokens int    `json:"max_tokens,omitempty"`
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	running, hasRunning := s.Jobs.Running()
	writeJSON(w, http.StatusOK, map[string]any{
		"repo": map[string]string{
			"name": s.RepoName,
			"root": s.RepoRoot,
		},
		"worktrees_dir": s.worktreesDir,
		"config":        s.config,
		"running_job":   jobOrNil(hasRunning, running),
	})
}

func jobOrNil(has bool, j JobSummary) any {
	if !has {
		return nil
	}
	return j
}

// --- Worktrees ---

type worktreeInfo struct {
	Slug        string `json:"slug"`
	Path        string `json:"path"`
	Status      string `json:"status"`
	Description string `json:"description,omitempty"`
	HasMaia     bool   `json:"has_maia"`
	HasChange   bool   `json:"has_change"`
	HasResearch bool   `json:"has_research"`
	HasPlan     bool   `json:"has_plan"`
	Branch      string `json:"branch"`
}

func (s *Server) worktreeInfo(slug string) (worktreeInfo, error) {
	if !slugRe.MatchString(slug) {
		return worktreeInfo{}, fmt.Errorf("invalid slug: %q", slug)
	}
	path := filepath.Join(s.worktreesDir, slug)
	info := worktreeInfo{
		Slug:    slug,
		Path:    path,
		Branch:  "maia/" + slug,
		HasMaia: state.HasWorktreeIn(path),
	}
	if !info.HasMaia {
		return info, nil
	}
	info.HasChange = state.HasChangeFileIn(path)
	info.HasResearch = state.HasResearchIn(path)
	info.HasPlan = state.HasPlanIn(path)
	if status, err := state.GetStatusIn(path); err == nil {
		info.Status = status
	} else {
		info.Status = "new"
	}
	if info.HasChange {
		if data, err := os.ReadFile(filepath.Join(path, state.StateDir, "change.md")); err == nil {
			info.Description = frontmatterField(string(data), "description")
		}
	}
	return info, nil
}

func (s *Server) handleListWorktrees(w http.ResponseWriter, r *http.Request) {
	slugs, err := state.ListWorktrees()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]worktreeInfo, 0, len(slugs))
	for _, slug := range slugs {
		info, err := s.worktreeInfo(slug)
		if err != nil {
			continue
		}
		out = append(out, info)
	}
	writeJSON(w, http.StatusOK, out)
}

// --- Documents ---

var docTypes = map[string]bool{"change": true, "research": true, "plan": true}

func (s *Server) docPath(slug, doc string) (string, error) {
	if !slugRe.MatchString(slug) || !docTypes[doc] {
		return "", fmt.Errorf("invalid slug or document type")
	}
	if doc == "change" {
		return filepath.Join(s.worktreesDir, slug, state.StateDir, "change.md"), nil
	}
	return filepath.Join(s.worktreesDir, slug, state.GeneratedDir, doc+".md"), nil
}

func (s *Server) handleGetDoc(w http.ResponseWriter, r *http.Request) {
	path, err := s.docPath(r.PathValue("slug"), r.PathValue("doc"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		writeErr(w, http.StatusNotFound, fmt.Sprintf("%s not found", r.PathValue("doc")))
		return
	}
	raw := string(data)
	html, err := RenderMarkdown(raw)
	if err != nil {
		html = "<pre>" + raw + "</pre>"
	}
	writeJSON(w, http.StatusOK, map[string]string{"raw": raw, "html": html})
}

func (s *Server) handlePutDoc(w http.ResponseWriter, r *http.Request) {
	doc := r.PathValue("doc")
	if doc != "change" {
		writeErr(w, http.StatusMethodNotAllowed, "only change.md is editable")
		return
	}
	path, err := s.docPath(r.PathValue("slug"), doc)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		writeErr(w, http.StatusNotFound, "worktree not found")
		return
	}
	var body struct {
		Raw string `json:"raw"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := os.WriteFile(path, []byte(body.Raw), 0644); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Diff ---

func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if !slugRe.MatchString(slug) {
		writeErr(w, http.StatusBadRequest, "invalid slug")
		return
	}
	dir := filepath.Join(s.worktreesDir, slug)
	if !state.HasWorktreeIn(dir) {
		writeErr(w, http.StatusNotFound, "worktree not found")
		return
	}

	git := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.Output()
		if err != nil {
			return ""
		}
		return string(out)
	}

	untracked := []string{}
	if porcelain := git("status", "--porcelain"); porcelain != "" {
		for _, line := range strings.Split(strings.TrimRight(porcelain, "\n"), "\n") {
			if strings.HasPrefix(line, "??") {
				untracked = append(untracked, strings.TrimSpace(line[2:]))
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"unstaged":  git("diff"),
		"staged":    git("diff", "--cached"),
		"untracked": untracked,
	})
}

// --- Jobs ---

type runRequest struct {
	Cmd         string `json:"cmd"`
	Slug        string `json:"slug"`
	Feedback    string `json:"feedback,omitempty"`
	DryRun      bool   `json:"dry_run,omitempty"`
	Phase       int    `json:"phase,omitempty"`
	Message     string `json:"message,omitempty"`
	Description string `json:"description,omitempty"`
	Base        string `json:"base,omitempty"`
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	var req runRequest
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	var slug, command string
	var argv []string

	switch req.Cmd {
	case "init", "plan", "merge", "cleanup":
		if !slugRe.MatchString(req.Slug) {
			writeErr(w, http.StatusBadRequest, "invalid or missing slug")
			return
		}
		slug, command = req.Slug, req.Cmd
		argv = []string{req.Cmd, req.Slug}

	case "steer":
		if !slugRe.MatchString(req.Slug) || strings.TrimSpace(req.Feedback) == "" {
			writeErr(w, http.StatusBadRequest, "slug and feedback are required")
			return
		}
		slug, command = req.Slug, "steer"
		argv = []string{"steer", req.Feedback, req.Slug}

	case "apply":
		if !slugRe.MatchString(req.Slug) {
			writeErr(w, http.StatusBadRequest, "invalid or missing slug")
			return
		}
		slug, command = req.Slug, "apply"
		argv = []string{"apply", req.Slug}
		if req.DryRun {
			argv = append(argv, "--dry-run")
		}
		if req.Phase > 0 {
			argv = append(argv, "--phase", fmt.Sprintf("%d", req.Phase))
		}

	case "commit":
		if !slugRe.MatchString(req.Slug) || strings.TrimSpace(req.Message) == "" {
			writeErr(w, http.StatusBadRequest, "slug and message are required")
			return
		}
		slug, command = req.Slug, "commit"
		argv = []string{"commit", req.Slug, req.Message}

	case "new":
		desc := strings.TrimSpace(req.Description)
		if desc == "" {
			writeErr(w, http.StatusBadRequest, "description is required")
			return
		}
		slug = generateSlug(desc)
		if !slugRe.MatchString(slug) {
			writeErr(w, http.StatusBadRequest, "description must contain letters or digits")
			return
		}
		if _, err := os.Stat(filepath.Join(s.worktreesDir, slug)); err == nil {
			writeErr(w, http.StatusConflict, fmt.Sprintf("worktree already exists: %s", slug))
			return
		}
		command = "new"
		argv = []string{"new", desc}
		if req.Base != "" {
			argv = append(argv, "--base", req.Base)
		}

	default:
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("unknown command: %q", req.Cmd))
		return
	}

	job, err := s.Jobs.Start(slug, req.Cmd, command, argv)
	if err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job.summary())
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Jobs.List())
}

func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	if err := s.Jobs.Cancel(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func generateSlug(desc string) string {
	slug := strings.ToLower(strings.TrimSpace(desc))
	slug = strings.ReplaceAll(slug, " ", "-")
	slug = strings.ReplaceAll(slug, "_", "-")
	var out []byte
	for _, c := range slug {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			out = append(out, byte(c))
		}
	}
	return strings.Trim(string(out), "-")
}

// frontmatterField extracts a top-level `key: value` from YAML frontmatter.
func frontmatterField(content, field string) string {
	if !strings.HasPrefix(content, "---") {
		return ""
	}
	for _, line := range strings.Split(content, "\n")[1:] {
		if strings.HasPrefix(line, "---") {
			break
		}
		if parts := strings.SplitN(line, ":", 2); len(parts) == 2 && strings.TrimSpace(parts[0]) == field {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}
