package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitOrSkip(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

// initBare makes dir a bare repo whose HEAD is main, matching initRepo, so a
// clone checks out the branch the tests push to (not the host default).
func initBare(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "init", "--bare")
	runGit(t, dir, "symbolic-ref", "HEAD", "refs/heads/main")
}

// initRepo makes dir a git repo on branch main with a test identity, without
// assuming the host git's default branch name.
func initRepo(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "init")
	runGit(t, dir, "symbolic-ref", "HEAD", "refs/heads/main")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "hebb test")
	runGit(t, dir, "config", "commit.gpgsign", "false") // keep tests hermetic
}

func TestIsGitRepo(t *testing.T) {
	gitOrSkip(t)
	if IsGitRepo(t.TempDir()) {
		t.Error("plain dir reported as a git repo")
	}
	repo := t.TempDir()
	initRepo(t, repo)
	if !IsGitRepo(repo) {
		t.Error("git repo not detected")
	}
}

func TestGitSyncRejectsNonRepo(t *testing.T) {
	gitOrSkip(t)
	if _, err := GitSync(t.TempDir(), SyncOptions{Commit: true}); err == nil {
		t.Fatal("expected an error syncing a non-repo")
	}
}

func TestGitCommitAll(t *testing.T) {
	gitOrSkip(t)
	repo := t.TempDir()
	initRepo(t, repo)

	if committed, err := GitCommitAll(repo, "noop"); err != nil || committed {
		t.Fatalf("clean tree should be a no-op: committed=%v err=%v", committed, err)
	}
	if err := os.WriteFile(filepath.Join(repo, "a.md"), []byte("# A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if committed, err := GitCommitAll(repo, "add a"); err != nil || !committed {
		t.Fatalf("expected a commit: committed=%v err=%v", committed, err)
	}
	if committed, _ := GitCommitAll(repo, "again"); committed {
		t.Error("second commit on a clean tree should be a no-op")
	}
}

func TestGitSyncRoundTrip(t *testing.T) {
	gitOrSkip(t)
	remote := t.TempDir()
	initBare(t, remote)

	work := t.TempDir()
	initRepo(t, work)
	if err := os.WriteFile(filepath.Join(work, "note.md"), []byte("# Note\nv1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := GitCommitAll(work, "init"); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "remote", "add", "origin", remote)
	runGit(t, work, "push", "-u", "origin", "main")

	// Edit, then a full sync (commit + pull + push).
	if err := os.WriteFile(filepath.Join(work, "note.md"), []byte("# Note\nv2 canarytoken"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := GitSync(work, SyncOptions{Commit: true, Pull: true, Push: true})
	if err != nil {
		t.Fatalf("GitSync: %v", err)
	}
	if !res.Committed || !res.Pushed {
		t.Fatalf("expected committed+pushed, got %+v", res)
	}

	// A fresh clone of the remote must contain the pushed change.
	clones := t.TempDir()
	runGit(t, clones, "clone", remote, "c")
	b, _ := os.ReadFile(filepath.Join(clones, "c", "note.md"))
	if !strings.Contains(string(b), "canarytoken") {
		t.Fatalf("pushed change not present in remote: %q", b)
	}
}

func TestGitPullFetchesRemoteChange(t *testing.T) {
	gitOrSkip(t)
	remote := t.TempDir()
	initBare(t, remote)

	a := t.TempDir()
	initRepo(t, a)
	if err := os.WriteFile(filepath.Join(a, "n.md"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := GitCommitAll(a, "init"); err != nil {
		t.Fatal(err)
	}
	runGit(t, a, "remote", "add", "origin", remote)
	runGit(t, a, "push", "-u", "origin", "main")

	// A second clone pushes a change.
	cdir := t.TempDir()
	runGit(t, cdir, "clone", remote, "b")
	b := filepath.Join(cdir, "b")
	runGit(t, b, "config", "user.email", "b@example.com")
	runGit(t, b, "config", "user.name", "b")
	runGit(t, b, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(b, "n.md"), []byte("v2 frombob"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := GitCommitAll(b, "bob"); err != nil {
		t.Fatal(err)
	}
	runGit(t, b, "push")

	// The first repo pulls it.
	if err := GitPull(a); err != nil {
		t.Fatalf("GitPull: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(a, "n.md"))
	if !strings.Contains(string(got), "frombob") {
		t.Fatalf("pull did not fetch the remote change: %q", got)
	}
}

func TestGitPullNoUpstreamIsNoOp(t *testing.T) {
	gitOrSkip(t)
	repo := t.TempDir()
	initRepo(t, repo)
	if err := os.WriteFile(filepath.Join(repo, "a.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := GitCommitAll(repo, "init"); err != nil {
		t.Fatal(err)
	}
	if err := GitPull(repo); err != nil {
		t.Fatalf("pull with no upstream should be a no-op, got: %v", err)
	}
}
