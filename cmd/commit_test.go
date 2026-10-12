package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStageWorktreeChangesExcludesMaiaState(t *testing.T) {
	repo := t.TempDir()
	gitRun(t, repo, "init", "-b", "main")
	gitRun(t, repo, "config", "user.email", "test@example.com")
	gitRun(t, repo, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "README")
	gitRun(t, repo, "commit", "-m", "initial")

	// maia state that must never be staged
	if err := os.MkdirAll(filepath.Join(repo, ".maia", ".generated"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		filepath.Join(".maia", "change.md"),
		filepath.Join(".maia", ".generated", "research.md"),
		filepath.Join(".maia", ".generated", "plan.md"),
	} {
		if err := os.WriteFile(filepath.Join(repo, name), []byte("---\nname: x\n---\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// a real change: new file plus modified tracked file
	if err := os.WriteFile(filepath.Join(repo, "feature.go"), []byte("package x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("v2\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := stageWorktreeChanges(repo); err != nil {
		t.Fatal(err)
	}

	// staging heals a missing local exclude as a side effect
	excl, err := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(excl), ".maia") {
		t.Fatalf("info/exclude should have been written while staging:\n%s", excl)
	}

	staged := gitOutput(t, repo, "diff", "--cached", "--name-only")
	if strings.Contains(staged, ".maia") {
		t.Fatalf(".maia/ must not be staged:\n%s", staged)
	}
	for _, want := range []string{"README", "feature.go"} {
		if !strings.Contains(staged, want) {
			t.Fatalf("expected %s staged:\n%s", want, staged)
		}
	}
}
