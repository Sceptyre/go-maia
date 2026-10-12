package web

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

func TestRegistrySnapshotSurvivesDeleteUnderIteration(t *testing.T) {
	store := &RegistryStore{path: filepath.Join(t.TempDir(), "web.json"), reg: defaultRegistry()}
	ids := make([]string, 0, 3)
	for _, name := range []string{"alpha", "beta", "gamma"} {
		p, _, err := store.AddProject(filepath.Join(t.TempDir(), name))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, p.ID)
	}

	// The poller's pattern: hold a registry read across a concurrent delete.
	snap := store.Get()
	if len(snap.Projects) != 3 {
		t.Fatalf("want 3 projects, got %d", len(snap.Projects))
	}
	if err := store.RemoveProject(ids[1]); err != nil {
		t.Fatal(err)
	}

	seen := 0
	for i := range snap.Projects {
		_ = snap.Projects[i]
		seen++
	}
	if seen != 3 {
		t.Fatalf("snapshot mutated underfoot: iterated %d of 3 projects", seen)
	}
	if got := len(store.Get().Projects); got != 2 {
		t.Fatalf("store should hold 2 projects after delete, got %d", got)
	}
}

func TestRegistrySnapshotMutationIsLocal(t *testing.T) {
	store := &RegistryStore{path: filepath.Join(t.TempDir(), "web.json"), reg: defaultRegistry()}
	if _, _, err := store.AddProject(filepath.Join(t.TempDir(), "alpha")); err != nil {
		t.Fatal(err)
	}
	labelsBefore := len(store.Get().Defaults.Github.Labels)

	snap := store.Get()
	snap.Projects[0].Name = "tampered"
	snap.Defaults.Github.Labels = append(snap.Defaults.Github.Labels, "extra")

	live := store.Get()
	if live.Projects[0].Name == "tampered" {
		t.Fatalf("snapshot mutation leaked into store: %q", live.Projects[0].Name)
	}
	if got := len(live.Defaults.Github.Labels); got != labelsBefore {
		t.Fatalf("snapshot label mutation leaked into store: %d != %d", got, labelsBefore)
	}
}

func TestRegistryPollerStyleReadsDuringDeletes(t *testing.T) {
	store := &RegistryStore{path: filepath.Join(t.TempDir(), "web.json"), reg: defaultRegistry()}
	base := t.TempDir()
	for i := 0; i < 3; i++ {
		if _, _, err := store.AddProject(filepath.Join(base, fmt.Sprintf("p%d", i))); err != nil {
			t.Fatal(err)
		}
	}

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
			}
			reg := store.Get()
			for i := range reg.Projects {
				_ = reg.Projects[i]
			}
		}
	}()
	for i := 0; i < 200; i++ {
		p, _, err := store.AddProject(filepath.Join(base, fmt.Sprintf("round-%d", i)))
		if err != nil {
			close(done)
			wg.Wait()
			t.Fatal(err)
		}
		if err := store.RemoveProject(p.ID); err != nil {
			close(done)
			wg.Wait()
			t.Fatal(err)
		}
	}
	close(done)
	wg.Wait()
}
