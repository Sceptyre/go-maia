package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sceptyre/maia/internal/config"
)

//go:embed static
var staticFiles embed.FS

// Server serves the maia web UI and its JSON/SSE API. It is not bound to a
// single repository: projects live in the registry and are addressed by id.
type Server struct {
	Jobs     *JobManager
	Token    string
	Addr     string
	Port     int
	Version  string
	registry *RegistryStore

	cfgMu  sync.RWMutex
	config configInfo
}

// New builds the server. token != "" requires auth on API requests (query
// param `token` or `Authorization: Bearer`). The repo the process runs in is
// auto-registered as a project (if any).
func New(token, addr string, port int) *Server {
	s := &Server{
		Jobs:     NewJobManager(),
		Token:    token,
		Addr:     addr,
		Port:     port,
		Version:  "2.0.0-dev",
		registry: NewRegistryStore(),
	}
	s.config = snapshotConfig()
	s.startPoller()
	if cwd, err := os.Getwd(); err == nil {
		if out, err := runGitIn(cwd, "rev-parse", "--show-toplevel"); err == nil {
			if root := strings.TrimSpace(string(out)); root != "" {
				s.registry.AddProject(root)
			}
		}
	}
	return s
}

// snapshotConfig resolves effective config (subprocess-backed secrets may run
// commands, so callers should not invoke it per-request).
func snapshotConfig() configInfo {
	cfg, _ := config.Load()
	if cfg == nil {
		cfg = &config.Config{}
	}
	return configInfo{
		Model:     config.Get("model"),
		BaseURL:   config.Get("openai_base_url"),
		MaxTokens: cfg.MaxTokens,
		HasAPIKey: config.Get("openai_api_key") != "",
		HasBrave:  config.Get("brave_api_key") != "",
	}
}

func (s *Server) currentConfig() configInfo {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.config
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Shell bootstrap + admin settings
	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("GET /api/settings", s.handleSettings)
	mux.HandleFunc("PUT /api/settings", s.handleSettings)

	// Projects (the supported repos)
	mux.HandleFunc("GET /api/projects", s.handleListProjects)
	mux.HandleFunc("POST /api/projects", s.handleAddProject)
	mux.HandleFunc("GET /api/projects/{pid}", s.handleGetProject)
	mux.HandleFunc("PUT /api/projects/{pid}", s.handleUpdateProject)
	mux.HandleFunc("DELETE /api/projects/{pid}", s.handleDeleteProject)

	// Project-scoped workflow
	mux.HandleFunc("GET /api/projects/{pid}/worktrees", s.handleListWorktrees)
	mux.HandleFunc("GET /api/projects/{pid}/worktrees/{slug}/doc/{doc}", s.handleGetDoc)
	mux.HandleFunc("PUT /api/projects/{pid}/worktrees/{slug}/doc/{doc}", s.handlePutDoc)
	mux.HandleFunc("GET /api/projects/{pid}/worktrees/{slug}/diff", s.handleDiff)
	mux.HandleFunc("POST /api/projects/{pid}/jobs", s.handleRun)

	// GitHub issues (detected change requests) — implemented in issues.go
	mux.HandleFunc("GET /api/projects/{pid}/issues", s.handleListIssues)
	mux.HandleFunc("POST /api/projects/{pid}/issues/scan", s.handleScanIssues)
	mux.HandleFunc("POST /api/projects/{pid}/issues/{num}/import", s.handleImportIssue)

	// Write-back
	mux.HandleFunc("POST /api/projects/{pid}/pull-requests", s.handleCreatePR)

	// Global job queue
	mux.HandleFunc("GET /api/jobs", s.handleListJobs)
	mux.HandleFunc("GET /api/jobs/{id}/events", s.handleJobEvents)
	mux.HandleFunc("POST /api/jobs/{id}/cancel", s.handleCancelJob)

	// Go sniffs .webmanifest as text/plain; register the PWA type so install
	// prompts work in strict browsers.
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")

	static, _ := fs.Sub(staticFiles, "static")
	mux.Handle("GET /", noCache(http.FileServerFS(static)))

	// API requests require the token; static assets do not — the page itself
	// is not secret, and <script>/<link> tags cannot carry the token. The JS
	// prompts for it on the first 401 instead.
	return s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'")
		mux.ServeHTTP(w, r)
	}))
}

// noCache revalidates every static request — edits to the UI are picked up
// on reload without cache-busting query params.
func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		next.ServeHTTP(w, r)
	})
}

// auth enforces the bearer/query token when one is configured.
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Token != "" && strings.HasPrefix(r.URL.Path, "/api/") {
			got := r.URL.Query().Get("token")
			if got == "" {
				got = r.Header.Get("Authorization")
				got = strings.TrimPrefix(got, "Bearer ")
			}
			if got != s.Token {
				writeErr(w, http.StatusUnauthorized, "missing or invalid token")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// handleJobEvents streams a job's log as Server-Sent Events. Replay is driven
// by `?after=` or the Last-Event-ID header so clients can resume.
func (s *Server) handleJobEvents(w http.ResponseWriter, r *http.Request) {
	job, ok := s.Jobs.Get(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "job not found")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	after := 0
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			after = n
		}
	}
	if v := r.URL.Query().Get("after"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > after {
			after = n
		}
	}

	replay, ch, unsubscribe := job.snapshot(after)
	defer unsubscribe()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	writeEvent := func(id int, data string) bool {
		if id >= 0 {
			fmt.Fprintf(w, "id: %d\ndata: %s\n\n", id, data)
		} else {
			fmt.Fprintf(w, "data: %s\n\n", data)
		}
		flusher.Flush()
		return true
	}

	lastID := after
	for _, ev := range replay {
		if ev.ID > lastID {
			writeEvent(ev.ID, eventJSON(ev))
			lastID = ev.ID
		}
	}

	if job.isDone() {
		b, _ := json.Marshal(JobEvent{Type: "end", Data: job.Status})
		writeEvent(-1, string(b))
		return
	}

	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case ev := <-ch:
			if ev.ID != -1 && ev.ID <= lastID {
				continue
			}
			if ev.ID > 0 {
				lastID = ev.ID
			}
			writeEvent(ev.ID, eventJSON(ev))
			if ev.Type == "end" {
				return
			}
		}
	}
}

func eventJSON(ev JobEvent) string {
	b, _ := json.Marshal(ev)
	return string(b)
}
