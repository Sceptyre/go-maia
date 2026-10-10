package web

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func projectDefaultBranch(repo, configured string) string {
	if branch := strings.TrimSpace(configured); branch != "" {
		return branch
	}
	if out, err := runGitIn(repo, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if branch := strings.TrimSpace(string(out)); branch != "" {
			return branch
		}
	}
	if out, err := runGitIn(repo, "branch", "--show-current"); err == nil {
		return strings.TrimSpace(string(out))
	}
	return ""
}

func listProjectBranches(repo, configured string) ([]string, string, error) {
	out, err := runGitIn(repo, "for-each-ref", "--format=%(refname:short)", "refs/heads", "refs/remotes/origin")
	if err != nil {
		return nil, "", fmt.Errorf("list branches: %w", err)
	}
	seen := make(map[string]bool)
	branches := make([]string, 0)
	for _, line := range strings.Split(string(out), "\n") {
		branch := strings.TrimSpace(line)
		branch = strings.TrimPrefix(branch, "remotes/")
		if branch == "" || branch == "origin/HEAD" || seen[branch] {
			continue
		}
		seen[branch] = true
		branches = append(branches, branch)
	}
	sort.Strings(branches)
	defaultBranch := projectDefaultBranch(repo, configured)
	if resolved, err := resolveSourceBranch(repo, defaultBranch); err == nil {
		defaultBranch = resolved
	}
	if defaultBranch == "" && len(branches) > 0 {
		defaultBranch = branches[0]
	}
	return branches, defaultBranch, nil
}

func resolveSourceBranch(repo, branch string) (string, error) {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return "", fmt.Errorf("source branch is required")
	}
	if _, err := runGitIn(repo, "check-ref-format", "--branch", branch); err != nil {
		return "", fmt.Errorf("invalid source branch %q", branch)
	}
	if strings.HasPrefix(branch, "origin/") {
		if _, err := runGitIn(repo, "show-ref", "--verify", "--quiet", "refs/remotes/"+branch); err != nil {
			return "", fmt.Errorf("source branch %q does not exist on origin", branch)
		}
		return branch, nil
	}
	if _, err := runGitIn(repo, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		return branch, nil
	}
	if _, err := runGitIn(repo, "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+branch); err == nil {
		return "origin/" + branch, nil
	}
	return "", fmt.Errorf("source branch %q does not exist locally or on origin", branch)
}

func setFrontmatterField(path, key, value string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return fmt.Errorf("missing frontmatter in %s", path)
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			for j := 1; j < i; j++ {
				if parts := strings.SplitN(lines[j], ":", 2); len(parts) == 2 && strings.TrimSpace(parts[0]) == key {
					lines[j] = key + ": " + value
					return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
				}
			}
			lines = append(lines[:i], append([]string{key + ": " + value}, lines[i:]...)...)
			return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
		}
	}
	return fmt.Errorf("unterminated frontmatter in %s", path)
}

func managedClonePath(remote string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	name := strings.TrimSuffix(filepath.Base(strings.TrimRight(remote, "/")), ".git")
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "repository"
	}
	sum := sha256.Sum256([]byte(remote))
	return filepath.Join(home, ".maia", "projects", name+"-"+hex.EncodeToString(sum[:4])), nil
}

func validateRemoteURL(raw string) (string, error) {
	remote := strings.TrimSpace(raw)
	if remote == "" || strings.ContainsAny(remote, "\r\n\t ") || strings.HasPrefix(remote, "-") {
		return "", fmt.Errorf("enter a valid HTTPS, SSH, or SCP-style repository URL")
	}
	if strings.Contains(remote, "@") && !strings.Contains(remote, "://") {
		parts := strings.SplitN(remote, ":", 2)
		if len(parts) == 2 && strings.Contains(parts[0], "@") && parts[1] != "" && !strings.HasPrefix(parts[1], "/") {
			return remote, nil
		}
	}
	u, err := url.Parse(remote)
	hasPassword := false
	if err == nil && u.User != nil {
		_, hasPassword = u.User.Password()
	}
	if err != nil || (u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "ssh") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || (u.User != nil && (u.Scheme != "ssh" || hasPassword)) {
		return "", fmt.Errorf("enter a valid HTTPS, SSH, or SCP-style repository URL without embedded credentials")
	}
	return remote, nil
}

func validHTTPHeaderName(name string) bool {
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

func cloneRemote(remote, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return fmt.Errorf("create project directory: %w", err)
	}
	cmd := exec.Command("git", "clone", "--", remote, target)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("clone repository: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
