package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
}

func TestEnsureLocalExcludeKeepsMaiaStateOutOfStatus(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "README")
	runGit(t, repo, "commit", "-m", "initial")

	wt := filepath.Join(t.TempDir(), "wt")
	runGit(t, repo, "worktree", "add", "-b", "feature/x", wt, "main")
	if err := EnsureLocalExclude(wt, ".maia"); err != nil {
		t.Fatal(err)
	}
	// second call must not duplicate the pattern
	if err := EnsureLocalExclude(wt, ".maia"); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(wt, ".maia", ".generated"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		filepath.Join(".maia", "change.md"),
		filepath.Join(".maia", ".generated", "plan.md"),
		"feature.txt",
	} {
		if err := os.WriteFile(filepath.Join(wt, name), []byte("x\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	status, err := exec.Command("git", "-C", wt, "status", "--porcelain").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(status), ".maia") {
		t.Fatalf(".maia/ must not appear in status:\n%s", status)
	}
	if !strings.Contains(string(status), "feature.txt") {
		t.Fatalf("regular untracked files must still appear:\n%s", status)
	}

	// the shared exclude file holds exactly one entry
	excl, err := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(excl), ".maia"); got != 1 {
		t.Fatalf("want exactly one .maia line in info/exclude, got %d:\n%s", got, excl)
	}
}
