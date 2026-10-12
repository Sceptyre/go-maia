package web

import (
	"testing"
	"time"
)

func TestJobManagerScopesRunningCommandsByChangeRequest(t *testing.T) {
	manager := NewJobManager()
	start := func(project, slug string) (*Job, error) {
		return manager.Start(JobStart{
			ProjectID: project, Slug: slug, Kind: "plan", Command: "plan",
			Dir: t.TempDir(), Exe: "sh", Argv: []string{"-c", "sleep 1"},
		})
	}
	first, err := start("project", "change-a")
	if err != nil {
		t.Fatal(err)
	}
	second, err := start("project", "change-b")
	if err != nil {
		t.Fatalf("a different CR should run concurrently: %v", err)
	}
	if _, err := start("project", "change-a"); err == nil {
		t.Fatal("expected a second command for the same CR to be rejected")
	}
	third, err := start("other-project", "change-a")
	if err != nil {
		t.Fatalf("same slug in another project should have an independent scope: %v", err)
	}
	if got := len(manager.RunningAll()); got != 3 {
		t.Fatalf("running jobs = %d, want 3", got)
	}
	select {
	case <-first.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("first job did not finish")
	}
	select {
	case <-second.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("second job did not finish")
	}
	select {
	case <-third.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("third job did not finish")
	}
}
