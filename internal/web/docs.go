package web

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sceptyre/maia/internal/state"
)

// --- Documents ---

var docTypes = map[string]bool{"change": true, "research": true, "plan": true}

func docPath(repoRoot, slug, doc string) (string, error) {
	if !slugRe.MatchString(slug) || !docTypes[doc] {
		return "", fmt.Errorf("invalid slug or document type")
	}
	if doc == "change" {
		return filepath.Join(state.RepoWorktreeDirFor(repoRoot), slug, state.StateDir, "change.md"), nil
	}
	return filepath.Join(state.RepoWorktreeDirFor(repoRoot), slug, state.GeneratedDir, doc+".md"), nil
}

func (s *Server) handleGetDoc(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projectFrom(w, r)
	if !ok {
		return
	}
	doc := r.PathValue("doc")
	path, err := docPath(p.Path, r.PathValue("slug"), doc)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		cr, ok, stateErr := state.GetChangeRequest(p.Path, r.PathValue("slug"))
		if stateErr != nil || !ok {
			writeErr(w, http.StatusNotFound, fmt.Sprintf("%s not found", doc))
			return
		}
		if doc == "change" {
			data = []byte(cr.Change)
		} else {
			data = []byte(cr.Artifacts[doc])
		}
		if len(data) == 0 {
			writeErr(w, http.StatusNotFound, fmt.Sprintf("%s not found", doc))
			return
		}
	}
	raw := string(data)
	html, err := RenderMarkdown(raw)
	if err != nil {
		html = "<pre>" + raw + "</pre>"
	}
	writeJSON(w, http.StatusOK, map[string]string{"raw": raw, "html": html})
}

func (s *Server) handlePutDoc(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projectFrom(w, r)
	if !ok {
		return
	}
	doc := r.PathValue("doc")
	if doc != "change" {
		writeErr(w, http.StatusMethodNotAllowed, "only change.md is editable")
		return
	}
	path, err := docPath(p.Path, r.PathValue("slug"), doc)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		cr, ok, stateErr := state.GetChangeRequest(p.Path, r.PathValue("slug"))
		if stateErr != nil || !ok || cr.Worktree != nil {
			writeErr(w, http.StatusNotFound, "change request not found")
			return
		}
	}
	var body struct {
		Raw string `json:"raw"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if _, err := os.Stat(filepath.Dir(path)); err == nil {
		if err := os.WriteFile(path, []byte(body.Raw), 0644); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	slug := r.PathValue("slug")
	if err := state.UpdateChangeDocument(p.Path, slug, body.Raw); err != nil {
		if _, syncErr := state.SyncChangeRequests(p.Path); syncErr != nil {
			writeErr(w, http.StatusInternalServerError, syncErr.Error())
			return
		}
		if err := state.UpdateChangeDocument(p.Path, slug, body.Raw); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := state.UpdateChangeDefaultBase(p.Path, slug, isProjectDefaultBranch(p.Path, p.BaseBranch, frontmatterField(body.Raw, "source_branch"))); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Diff ---

func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projectFrom(w, r)
	if !ok {
		return
	}
	slug := r.PathValue("slug")
	if !slugRe.MatchString(slug) {
		writeErr(w, http.StatusBadRequest, "invalid slug")
		return
	}
	dir := filepath.Join(state.RepoWorktreeDirFor(p.Path), slug)
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
				name := strings.TrimSpace(line[2:])
				if name == state.StateDir || strings.HasPrefix(name, state.StateDir+"/") {
					continue // maia's own state is never part of the change
				}
				untracked = append(untracked, name)
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"unstaged":  git("diff"),
		"staged":    git("diff", "--cached"),
		"untracked": untracked,
	})
}
