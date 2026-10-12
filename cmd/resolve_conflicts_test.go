package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyConflictPatchOnlyStagesConflictFiles(t *testing.T) {
	repo := t.TempDir()
	gitRun(t, repo, "init", "-b", "main")
	gitRun(t, repo, "config", "user.name", "Test")
	gitRun(t, repo, "config", "user.email", "test@example.com")
	conflictPath := filepath.Join(repo, "conflict.txt")
	if err := os.WriteFile(conflictPath, []byte("<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> branch\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "other.txt"), []byte("untouched\n"), 0600); err != nil {
		t.Fatal(err)
	}
	patch := "diff --git a/conflict.txt b/conflict.txt\n--- a/conflict.txt\n+++ b/conflict.txt\n@@ -1,5 +1 @@\n-<<<<<<< HEAD\n-ours\n-=======\n-theirs\n->>>>>>> branch\n+resolved\n"
	if err := applyConflictPatch(repo, patch, []string{"conflict.txt"}); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(conflictPath)
	if err != nil || string(contents) != "resolved\n" {
		t.Fatalf("resolved content %q, err %v", contents, err)
	}
	if staged := gitOutput(t, repo, "diff", "--cached", "--name-only"); strings.TrimSpace(staged) != "conflict.txt" {
		t.Fatalf("staged files = %q", staged)
	}
}

func TestApplyConflictPatchRejectsOtherFiles(t *testing.T) {
	repo := t.TempDir()
	gitRun(t, repo, "init", "-b", "main")
	patch := "diff --git a/other.txt b/other.txt\n--- a/other.txt\n+++ b/other.txt\n@@ -1 +1 @@\n-a\n+b\n"
	if err := applyConflictPatch(repo, patch, []string{"conflict.txt"}); err == nil {
		t.Fatal("expected non-conflict file patch to be rejected")
	}
	if _, err := os.Stat(filepath.Join(repo, "other.txt")); !os.IsNotExist(err) {
		t.Fatalf("unapproved file was changed: %v", err)
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
	return string(out)
}
