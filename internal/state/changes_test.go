package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestChangeRequestSurvivesWorktreeDetach(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := filepath.Join(home, "repo")
	cr := ChangeRequest{
		ProjectPath: project, Slug: "child-change", Description: "child change",
		SourceBranch: "feature/parent", Status: "planned",
		Change:    "---\nname: child-change\nstatus: planned\n---\n\nGoal\n",
		Artifacts: map[string]string{"plan": "phase one"},
		Worktree:  &ChangeWorktree{Path: filepath.Join(home, "worktrees", "child-change"), Branch: "maia/child-change"},
	}
	if err := SaveChangeRequest(cr); err != nil {
		t.Fatal(err)
	}
	if err := DetachChangeWorktree(project, cr.Slug); err != nil {
		t.Fatal(err)
	}
	got, ok, err := GetChangeRequest(project, cr.Slug)
	if err != nil || !ok {
		t.Fatalf("GetChangeRequest: ok=%v err=%v", ok, err)
	}
	if got.Worktree != nil || got.Change != cr.Change || got.Artifacts["plan"] != "phase one" {
		t.Fatalf("detach lost CR data: %+v", got)
	}
}

func TestSyncChangeRequestsImportsLegacyWorktree(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := filepath.Join(home, "repo")
	changePath := filepath.Join(RepoWorktreeDirFor(project), "legacy", StateDir, "change.md")
	if err := os.MkdirAll(filepath.Dir(changePath), 0755); err != nil {
		t.Fatal(err)
	}
	contents := "---\nname: legacy\ndescription: imported legacy\nstatus: new\nsource_branch: main\nissue: 42\n---\n"
	if err := os.WriteFile(changePath, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	changes, err := SyncChangeRequests(project)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Slug != "legacy" || changes[0].Issue != 42 || changes[0].Worktree == nil {
		t.Fatalf("unexpected migrated requests: %+v", changes)
	}
}

func TestMaterializedCountDoesNotDeleteRequestOnCleanup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := filepath.Join(home, "repo")
	worktree := filepath.Join(RepoWorktreeDirFor(project), "pending-change")
	changePath := filepath.Join(worktree, StateDir, "change.md")
	if err := os.MkdirAll(filepath.Dir(changePath), 0755); err != nil {
		t.Fatal(err)
	}
	contents := "---\nname: pending-change\ndescription: pending\nstatus: new\nsource_branch: main\n---\n"
	if err := os.WriteFile(changePath, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	if err := SaveChangeRequest(ChangeRequest{
		ProjectPath: project, Slug: "pending-change", Branch: "maia/pending-change",
		Description: "pending", SourceBranch: "main", Status: "new", Change: contents,
		Worktree: &ChangeWorktree{Path: worktree, Branch: "maia/pending-change"},
	}); err != nil {
		t.Fatal(err)
	}
	if count, err := CountMaterializedChangeWorktrees(project); err != nil || count != 1 {
		t.Fatalf("materialized count = %d, %v; want 1", count, err)
	}
	if err := os.RemoveAll(worktree); err != nil {
		t.Fatal(err)
	}
	if count, err := CountMaterializedChangeWorktrees(project); err != nil || count != 0 {
		t.Fatalf("materialized count after cleanup = %d, %v; want 0", count, err)
	}
	if cr, ok, err := GetChangeRequest(project, "pending-change"); err != nil || !ok || cr.Worktree != nil || cr.Change != contents {
		t.Fatalf("cleanup lost CR state: cr=%+v ok=%v err=%v", cr, ok, err)
	}
}
