package install

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in              string
		maj, min, patch int
		ok              bool
	}{
		{"v0.1.1", 0, 1, 1, true},
		{"1.2.3", 1, 2, 3, true},
		{"v2.0.0-rc1", 2, 0, 0, true},
		{"0.0.0-dev (abc123)", 0, 0, 0, true},
		{"garbage", 0, 0, 0, false},
		{"1.2", 0, 0, 0, false},
	}
	for _, c := range cases {
		maj, min, patch, ok := ParseVersion(c.in)
		if ok != c.ok || (ok && (maj != c.maj || min != c.min || patch != c.patch)) {
			t.Errorf("ParseVersion(%q) = %d.%d.%d ok=%v, want %d.%d.%d ok=%v",
				c.in, maj, min, patch, ok, c.maj, c.min, c.patch, c.ok)
		}
	}
}

func TestNewerAvailable(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"v0.1.0", "v0.1.1", true},
		{"v0.1.1", "v0.1.1", false},
		{"v0.2.0", "v0.1.9", false},
		{"v0.1.1", "v1.0.0", true},
		{"0.0.0-dev (abc)", "v0.1.1", false}, // dev build never auto-"updates"
		{"v0.1.0", "nonsense", false},
	}
	for _, c := range cases {
		if got := NewerAvailable(c.current, c.latest); got != c.want {
			t.Errorf("NewerAvailable(%q,%q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}

func TestAssetName(t *testing.T) {
	if got := AssetName("v0.1.1", "darwin", "arm64"); got != "hebb_0.1.1_darwin_arm64.tar.gz" {
		t.Errorf("AssetName = %q", got)
	}
}

func TestDetectInstallMethod(t *testing.T) {
	cases := []struct {
		path string
		want InstallMethod
	}{
		{"/Users/x/.local/bin/hebb", SelfManaged},
		{"/opt/homebrew/Cellar/hebb/0.1.1/bin/hebb", Homebrew},
		{"/home/x/go/bin/hebb", GoInstall},
	}
	for _, c := range cases {
		if got := DetectInstallMethod(c.path); got != c.want {
			t.Errorf("DetectInstallMethod(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

// makeArchive builds a .tar.gz containing a "hebb" file with the given body,
// plus its checksums.txt line.
func makeArchive(t *testing.T, body []byte) (targz []byte, checksums string, asset string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "hebb", Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	tw.Write(body)
	tw.Close()
	gz.Close()
	targz = buf.Bytes()
	asset = AssetName("0.1.1", "linux", "amd64")
	sum := sha256.Sum256(targz)
	checksums = fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), asset)
	return
}

func TestVerifyChecksumAndExtract(t *testing.T) {
	targz, checksums, asset := makeArchive(t, []byte("FAKE-HEBB-BINARY"))
	if err := VerifyChecksum(targz, checksums, asset); err != nil {
		t.Fatalf("VerifyChecksum: %v", err)
	}
	if err := VerifyChecksum([]byte("tampered"), checksums, asset); err == nil {
		t.Error("expected checksum mismatch for tampered data")
	}
	bin, err := BinaryFromArchive(targz)
	if err != nil || string(bin) != "FAKE-HEBB-BINARY" {
		t.Fatalf("BinaryFromArchive = %q, %v", bin, err)
	}
}

func TestReplaceBinary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hebb")
	if err := os.WriteFile(path, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceBinary(path, []byte("new-binary")); err != nil {
		t.Fatalf("ReplaceBinary: %v", err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "new-binary" {
		t.Fatalf("after replace = %q", b)
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm()&0o100 == 0 {
		t.Error("replaced binary is not executable")
	}
	// no temp files left behind
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("expected only the binary, got %d entries", len(entries))
	}
}

// TestUpdaterEndToEnd exercises LatestTag + DownloadBinary against a local
// server standing in for GitHub (no gh, HTTP only).
func TestUpdaterEndToEnd(t *testing.T) {
	targz, checksums, asset := makeArchive(t, []byte("v2-binary"))
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/me/hebb/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v0.1.1"}`)
	})
	mux.HandleFunc("/me/hebb/releases/download/v0.1.1/"+asset, func(w http.ResponseWriter, r *http.Request) {
		w.Write(targz)
	})
	mux.HandleFunc("/me/hebb/releases/download/v0.1.1/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, checksums)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	u := Updater{Repo: "me/hebb", APIBase: srv.URL, DownloadBase: srv.URL, HTTP: srv.Client(), UseGH: false}
	tag, err := u.LatestTag()
	if err != nil || tag != "v0.1.1" {
		t.Fatalf("LatestTag = %q, %v", tag, err)
	}
	bin, err := u.DownloadBinary(tag, "linux", "amd64")
	if err != nil {
		t.Fatalf("DownloadBinary: %v", err)
	}
	if string(bin) != "v2-binary" {
		t.Fatalf("downloaded binary = %q", bin)
	}
}

// TestUpdaterRejectsBadChecksum proves a tampered asset is refused.
func TestUpdaterRejectsBadChecksum(t *testing.T) {
	targz, _, asset := makeArchive(t, []byte("good"))
	mux := http.NewServeMux()
	mux.HandleFunc("/me/hebb/releases/download/v0.1.1/"+asset, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("TAMPERED")) // not the real asset
	})
	mux.HandleFunc("/me/hebb/releases/download/v0.1.1/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		sum := sha256.Sum256(targz) // checksum of the real asset
		fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), asset)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	u := Updater{Repo: "me/hebb", APIBase: srv.URL, DownloadBase: srv.URL, HTTP: srv.Client(), UseGH: false}
	if _, err := u.DownloadBinary("v0.1.1", "linux", "amd64"); err == nil {
		t.Fatal("expected checksum verification to reject a tampered asset")
	}
}
