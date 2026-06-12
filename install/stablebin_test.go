package install

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestStableBinPathPrefersSymlinkIntoCellar(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test uses POSIX symlinks")
	}
	dir := t.TempDir()
	// Simulate a Homebrew layout: the real binary lives in a versioned Cellar
	// dir; a stable symlink points at it.
	cellar := filepath.Join(dir, "Cellar", "hebb", "0.2.0", "bin")
	if err := os.MkdirAll(cellar, 0o755); err != nil {
		t.Fatal(err)
	}
	realBin := filepath.Join(cellar, "hebb")
	if err := os.WriteFile(realBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	stable := filepath.Join(dir, "bin", "hebb")
	if err := os.MkdirAll(filepath.Dir(stable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realBin, stable); err != nil {
		t.Fatal(err)
	}

	// The executable hebb sees is the Cellar path. With the stable symlink among
	// the candidates and resolving to the same binary, it is preferred.
	got := StableBinPath(realBin, []string{stable})
	if got != stable {
		t.Errorf("StableBinPath = %q, want the stable symlink %q", got, stable)
	}
}

func TestStableBinPathKeepsExeWhenNoCandidateMatches(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test uses POSIX symlinks")
	}
	dir := t.TempDir()
	cellar := filepath.Join(dir, "Cellar", "hebb", "0.2.0", "bin")
	if err := os.MkdirAll(cellar, 0o755); err != nil {
		t.Fatal(err)
	}
	realBin := filepath.Join(cellar, "hebb")
	if err := os.WriteFile(realBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A candidate that points at a different binary must not be preferred.
	other := filepath.Join(dir, "other-hebb")
	if err := os.WriteFile(other, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	stable := filepath.Join(dir, "bin", "hebb")
	if err := os.MkdirAll(filepath.Dir(stable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, stable); err != nil {
		t.Fatal(err)
	}

	got := StableBinPath(realBin, []string{stable})
	if got != realBin {
		t.Errorf("StableBinPath = %q, want the executable path %q when no candidate resolves to it", got, realBin)
	}
}

func TestStableBinPathKeepsExeWhenNotCellar(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test uses POSIX symlinks")
	}
	dir := t.TempDir()
	// A non-Cellar binary (e.g. a self-managed or go-install build): even if a
	// stable symlink exists, there is no versioned-path churn to dodge, so the
	// executable path is kept as-is.
	realBin := filepath.Join(dir, "hebb")
	if err := os.WriteFile(realBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	stable := filepath.Join(dir, "bin", "hebb")
	if err := os.MkdirAll(filepath.Dir(stable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realBin, stable); err != nil {
		t.Fatal(err)
	}
	got := StableBinPath(realBin, []string{stable})
	if got != realBin {
		t.Errorf("StableBinPath = %q, want the executable path %q for a non-Cellar binary", got, realBin)
	}
}
