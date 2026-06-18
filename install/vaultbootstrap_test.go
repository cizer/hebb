package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderBootstrapPinsReleaseVersion(t *testing.T) {
	s := string(RenderBootstrap("v0.5.0"))
	if !strings.Contains(s, `HEBB_VERSION="${HEBB_VERSION:-v0.5.0}"`) {
		t.Errorf("expected pinned HEBB_VERSION, got:\n%s", s)
	}
	for _, want := range []string{"#!/bin/sh", "install.sh | sh", `hebb install --vault "$VAULT"`, "--no-interaction"} {
		if !strings.Contains(s, want) {
			t.Errorf("bootstrap missing %q", want)
		}
	}
}

func TestRenderBootstrapDevIsUnpinned(t *testing.T) {
	if s := string(RenderBootstrap("0.0.0-dev (abc123)")); strings.Contains(s, "HEBB_VERSION") {
		t.Errorf("a dev build must not pin a version, got:\n%s", s)
	}
}

func TestWriteBootstrapExecutableAndIdempotent(t *testing.T) {
	dir := t.TempDir()
	changed, err := WriteBootstrap(dir, "v0.5.0")
	if err != nil || !changed {
		t.Fatalf("first write: changed=%v err=%v", changed, err)
	}
	fi, err := os.Stat(filepath.Join(dir, "bootstrap.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o100 == 0 {
		t.Error("bootstrap.sh should be executable")
	}
	if changed, _ := WriteBootstrap(dir, "v0.5.0"); changed {
		t.Error("re-writing the same version should be a no-op")
	}
	// A version change rewrites it so the pin tracks the installing binary.
	if changed, _ := WriteBootstrap(dir, "v0.6.0"); !changed {
		t.Error("a version change should rewrite bootstrap.sh")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "bootstrap.sh")); !strings.Contains(string(b), "v0.6.0") {
		t.Error("bootstrap.sh should pin the new version after a re-install")
	}
}
