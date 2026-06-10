package core

import (
	"fmt"
	"os/exec"
	"strings"
)

// Git mode keeps a vault's markdown in sync with a remote: pull before work,
// commit and push after. hebb is not in the path of individual file writes
// (the agent and Obsidian edit on disk directly), so sync happens at coarse
// boundaries: process startup (pull), a debounced quiet period in the watcher
// (commit + push), and the explicit `hebb sync` command. All operations shell
// out to the user's git (honouring their existing remotes and credentials)
// rather than carrying a git library, and never force-push or auto-resolve a
// conflict.

// SyncOptions selects which git steps GitSync performs.
type SyncOptions struct {
	Pull    bool
	Commit  bool
	Push    bool
	Message string // commit message; defaults when empty
}

// SyncResult reports what GitSync actually did.
type SyncResult struct {
	Pulled    bool
	Committed bool
	Pushed    bool
}

const defaultCommitMessage = "hebb: sync vault"

// IsGitRepo reports whether vaultPath is inside a git work tree.
func IsGitRepo(vaultPath string) bool {
	out, err := gitOut(vaultPath, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(out) == "true"
}

// GitSync commits local changes, pulls (rebasing onto the upstream), and pushes,
// per opts. Commit runs before pull so the rebase has a clean tree; pull runs
// before push so a fast-forward is likely. It is a no-op where there is nothing
// to do (clean tree, no upstream). It never force-pushes; a pull that conflicts
// is aborted and surfaced as an error so the user resolves it by hand.
func GitSync(vaultPath string, opts SyncOptions) (SyncResult, error) {
	var r SyncResult
	if !IsGitRepo(vaultPath) {
		return r, fmt.Errorf("not a git repository: %s", vaultPath)
	}
	msg := opts.Message
	if msg == "" {
		msg = defaultCommitMessage
	}
	if opts.Commit {
		committed, err := GitCommitAll(vaultPath, msg)
		if err != nil {
			return r, err
		}
		r.Committed = committed
	}
	if opts.Pull && hasUpstream(vaultPath) {
		if err := GitPull(vaultPath); err != nil {
			return r, err
		}
		r.Pulled = true
	}
	if opts.Push {
		if err := GitPush(vaultPath); err != nil {
			return r, err
		}
		r.Pushed = true
	}
	return r, nil
}

// GitPull runs `git pull --rebase --autostash`. On a conflict (or any rebase
// failure) it aborts the rebase to restore a clean working tree and returns an
// error, so hebb never leaves the vault mid-rebase or auto-resolves. With no
// upstream configured it is a no-op.
func GitPull(vaultPath string) error {
	if !hasUpstream(vaultPath) {
		return nil
	}
	if err := gitRun(vaultPath, "pull", "--rebase", "--autostash"); err != nil {
		_ = gitRun(vaultPath, "rebase", "--abort") // best-effort; no-op if not rebasing
		return fmt.Errorf("git pull --rebase failed (likely a conflict); resolve it by hand: %w", err)
	}
	return nil
}

// GitCommitAll stages every change and commits with msg. It reports whether a
// commit was made: a clean tree is a no-op (committed=false), not an error.
func GitCommitAll(vaultPath, msg string) (bool, error) {
	status, err := gitOut(vaultPath, "status", "--porcelain")
	if err != nil {
		return false, fmt.Errorf("git status: %w", err)
	}
	if strings.TrimSpace(status) == "" {
		return false, nil
	}
	if err := gitRun(vaultPath, "add", "-A"); err != nil {
		return false, err
	}
	if err := gitRun(vaultPath, "commit", "-m", msg); err != nil {
		return false, err
	}
	return true, nil
}

// GitPush pushes the current branch to its upstream.
func GitPush(vaultPath string) error {
	return gitRun(vaultPath, "push")
}

// hasUpstream reports whether the current branch has a configured upstream.
func hasUpstream(vaultPath string) bool {
	_, err := gitOut(vaultPath, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	return err == nil
}

func gitRun(vaultPath string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = vaultPath
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func gitOut(vaultPath string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = vaultPath
	out, err := cmd.Output()
	return string(out), err
}
