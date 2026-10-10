package llm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSessionIDForStableByProjectAndWork(t *testing.T) {
	first := SessionIDFor("/repo", "change-a")
	if first != SessionIDFor("/repo", "change-a") {
		t.Fatal("session ID changed for the same project and change request")
	}
	if first == SessionIDFor("/repo", "change-b") || first == SessionIDFor("/other", "change-a") {
		t.Fatal("session IDs should differ across change requests and projects")
	}
}

func TestWorktreeSessionIDMatchesProjectSlug(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README"), []byte("test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "init", "-b", "main")
	gitRun(t, root, "config", "user.name", "Test")
	gitRun(t, root, "config", "user.email", "test@example.com")
	gitRun(t, root, "add", "README")
	gitRun(t, root, "commit", "-m", "initial")
	worktree := filepath.Join(t.TempDir(), "child")
	gitRun(t, root, "worktree", "add", "-b", "maia/child", worktree, "HEAD")
	gotRoot, gotWork := sessionIdentity(worktree, "")
	if got := SessionIDFor(gotRoot, gotWork); got != SessionIDFor(root, "child") {
		t.Fatalf("worktree session ID %q does not match project/slug ID", got)
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

func TestChatSubstitutesMaiaSessionHeader(t *testing.T) {
	const session = "maia-stable-session"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-OpenCode-Session"); got != session {
			t.Errorf("X-OpenCode-Session = %q, want %q", got, session)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{
			"message":       map[string]string{"role": "assistant", "content": "ok"},
			"finish_reason": "stop",
		}}})
	}))
	defer server.Close()

	client := &Client{
		BaseURL:    server.URL,
		Model:      "test-model",
		MaxTokens:  64,
		Headers:    map[string]string{"X-OpenCode-Session": "{maia_session}"},
		SessionID:  session,
		HTTPClient: server.Client(),
	}
	if _, err := client.Chat(nil, nil); err != nil {
		t.Fatal(err)
	}
}
