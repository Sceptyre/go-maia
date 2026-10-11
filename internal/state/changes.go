package state

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ChangeWorktree struct {
	Path   string `json:"path"`
	Branch string `json:"branch"`
}

// ChangeRequest is durable maia intent; its worktree is an optional checkout.
type ChangeRequest struct {
	ProjectPath    string            `json:"project_path"`
	Slug           string            `json:"slug"`
	Branch         string            `json:"branch"`
	Description    string            `json:"description"`
	SourceBranch   string            `json:"source_branch,omitempty"`
	DefaultBase    bool              `json:"default_base,omitempty"`
	Status         string            `json:"status"`
	Issue          int               `json:"issue,omitempty"`
	PR             string            `json:"pr,omitempty"`
	RebaseConflict bool              `json:"rebase_conflict,omitempty"`
	Change         string            `json:"change"`
	Artifacts      map[string]string `json:"artifacts,omitempty"`
	Worktree       *ChangeWorktree   `json:"worktree,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

func DefaultSourceBranch(repo string) string {
	if out, err := exec.Command("git", "-C", repo, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD").Output(); err == nil {
		return strings.TrimSpace(string(out))
	}
	for _, branch := range []string{"main", "master"} {
		if err := exec.Command("git", "-C", repo, "show-ref", "--verify", "--quiet", "refs/heads/"+branch).Run(); err == nil {
			return branch
		}
	}
	if out, err := exec.Command("git", "-C", repo, "branch", "--show-current").Output(); err == nil {
		return strings.TrimSpace(string(out))
	}
	return ""
}

func IsDefaultSourceBranch(repo, branch string) bool {
	defaultBranch := strings.TrimPrefix(DefaultSourceBranch(repo), "origin/")
	return defaultBranch != "" && strings.TrimPrefix(strings.TrimSpace(branch), "origin/") == defaultBranch
}

type changeFile struct {
	Version  int             `json:"version"`
	Requests []ChangeRequest `json:"change_requests"`
}

var changeFileMu sync.Mutex

func changeStatePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".maia", "changes.json")
}

func LoadChangeRequests(projectPath string) ([]ChangeRequest, error) {
	changeFileMu.Lock()
	defer changeFileMu.Unlock()
	data, err := readChangeFile()
	if err != nil {
		return nil, err
	}
	projectPath = cleanProjectPath(projectPath)
	out := make([]ChangeRequest, 0)
	for _, cr := range data.Requests {
		if cleanProjectPath(cr.ProjectPath) == projectPath {
			out = append(out, cr)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].Slug < out[j].Slug
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

func GetChangeRequest(projectPath, slug string) (*ChangeRequest, bool, error) {
	changes, err := LoadChangeRequests(projectPath)
	if err != nil {
		return nil, false, err
	}
	for i := range changes {
		if changes[i].Slug == slug {
			return &changes[i], true, nil
		}
	}
	return nil, false, nil
}

func SaveChangeRequest(cr ChangeRequest) error {
	changeFileMu.Lock()
	defer changeFileMu.Unlock()
	unlock, err := acquireChangeLock()
	if err != nil {
		return err
	}
	defer unlock()
	if cr.Slug == "" || cr.ProjectPath == "" {
		return fmt.Errorf("change request project path and slug are required")
	}
	cr.ProjectPath = cleanProjectPath(cr.ProjectPath)
	data, err := readChangeFile()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	requestedUpdated := cr.UpdatedAt
	for i := range data.Requests {
		current := &data.Requests[i]
		if cleanProjectPath(current.ProjectPath) == cr.ProjectPath && current.Slug == cr.Slug {
			if !requestedUpdated.IsZero() && current.UpdatedAt.After(requestedUpdated) {
				return nil
			}
			if cr.CreatedAt.IsZero() {
				cr.CreatedAt = current.CreatedAt
			}
			if cr.Branch == "" {
				cr.Branch = current.Branch
			}
			if cr.Branch == "" {
				cr.Branch = "maia/" + cr.Slug
			}
			if cr.Description == "" {
				cr.Description = current.Description
			}
			if cr.Status == "" {
				cr.Status = current.Status
			}
			if cr.Change == "" {
				cr.Change = current.Change
			}
			if cr.Artifacts == nil {
				cr.Artifacts = current.Artifacts
			}
			if cr.Worktree == nil {
				cr.Worktree = current.Worktree
			}
			cr.UpdatedAt = now
			data.Requests[i] = cr
			return writeChangeFile(data)

		}
	}
	if cr.CreatedAt.IsZero() {
		cr.CreatedAt = now
	}
	if cr.UpdatedAt.IsZero() {
		cr.UpdatedAt = now
	}
	if cr.Branch == "" {
		cr.Branch = "maia/" + cr.Slug
	}
	data.Requests = append(data.Requests, cr)
	return writeChangeFile(data)
}

func DetachChangeWorktree(projectPath, slug string) error {
	return updateChangeRequest(projectPath, slug, func(cr *ChangeRequest) error {
		cr.Worktree = nil
		return nil
	})
}

func SetChangeWorktree(projectPath, slug string, worktree *ChangeWorktree) error {
	return updateChangeRequest(projectPath, slug, func(cr *ChangeRequest) error {
		cr.Worktree = worktree
		return nil
	})
}

func UpdateChangeArtifact(projectPath, slug, name, contents string) error {
	if name != "research" && name != "plan" {
		return fmt.Errorf("unsupported change artifact: %s", name)
	}
	return updateChangeRequest(projectPath, slug, func(cr *ChangeRequest) error {
		if cr.Artifacts == nil {
			cr.Artifacts = make(map[string]string)
		}
		cr.Artifacts[name] = contents
		return nil
	})
}

func UpdateChangeDocument(projectPath, slug, contents string) error {
	return updateChangeRequest(projectPath, slug, func(cr *ChangeRequest) error {
		cr.Change = contents
		if value := frontmatterField(contents, "description"); value != "" {
			cr.Description = value
		}
		if value := frontmatterField(contents, "source_branch"); value != "" {
			cr.SourceBranch = value
			cr.DefaultBase = IsDefaultSourceBranch(projectPath, value)
		}
		if value := frontmatterField(contents, "status"); value != "" {
			cr.Status = value
		} else if cr.Status == "" {
			cr.Status = "new"
		}
		if value := frontmatterField(contents, "pr"); value != "" {
			cr.PR = value
		}
		cr.Issue = 0
		if value := frontmatterField(contents, "issue"); value != "" {
			cr.Issue, _ = strconv.Atoi(value)
		}
		return nil
	})
}

func UpdateChangePR(projectPath, slug, url string) error {
	return updateChangeRequest(projectPath, slug, func(cr *ChangeRequest) error {
		cr.PR = url
		cr.Change = setFrontmatterFieldContent(cr.Change, "pr", url)
		return nil
	})
}

func UpdateChangeDefaultBase(projectPath, slug string, isDefault bool) error {
	return updateChangeRequest(projectPath, slug, func(cr *ChangeRequest) error {
		cr.DefaultBase = isDefault
		return nil
	})
}

func setFrontmatterFieldContent(content, key, value string) string {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return content
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			for j := 1; j < i; j++ {
				parts := strings.SplitN(lines[j], ":", 2)
				if len(parts) == 2 && strings.TrimSpace(parts[0]) == key {
					lines[j] = key + ": " + value
					return strings.Join(lines, "\n")
				}
			}
			lines = append(lines[:i], append([]string{key + ": " + value}, lines[i:]...)...)
			return strings.Join(lines, "\n")
		}
	}
	return content
}

func UpdateChangeRebaseConflict(projectPath, slug string, conflicted bool) error {
	return updateChangeRequest(projectPath, slug, func(cr *ChangeRequest) error {
		cr.RebaseConflict = conflicted
		return nil
	})
}

func UpdateChangeStatus(projectPath, slug, status string) error {
	return updateChangeRequest(projectPath, slug, func(cr *ChangeRequest) error {
		cr.Status = status
		return nil
	})
}

func DeleteChangeRequest(projectPath, slug string) error {
	changeFileMu.Lock()
	defer changeFileMu.Unlock()
	unlock, err := acquireChangeLock()
	if err != nil {
		return err
	}
	defer unlock()
	data, err := readChangeFile()
	if err != nil {
		return err
	}
	projectPath = cleanProjectPath(projectPath)
	for i, cr := range data.Requests {
		if cleanProjectPath(cr.ProjectPath) == projectPath && cr.Slug == slug {
			if cr.Worktree != nil {
				return fmt.Errorf("cleanup the worktree before deleting change request %s", slug)
			}
			data.Requests = append(data.Requests[:i], data.Requests[i+1:]...)
			return writeChangeFile(data)
		}
	}
	return fmt.Errorf("change request not found: %s", slug)
}

// SyncChangeRequests imports legacy on-disk worktrees and reconciles materialization.
func SyncChangeRequests(projectPath string) ([]ChangeRequest, error) {
	projectPath = cleanProjectPath(projectPath)
	slugs, err := ListWorktreesIn(projectPath)
	if err != nil {
		return nil, err
	}
	existing, err := LoadChangeRequests(projectPath)
	if err != nil {
		return nil, err
	}
	bySlug := make(map[string]ChangeRequest, len(existing))
	for _, cr := range existing {
		bySlug[cr.Slug] = cr
	}
	seen := make(map[string]bool, len(slugs))
	for _, slug := range slugs {
		path := filepath.Join(RepoWorktreeDirFor(projectPath), slug)
		changePath := filepath.Join(path, StateDir, "change.md")
		contents, readErr := os.ReadFile(changePath)
		cr, found := bySlug[slug]
		if readErr != nil {
			original := cr
			if !found {
				cr = ChangeRequest{ProjectPath: projectPath, Slug: slug, Status: "new", CreatedAt: time.Now().UTC()}
			}
			cr.ProjectPath = projectPath
			cr.Slug = slug
			if cr.Branch == "" {
				cr.Branch = "maia/" + slug
				if branch, err := exec.Command("git", "-C", path, "rev-parse", "--abbrev-ref", "HEAD").Output(); err == nil {
					cr.Branch = strings.TrimSpace(string(branch))
				}
			}
			cr.Worktree = &ChangeWorktree{Path: path, Branch: cr.Branch}
			if !found || !reflect.DeepEqual(original, cr) {
				if err := SaveChangeRequest(cr); err != nil {
					return nil, err
				}
			}
			seen[slug] = true
			continue
		}
		original := cr
		if cr.CreatedAt.IsZero() {
			cr.CreatedAt = time.Now().UTC()
		}
		if cr.Branch == "" {
			cr.Branch = "maia/" + slug
		}
		if branch, err := exec.Command("git", "-C", path, "rev-parse", "--abbrev-ref", "HEAD").Output(); err == nil {
			if actual := strings.TrimSpace(string(branch)); actual != "" && actual != "HEAD" {
				cr.Branch = actual
			}
		}

		for _, artifact := range []string{"research", "plan"} {
			artifactPath := filepath.Join(path, GetGeneratedDir(), artifact+".md")
			if contents, err := os.ReadFile(artifactPath); err == nil {
				if cr.Artifacts == nil {
					cr.Artifacts = make(map[string]string)
				}
				cr.Artifacts[artifact] = string(contents)
			}
		}
		if status, err := GetStatusIn(path); err == nil {
			cr.Status = status
		}

		cr.ProjectPath = projectPath
		cr.Slug = slug
		cr.Change = string(contents)
		if value := frontmatterField(cr.Change, "description"); value != "" {
			cr.Description = value
		}
		if value := frontmatterField(cr.Change, "source_branch"); value != "" {
			cr.SourceBranch = value
			if IsDefaultSourceBranch(projectPath, value) {
				cr.DefaultBase = true
			}
		}
		if value := frontmatterField(cr.Change, "status"); value != "" {
			cr.Status = value
		}
		if value := frontmatterField(cr.Change, "issue"); value != "" {
			cr.Issue, _ = strconv.Atoi(value)
		}
		if value := frontmatterField(cr.Change, "pr"); value != "" {
			cr.PR = value
		}
		cr.Worktree = &ChangeWorktree{Path: path, Branch: cr.Branch}
		if !found || !reflect.DeepEqual(original, cr) {
			if err := SaveChangeRequest(cr); err != nil {
				return nil, err
			}
		}
		seen[slug] = true
	}
	for _, cr := range existing {
		if cr.Worktree != nil && !seen[cr.Slug] {
			if err := DetachChangeWorktree(projectPath, cr.Slug); err != nil {
				return nil, err
			}
		}
	}
	return LoadChangeRequests(projectPath)
}

func CountMaterializedChangeWorktrees(projectPath string) (int, error) {
	changes, err := SyncChangeRequests(projectPath)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, cr := range changes {
		if cr.Worktree != nil {
			count++
		}
	}
	return count, nil
}

func updateChangeRequest(projectPath, slug string, update func(*ChangeRequest) error) error {
	changeFileMu.Lock()
	defer changeFileMu.Unlock()
	unlock, err := acquireChangeLock()
	if err != nil {
		return err
	}
	defer unlock()
	data, err := readChangeFile()
	if err != nil {
		return err
	}
	projectPath = cleanProjectPath(projectPath)
	for i := range data.Requests {
		cr := &data.Requests[i]
		if cleanProjectPath(cr.ProjectPath) == projectPath && cr.Slug == slug {
			if err := update(cr); err != nil {
				return err
			}
			cr.UpdatedAt = time.Now().UTC()
			return writeChangeFile(data)
		}
	}
	return fmt.Errorf("change request not found: %s", slug)
}

func readChangeFile() (changeFile, error) {
	path := changeStatePath()
	contents, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return changeFile{Version: 1, Requests: []ChangeRequest{}}, nil
	}
	if err != nil {
		return changeFile{}, err
	}
	var data changeFile
	if err := json.Unmarshal(contents, &data); err != nil {
		return changeFile{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if data.Version == 0 {
		data.Version = 1
	}
	if data.Requests == nil {
		data.Requests = []ChangeRequest{}
	}
	return data, nil
}

func writeChangeFile(data changeFile) error {
	path := changeStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data.Version = 1
	contents, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(contents, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func cleanProjectPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	return filepath.Clean(abs)
}

func frontmatterField(content, key string) string {
	if !strings.HasPrefix(content, "---") {
		return ""
	}
	for _, line := range strings.Split(content, "\n")[1:] {
		if strings.TrimSpace(line) == "---" {
			break
		}
		if parts := strings.SplitN(line, ":", 2); len(parts) == 2 && strings.TrimSpace(parts[0]) == key {
			value := strings.TrimSpace(parts[1])
			if unquoted, err := strconv.Unquote(value); err == nil {
				return unquoted
			}
			return value
		}
	}
	return ""
}

func RepositoryForWorktree(worktreePath string) (string, error) {
	out, err := exec.Command("git", "-C", worktreePath, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return "", fmt.Errorf("not a git worktree: %s", worktreePath)
	}
	root := filepath.Dir(strings.TrimSpace(string(out)))
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	return root, nil
}

func acquireChangeLock() (func(), error) {
	lockPath := changeStatePath() + ".lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0700); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := os.Mkdir(lockPath, 0700); err == nil {
			return func() { _ = os.Remove(lockPath) }, nil
		} else if !os.IsExist(err) {
			return nil, err
		}
		if info, err := os.Stat(lockPath); err == nil && time.Since(info.ModTime()) > time.Minute {
			_ = os.Remove(lockPath)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for change state lock")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
