package web

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// GithubSettings is the per-project GitHub integration config. The zero/empty
// values fall back to Registry.Defaults.Github.
type GithubSettings struct {
	Enabled         bool     `json:"enabled"`
	Mode            string   `json:"mode"`              // "origin" | "custom" | "none"
	Repo            string   `json:"repo,omitempty"`    // "owner/repo" when mode == "custom"
	Labels          []string `json:"labels"`            // only issues with these labels are detected
	ImportMode      string   `json:"import_mode"`       // "auto" (materialize up to cap) | "manual"
	PollIntervalMin int      `json:"poll_interval_min"` // 0 = default
	LastScan        string   `json:"last_scan,omitempty"`
}

// Project is one supported repository.
type Project struct {
	ID         string         `json:"id"`
	Path       string         `json:"path"`
	Name       string         `json:"name"`
	BaseBranch string         `json:"base_branch,omitempty"`
	AddedAt    time.Time      `json:"added_at"`
	LastOpened time.Time      `json:"last_opened,omitempty"`
	Github     GithubSettings `json:"github"`
}

// Defaults are project-level defaults applied to newly added projects.
type Defaults struct {
	MaxActiveWorktrees int            `json:"max_active_worktrees"`
	Github             GithubSettings `json:"github"`
}

// Registry is the master web config (~/.maia/web.json).
type Registry struct {
	Version  int        `json:"version"`
	UI       UISettings `json:"ui"`
	Defaults Defaults   `json:"defaults"`
	Projects []Project  `json:"projects"`
}

type UISettings struct {
	Theme string `json:"theme"` // "dark" | "light" | "system"
}

const registryVersion = 1

func registryPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".maia", "web.json")
}

func defaultRegistry() *Registry {
	return &Registry{
		Version: registryVersion,
		UI:      UISettings{Theme: "dark"},
		Defaults: Defaults{
			MaxActiveWorktrees: 10,
			Github: GithubSettings{
				Enabled:         true,
				Mode:            "origin",
				Labels:          []string{"maia"},
				ImportMode:      "auto",
				PollIntervalMin: 15,
			},
		},
		Projects: []Project{},
	}
}

// RegistryStore serializes access to web.json.
type RegistryStore struct {
	mu   sync.Mutex
	path string
	reg  *Registry
}

func NewRegistryStore() *RegistryStore {
	s := &RegistryStore{path: registryPath()}
	data, err := os.ReadFile(s.path)
	if err != nil {
		s.reg = defaultRegistry()
		return s
	}
	s.reg = defaultRegistry()
	if err := json.Unmarshal(data, s.reg); err != nil {
		// Corrupt registry: keep defaults but do not clobber the file until a save.
		s.reg = defaultRegistry()
		return s
	}
	s.reg.Version = registryVersion
	return s
}

func (s *RegistryStore) saveLocked() error {
	data, err := json.MarshalIndent(s.reg, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Get returns the registry for reading. Callers must not mutate the result.
func (s *RegistryStore) Get() *Registry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reg
}

// Update applies fn under the lock and persists the registry.
func (s *RegistryStore) Update(fn func(*Registry) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := fn(s.reg); err != nil {
		return err
	}
	return s.saveLocked()
}

// projectID derives a stable id from the absolute repo path.
func projectID(path string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(path)))
	return hex.EncodeToString(sum[:6])
}

// Find returns the project with the given id.
func (r *Registry) Find(id string) (*Project, bool) {
	for i := range r.Projects {
		if r.Projects[i].ID == id {
			return &r.Projects[i], true
		}
	}
	return nil, false
}

// AddProject registers a repository path (idempotent — returns existing).
func (s *RegistryStore) AddProject(path string) (*Project, bool, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, false, err
	}
	abs = filepath.Clean(abs)

	var existing *Project
	found := false
	err = s.Update(func(r *Registry) error {
		for i := range r.Projects {
			if r.Projects[i].Path == abs {
				existing = &r.Projects[i]
				found = true
				return nil
			}
		}
		p := Project{
			ID:      projectID(abs),
			Path:    abs,
			Name:    filepath.Base(abs),
			AddedAt: time.Now().UTC(),
			Github:  r.Defaults.Github,
		}
		r.Projects = append(r.Projects, p)
		sort.Slice(r.Projects, func(i, j int) bool { return r.Projects[i].Name < r.Projects[j].Name })
		for i := range r.Projects {
			if r.Projects[i].ID == p.ID {
				existing = &r.Projects[i]
			}
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return existing, found, nil
}

// RemoveProject unregisters a project (worktrees are left untouched).
func (s *RegistryStore) RemoveProject(id string) error {
	return s.Update(func(r *Registry) error {
		for i, p := range r.Projects {
			if p.ID == id {
				r.Projects = append(r.Projects[:i], r.Projects[i+1:]...)
				return nil
			}
		}
		return fmt.Errorf("project not found: %s", id)
	})
}

// TouchOpened bumps last_opened at most once a minute.
func (s *RegistryStore) TouchOpened(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.reg.Find(id)
	if !ok {
		return
	}
	if time.Since(p.LastOpened) < time.Minute {
		return
	}
	p.LastOpened = time.Now().UTC()
	_ = s.saveLocked()
}

// UpdateGithub merges a partial github config into a project.
func (s *RegistryStore) UpdateGithub(id string, in GithubSettings, setLabels bool) error {
	return s.Update(func(r *Registry) error {
		p, ok := r.Find(id)
		if !ok {
			return fmt.Errorf("project not found: %s", id)
		}
		p.Github.Enabled = in.Enabled
		if in.Mode != "" {
			p.Github.Mode = in.Mode
		}
		if in.Repo != "" {
			p.Github.Repo = in.Repo
		}
		if setLabels {
			p.Github.Labels = in.Labels
		}
		if in.ImportMode == "auto" || in.ImportMode == "manual" {
			p.Github.ImportMode = in.ImportMode
		}
		if in.PollIntervalMin > 0 {
			p.Github.PollIntervalMin = in.PollIntervalMin
		}
		return nil
	})
}

// RepoSlug resolves the "owner/repo" for a project from its GitHub settings
// or, in "origin" mode, from the git remote.
func (p *Project) RepoSlug() (string, bool) {
	switch p.Github.Mode {
	case "custom":
		s := strings.TrimSpace(p.Github.Repo)
		if strings.Count(s, "/") == 1 {
			return s, true
		}
	case "origin":
		if out, err := runGitIn(p.Path, "remote", "get-url", "origin"); err == nil {
			if slug := parseRemoteSlug(strings.TrimSpace(string(out))); slug != "" {
				return slug, true
			}
		}
	}
	return "", false
}

// runGitIn executes git with dir as working directory.
func runGitIn(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd.Output()
}

// parseRemoteSlug extracts "owner/repo" from an origin URL (https or ssh).
func parseRemoteSlug(url string) string {
	url = strings.TrimSuffix(url, ".git")
	if i := strings.Index(url, "github.com:"); i >= 0 {
		return url[i+len("github.com:"):]
	}
	if i := strings.Index(url, "github.com/"); i >= 0 {
		return url[i+len("github.com/"):]
	}
	if strings.HasPrefix(url, "git@") {
		if parts := strings.SplitN(url, ":", 2); len(parts) == 2 && strings.Count(parts[1], "/") == 1 {
			return parts[1]
		}
	}
	if strings.Count(url, "/") >= 2 && strings.Contains(url, "://") {
		parts := strings.SplitN(url, "://", 2)
		seg := strings.Split(parts[1], "/")
		if len(seg) >= 3 {
			return strings.Join(seg[len(seg)-2:], "/")
		}
	}
	return ""
}
