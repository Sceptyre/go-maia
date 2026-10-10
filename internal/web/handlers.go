package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

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

// --- State bootstrap ---

type configInfo struct {
	Model     string `json:"model"`
	BaseURL   string `json:"base_url"`
	HasAPIKey bool   `json:"has_api_key"`
	HasBrave  bool   `json:"has_brave_key"`
	MaxTokens int    `json:"max_tokens,omitempty"`
}

// projectSummary is the dashboard card payload.
type projectSummary struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Path       string         `json:"path"`
	BaseBranch string         `json:"base_branch,omitempty"`
	AddedAt    time.Time      `json:"added_at"`
	LastOpened time.Time      `json:"last_opened,omitempty"`
	Github     GithubSettings `json:"github"`
	CRCount    int            `json:"cr_count"`
	Statuses   map[string]int `json:"statuses"`
}

func summarize(p *Project) projectSummary {
	count, statuses := projectCRStats(p.Path)
	return projectSummary{
		ID: p.ID, Name: p.Name, Path: p.Path, BaseBranch: p.BaseBranch,
		AddedAt: p.AddedAt, LastOpened: p.LastOpened, Github: p.Github,
		CRCount: count, Statuses: statuses,
	}
}

// projectCRStats counts a project's change requests by status.
func projectCRStats(repoRoot string) (int, map[string]int) {
	statuses := map[string]int{}
	slugs, err := state.ListWorktreesIn(repoRoot)
	if err != nil {
		return 0, statuses
	}
	for _, slug := range slugs {
		status := "new"
		if info, err := readWorktreeInfo(repoRoot, slug); err == nil && info.Status != "" {
			status = info.Status
		}
		statuses[status]++
	}
	return len(slugs), statuses
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	reg := s.registry.Get()
	projects := make([]projectSummary, 0, len(reg.Projects))
	for i := range reg.Projects {
		projects = append(projects, summarize(&reg.Projects[i]))
	}
	running, hasRunning := s.Jobs.Running()
	writeJSON(w, http.StatusOK, map[string]any{
		"ui":          reg.UI,
		"defaults":    reg.Defaults,
		"config":      s.currentConfig(),
		"projects":    projects,
		"running_job": jobOrNil(hasRunning, running),
		"server": map[string]any{
			"version": s.Version,
			"port":    s.Port,
		},
	})
}

func jobOrNil(has bool, j JobSummary) any {
	if !has {
		return nil
	}
	return j
}

// --- Projects ---

// projectFrom resolves the {pid} route value to a registered project.
func (s *Server) projectFrom(w http.ResponseWriter, r *http.Request) (*Project, bool) {
	p, ok := s.registry.Get().Find(r.PathValue("pid"))
	if !ok {
		writeErr(w, http.StatusNotFound, "project not found")
		return nil, false
	}
	return p, true
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	reg := s.registry.Get()
	out := make([]projectSummary, 0, len(reg.Projects))
	for i := range reg.Projects {
		out = append(out, summarize(&reg.Projects[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleAddProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := decodeBody(r, &req); err != nil || strings.TrimSpace(req.Path) == "" {
		writeErr(w, http.StatusBadRequest, "path is required")
		return
	}
	abs, err := filepath.Abs(strings.TrimSpace(req.Path))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	out, err := runGitIn(abs, "rev-parse", "--show-toplevel")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "not a git repository: "+abs)
		return
	}
	toplevel := strings.TrimSpace(string(out))
	if top, err := filepath.EvalSymlinks(toplevel); err == nil {
		toplevel = top
	}
	p, created, err := s.registry.AddProject(toplevel)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"project": summarize(p), "created": created})
}

func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projectFrom(w, r)
	if !ok {
		return
	}
	s.registry.TouchOpened(p.ID)
	p, _ = s.registry.Get().Find(p.ID)
	writeJSON(w, http.StatusOK, summarize(p))
}

func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projectFrom(w, r)
	if !ok {
		return
	}
	var req struct {
		Name       string          `json:"name"`
		BaseBranch *string         `json:"base_branch"`
		Github     *GithubSettings `json:"github"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Github != nil {
		setLabels := true // client always sends the full labels list
		if err := s.registry.UpdateGithub(p.ID, *req.Github, setLabels); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	err := s.registry.Update(func(reg *Registry) error {
		proj, ok := reg.Find(p.ID)
		if !ok {
			return fmt.Errorf("project not found")
		}
		if n := strings.TrimSpace(req.Name); n != "" {
			proj.Name = n
		}
		if req.BaseBranch != nil {
			proj.BaseBranch = strings.TrimSpace(*req.BaseBranch)
		}
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	p, _ = s.registry.Get().Find(p.ID)
	writeJSON(w, http.StatusOK, summarize(p))
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projectFrom(w, r)
	if !ok {
		return
	}
	if err := s.registry.RemoveProject(p.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Worktrees (change requests) ---

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
	Issue       int    `json:"issue,omitempty"` // linked GitHub issue number
	PR          string `json:"pr,omitempty"`    // draft PR URL once created
}

// worktreeInfo builds CR metadata for slug inside repoRoot's worktree dir.
func readWorktreeInfo(repoRoot, slug string) (worktreeInfo, error) {
	if !slugRe.MatchString(slug) {
		return worktreeInfo{}, fmt.Errorf("invalid slug: %q", slug)
	}
	path := filepath.Join(state.RepoWorktreeDirFor(repoRoot), slug)
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
			content := string(data)
			info.Description = frontmatterField(content, "description")
			info.PR = frontmatterField(content, "pr")
			if n := frontmatterField(content, "issue"); n != "" {
				fmt.Sscanf(n, "%d", &info.Issue)
			}
		}
	}
	return info, nil
}

func (s *Server) handleListWorktrees(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projectFrom(w, r)
	if !ok {
		return
	}
	s.registry.TouchOpened(p.ID)
	slugs, err := state.ListWorktreesIn(p.Path)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]worktreeInfo, 0, len(slugs))
	for _, slug := range slugs {
		info, err := readWorktreeInfo(p.Path, slug)
		if err != nil {
			continue
		}
		out = append(out, info)
	}
	writeJSON(w, http.StatusOK, out)
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
	p, ok := s.projectFrom(w, r)
	if !ok {
		return
	}
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
		if _, err := os.Stat(filepath.Join(state.RepoWorktreeDirFor(p.Path), slug)); err == nil {
			writeErr(w, http.StatusConflict, fmt.Sprintf("worktree already exists: %s", slug))
			return
		}
		if err := s.checkCapacity(w, p); err != nil {
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

	job, err := s.Jobs.Start(JobStart{
		ProjectID: p.ID,
		Slug:      slug,
		Kind:      req.Cmd,
		Command:   command,
		Dir:       p.Path,
		Argv:      argv,
	})
	if err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job.summary())
}

// checkCapacity enforces max_active_worktrees; writes 409 and returns an
// error when the project is at capacity.
func (s *Server) checkCapacity(w http.ResponseWriter, p *Project) error {
	reg := s.registry.Get()
	max := reg.Defaults.MaxActiveWorktrees
	if max <= 0 {
		return nil
	}
	count, _ := projectCRStats(p.Path)
	if count >= max {
		err := fmt.Errorf("at capacity: %d/%d active change requests — cleanup one or raise max_active_worktrees in Settings", count, max)
		writeErr(w, http.StatusConflict, err.Error())
		return err
	}
	return nil
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

// --- Shared helpers ---

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
