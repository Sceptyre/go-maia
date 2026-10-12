package web

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sceptyre/maia/internal/state"
)

func TestDiffEndpointHidesMaiaStateFromUntracked(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := initTestRepo(t)
	store := &RegistryStore{path: filepath.Join(home, "web.json"), reg: defaultRegistry()}
	project, _, err := store.AddProject(repo)
	if err != nil {
		t.Fatal(err)
	}

	wt := filepath.Join(state.RepoWorktreeDirFor(repo), "fix-things")
	if err := os.MkdirAll(filepath.Dir(wt), 0755); err != nil {
		t.Fatal(err)
	}
	gitTest(t, repo, "worktree", "add", "-b", "maia/fix-things", wt, "main")
	if err := os.MkdirAll(filepath.Join(wt, state.StateDir, ".generated"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, state.StateDir, "change.md"), []byte("---\nname: fix-things\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "feature.txt"), []byte("x\n"), 0644); err != nil {
		t.Fatal(err)
	}

	server := &Server{Jobs: NewJobManager(), registry: store}
	req := httptest.NewRequest("GET", "/api/projects/"+project.ID+"/worktrees/fix-things/diff", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != 200 {
		t.Fatalf("diff status %d: %s", res.Code, res.Body.String())
	}
	body := res.Body.String()
	if !strings.Contains(body, "feature.txt") {
		t.Fatalf("regular untracked file missing from diff: %s", body)
	}
	if strings.Contains(body, ".maia") {
		t.Fatalf(".maia/ leaked into diff: %s", body)
	}
}
