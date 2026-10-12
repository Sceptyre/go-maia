package web

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sceptyre/maia/internal/state"
)

func TestResolveSourceBranchAndList(t *testing.T) {
	repo := initTestRepo(t)
	gitTest(t, repo, "branch", "feature/child")
	gitTest(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD")
	gitTest(t, repo, "update-ref", "refs/remotes/origin/release", "HEAD")
	gitTest(t, repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")

	branches, defaultBranch, err := listProjectBranches(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if defaultBranch != "origin/main" {
		t.Fatalf("default branch = %q, want origin/main", defaultBranch)
	}
	joined := strings.Join(branches, ",")
	for _, branch := range []string{"main", "feature/child", "origin/main", "origin/release"} {
		if !strings.Contains(joined, branch) {
			t.Errorf("branch list %q missing %q", joined, branch)
		}
	}
	if got, err := resolveSourceBranch(repo, "feature/child"); err != nil || got != "feature/child" {
		t.Fatalf("resolve local branch = %q, %v", got, err)
	}
	if got, err := resolveSourceBranch(repo, "release"); err != nil || got != "origin/release" {
		t.Fatalf("resolve remote branch = %q, %v", got, err)
	}
	if _, err := resolveSourceBranch(repo, "missing"); err == nil {
		t.Fatal("expected missing branch to fail")
	}
}

func TestCloneRemote(t *testing.T) {
	source := initTestRepo(t)
	bare := filepath.Join(t.TempDir(), "repo.git")
	gitTest(t, filepath.Dir(source), "clone", "--bare", source, bare)
	dest := filepath.Join(t.TempDir(), "cloned")
	if err := cloneRemote("file://"+bare, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := runGitIn(dest, "rev-parse", "--show-toplevel"); err != nil {
		t.Fatalf("clone is not a git repository: %v", err)
	}
}

func TestProjectBranchesEndpoint(t *testing.T) {
	repo := initTestRepo(t)
	gitTest(t, repo, "branch", "feature/child")
	store := &RegistryStore{path: filepath.Join(t.TempDir(), "web.json"), reg: defaultRegistry()}
	project, _, err := store.AddProject(repo)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{Jobs: NewJobManager(), registry: store}
	req := httptest.NewRequest("GET", "/api/projects/"+project.ID+"/branches", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != 200 || !strings.Contains(res.Body.String(), "feature/child") {
		t.Fatalf("branches response: status %d body %s", res.Code, res.Body.String())
	}
}

func TestChangeRequestListIncludesUnmaterializedRecord(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := initTestRepo(t)
	store := &RegistryStore{path: filepath.Join(home, "web.json"), reg: defaultRegistry()}
	project, _, err := store.AddProject(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.SaveChangeRequest(state.ChangeRequest{
		ProjectPath: repo, Slug: "waiting", Branch: "maia/waiting",
		Description: "waiting", SourceBranch: "main", Status: "new",
		Change: "---\nname: waiting\nstatus: new\n---\n",
	}); err != nil {
		t.Fatal(err)
	}
	server := &Server{Jobs: NewJobManager(), registry: store}
	req := httptest.NewRequest("GET", "/api/projects/"+project.ID+"/worktrees", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"slug":"waiting"`) || !strings.Contains(res.Body.String(), `"materialized":false`) {
		t.Fatalf("change request response: status %d body %s", res.Code, res.Body.String())
	}
}

func TestCreateChangeRequestPersistsWithoutWorktree(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := initTestRepo(t)
	store := &RegistryStore{path: filepath.Join(home, "web.json"), reg: defaultRegistry()}
	project, _, err := store.AddProject(repo)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{Jobs: NewJobManager(), registry: store}
	body := strings.NewReader(`{"description":"saved request","base":"main"}`)
	req := httptest.NewRequest("POST", "/api/projects/"+project.ID+"/change-requests", body)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != 201 {
		t.Fatalf("create status %d: %s", res.Code, res.Body.String())
	}
	cr, ok, err := state.GetChangeRequest(repo, "saved-request")
	if err != nil || !ok || cr.Worktree != nil || cr.SourceBranch != "main" {
		t.Fatalf("persisted change request: %+v ok=%v err=%v", cr, ok, err)
	}

	path := "/api/projects/" + project.ID + "/worktrees/saved-request/doc/change"
	getReq := httptest.NewRequest("GET", path, nil)
	getRes := httptest.NewRecorder()
	server.Handler().ServeHTTP(getRes, getReq)
	if getRes.Code != 200 || !strings.Contains(getRes.Body.String(), "saved request") {
		t.Fatalf("detached change doc GET: status %d body %s", getRes.Code, getRes.Body.String())
	}
	putReq := httptest.NewRequest("PUT", path, strings.NewReader(`{"raw":"---\nname: saved-request\ndescription: edited while detached\nstatus: new\nsource_branch: main\n---\n\nupdated\n"}`))
	putRes := httptest.NewRecorder()
	server.Handler().ServeHTTP(putRes, putReq)
	if putRes.Code != 200 {
		t.Fatalf("detached change doc PUT: status %d body %s", putRes.Code, putRes.Body.String())
	}
	cr, _, _ = state.GetChangeRequest(repo, "saved-request")
	if !strings.Contains(cr.Change, "edited while detached") || !strings.Contains(cr.Change, "updated") {
		t.Fatalf("detached edit not persisted: %q", cr.Change)
	}

}

func TestValidateRemoteURL(t *testing.T) {
	for _, valid := range []string{"https://github.com/owner/repo.git", "ssh://git@example.com/owner/repo.git", "git@github.com:owner/repo.git"} {
		if _, err := validateRemoteURL(valid); err != nil {
			t.Errorf("validateRemoteURL(%q): %v", valid, err)
		}
	}
	for _, invalid := range []string{"", "-upload-pack=bad", "file:///tmp/repo", "https://user:password@example.com/repo.git", "https://example.com/a b"} {
		if _, err := validateRemoteURL(invalid); err == nil {
			t.Errorf("validateRemoteURL(%q) unexpectedly succeeded", invalid)
		}
	}
}

func TestSetFrontmatterField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "change.md")
	if err := os.WriteFile(path, []byte("---\nname: first\n---\nbody\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := setFrontmatterField(path, "source_branch", "origin/feature"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := frontmatterField(string(data), "source_branch"); got != "origin/feature" {
		t.Fatalf("source_branch = %q", got)
	}
}

func initTestRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	gitTest(t, repo, "init", "-b", "main")
	gitTest(t, repo, "config", "user.email", "test@example.com")
	gitTest(t, repo, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	gitTest(t, repo, "add", "README")
	gitTest(t, repo, "commit", "-m", "initial")
	return repo
}

func gitTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
}
