package llm

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SessionIDFor returns a stable, opaque ID for a repository and work item.
func SessionIDFor(repo, work string) string {
	abs, err := filepath.Abs(repo)
	if err == nil {
		repo = filepath.Clean(abs)
	}
	sum := sha256.Sum256([]byte(repo + "\x00" + work))
	return "maia-" + hex.EncodeToString(sum[:16])
}

func currentSessionID() string {
	if id := strings.TrimSpace(os.Getenv("MAIA_SESSION_ID")); id != "" {
		return id
	}
	cwd, err := os.Getwd()
	if err != nil {
		return SessionIDFor(".", "")
	}
	root := cwd
	if out, err := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel").Output(); err == nil {
		root = strings.TrimSpace(string(out))
	}
	branch := ""
	if out, err := exec.Command("git", "-C", cwd, "rev-parse", "--abbrev-ref", "HEAD").Output(); err == nil {
		branch = strings.TrimSpace(string(out))
	}
	return sessionIDForPath(root, branch)
}

// UseSessionIDFor binds subsequent client construction in this process to one work item.
func UseSessionIDFor(repo, work string) {
	if strings.TrimSpace(os.Getenv("MAIA_SESSION_ID")) == "" {
		root, key := sessionIdentity(repo, work)
		_ = os.Setenv("MAIA_SESSION_ID", SessionIDFor(root, key))
	}
}

func sessionIDForPath(repo, branch string) string {
	root, work := sessionIdentity(repo, branch)
	return SessionIDFor(root, work)
}

func sessionIdentity(repo, work string) (string, string) {
	root := repo
	if out, err := exec.Command("git", "-C", repo, "rev-parse", "--show-toplevel").Output(); err == nil {
		root = strings.TrimSpace(string(out))
	}
	if work == "" {
		if info, err := os.Stat(filepath.Join(root, ".git")); err == nil && !info.IsDir() {
			if common, err := exec.Command("git", "-C", root, "rev-parse", "--path-format=absolute", "--git-common-dir").Output(); err == nil {
				work = filepath.Base(root)
				root = filepath.Dir(strings.TrimSpace(string(common)))
			}
		}
	}
	return root, work
}

func expandSessionTemplate(value, session string) string {
	value = strings.ReplaceAll(value, "${maia_session}", session)
	value = strings.ReplaceAll(value, "{{maia_session}}", session)
	return strings.ReplaceAll(value, "{maia_session}", session)
}

func validHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(c)) {
			continue
		}
		return false
	}
	return true
}
