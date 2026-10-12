package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sceptyre/maia/internal/llm"
	"github.com/sceptyre/maia/internal/state"
	"github.com/spf13/cobra"
)

var resolveConflictsCmd = &cobra.Command{
	Use:   "resolve-conflicts <slug>",
	Short: "Ask the configured model to resolve source-rebase conflicts",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := state.RepositoryForWorktree(".")
		if err != nil {
			return err
		}
		if _, err := state.SyncChangeRequests(repoRoot); err != nil {
			return err
		}
		cr, found, err := state.GetChangeRequest(repoRoot, args[0])
		if err != nil {
			return err
		}
		if !found || cr.Worktree == nil {
			return fmt.Errorf("change request %s must have a materialized worktree", args[0])
		}
		if !rebaseInProgress(cr.Worktree.Path) {
			return fmt.Errorf("no rebase is in progress for %s", cr.Slug)
		}
		files, err := unresolvedConflictFiles(cr.Worktree.Path)
		if err != nil {
			return err
		}
		if len(files) == 0 {
			return fmt.Errorf("no unresolved conflict files found; stage your resolutions and continue the rebase")
		}
		llm.UseSessionIDFor(cr.Worktree.Path, "")
		client := llm.NewClient()
		if client.APIKey == "" {
			return fmt.Errorf("API key not configured. Set it in ~/.maia/config.json or Settings")
		}
		context, err := conflictContext(cr.Worktree.Path, files)
		if err != nil {
			return err
		}
		prompt := fmt.Sprintf(`Resolve the Git rebase conflicts for this maia change request.

Change request: %s
Source branch: %s
Change request document:
%s

Return only a standard unified Git diff (diff --git headers) that resolves the conflicts while preserving the intent of both sides. Do not include markdown fences, explanations, commands, or changes to files not listed. Maia validates and stages only the listed conflict files; the user reviews the patch before continuing the rebase.

Conflict files and current contents (JSON):
%s`, cr.Slug, cr.SourceBranch, cr.Change, context)
		response, err := client.GetResponse([]llm.Message{
			llm.NewMessage("system", "You resolve merge conflicts conservatively. Return a valid unified Git patch only. Preserve unrelated changes and do not invent behavior."),
			llm.NewMessage("user", prompt),
		})
		if err != nil {
			return fmt.Errorf("conflict resolution request: %w", err)
		}
		patch := strings.TrimSpace(response)
		if strings.HasPrefix(patch, "```diff") {
			patch = strings.TrimSpace(strings.TrimPrefix(patch, "```diff"))
			patch = strings.TrimSuffix(patch, "```")
		}
		if err := applyConflictPatch(cr.Worktree.Path, patch, files); err != nil {
			return err
		}
		fmt.Println("AI patch applied and only the conflicted files were staged. Review the diff before continuing the rebase:")
		fmt.Println(patch)
		fmt.Printf("\nWhen satisfied, run `maia update-base %s --continue`.\n", cr.Slug)
		return nil
	},
}

func conflictContext(worktree string, files []string) (string, error) {
	var b strings.Builder
	b.WriteString("{\n")
	for i, name := range files {
		path, err := confinedConflictPath(worktree, name)
		if err != nil {
			return "", err
		}
		contents, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("read conflict file %s: %w", name, err)
		}
		if i > 0 {
			b.WriteString(",\n")
		}
		quotedName := fmt.Sprintf("%q", name)
		quotedContents := fmt.Sprintf("%q", string(contents))
		b.WriteString(quotedName + ": " + quotedContents)
	}
	b.WriteString("\n}")
	return b.String(), nil
}

func unresolvedConflictFiles(worktree string) ([]string, error) {
	cmd := exec.Command("git", "-C", worktree, "diff", "--name-only", "--diff-filter=U", "-z")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list conflict files: %w", err)
	}
	var files []string
	for _, raw := range bytes.Split(out, []byte{0}) {
		if len(raw) == 0 {
			continue
		}
		name := string(raw)
		if filepath.IsAbs(name) || filepath.Clean(name) == ".." || strings.HasPrefix(filepath.Clean(name), ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("unsafe conflict path: %q", name)
		}
		files = append(files, filepath.ToSlash(name))
	}
	return files, nil
}

func applyConflictPatch(worktree, patch string, conflicts []string) error {
	if !strings.HasPrefix(patch, "diff --git ") {
		return fmt.Errorf("model response was not a unified Git diff; no changes applied")
	}
	allowed := make(map[string]bool, len(conflicts))
	for _, path := range conflicts {
		allowed[filepath.ToSlash(path)] = true
	}
	list := exec.Command("git", "-C", worktree, "apply", "--numstat", "-z")
	list.Stdin = strings.NewReader(patch)
	out, err := list.Output()
	if err != nil {
		return fmt.Errorf("invalid conflict patch: %w", err)
	}
	files, err := parseNumstatPaths(out)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("model returned an empty patch")
	}
	for _, path := range files {
		if !allowed[path] {
			return fmt.Errorf("model patch touches a non-conflict file: %s", path)
		}
	}
	check := exec.Command("git", "-C", worktree, "apply", "--check", "--")
	check.Stdin = strings.NewReader(patch)
	if out, err := check.CombinedOutput(); err != nil {
		return fmt.Errorf("patch check failed: %s", strings.TrimSpace(string(out)))
	}
	apply := exec.Command("git", "-C", worktree, "apply", "--")
	apply.Stdin = strings.NewReader(patch)
	if out, err := apply.CombinedOutput(); err != nil {
		return fmt.Errorf("apply conflict patch: %s", strings.TrimSpace(string(out)))
	}
	for _, path := range files {
		fullPath, err := confinedConflictPath(worktree, path)
		if err != nil {
			return err
		}
		contents, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}
		text := string(contents)
		if strings.Contains(text, "<<<<<<< ") || strings.Contains(text, ">>>>>>> ") {
			return fmt.Errorf("conflict markers remain in %s; edit it manually before continuing", path)
		}
	}
	args := []string{"-C", worktree, "add", "-A", "--"}
	for _, path := range files {
		args = append(args, filepath.FromSlash(path))
	}
	stage := exec.Command("git", args...)
	if out, err := stage.CombinedOutput(); err != nil {
		return fmt.Errorf("stage resolved conflict files: %s", strings.TrimSpace(string(out)))
	}

	return nil
}

func parseNumstatPaths(output []byte) ([]string, error) {
	var paths []string
	for _, record := range bytes.Split(output, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		first := bytes.IndexByte(record, '\t')
		if first < 0 {
			return nil, fmt.Errorf("invalid git numstat output")
		}
		rest := record[first+1:]
		second := bytes.IndexByte(rest, '\t')
		if second < 0 || second == len(rest)-1 {
			return nil, fmt.Errorf("unsupported conflict patch path record")
		}
		paths = append(paths, filepath.ToSlash(string(rest[second+1:])))
	}
	return paths, nil
}

func confinedConflictPath(worktree, name string) (string, error) {
	if filepath.IsAbs(name) {
		return "", fmt.Errorf("absolute conflict path is not allowed: %q", name)
	}
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("conflict path escapes the CR worktree: %q", name)
	}
	root, err := filepath.EvalSymlinks(worktree)
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(root, clean)
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err == nil {
		candidate = resolved
	} else if parent, parentErr := filepath.EvalSymlinks(filepath.Dir(candidate)); parentErr == nil {
		candidate = filepath.Join(parent, filepath.Base(candidate))
	}
	rel, err := filepath.Rel(root, candidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("conflict path escapes the CR worktree: %q", name)
	}
	return candidate, nil
}

func init() {
	rootCmd.AddCommand(resolveConflictsCmd)
}
