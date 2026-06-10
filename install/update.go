package install

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DefaultRepo is the GitHub repo hebb releases come from.
const DefaultRepo = "cizer/hebb"

// hebb updates have two tracks. The binary (this code) comes from GitHub
// releases, the same artefacts install.sh fetches. Skills and the MCP server
// ship as the Claude Code plugin and are updated by Claude Code's marketplace,
// not here, so `hebb update` only ever reminds about those. Self-replacing a
// binary is deliberately gated: it happens only when hebb owns the binary
// (install.sh into ~/.local/bin); a Homebrew or `go install` binary is left to
// its package manager, and applying an update is opt-in.

// InstallMethod is how the running binary was installed, which decides whether
// `hebb update` may self-replace it.
type InstallMethod int

const (
	SelfManaged InstallMethod = iota // hebb owns the binary (e.g. install.sh)
	Homebrew                         // managed by `brew upgrade`
	GoInstall                        // managed by `go install ...@latest`
)

func (m InstallMethod) String() string {
	switch m {
	case Homebrew:
		return "homebrew"
	case GoInstall:
		return "go-install"
	default:
		return "self-managed"
	}
}

// AdviseCommand returns the command a user should run to update a binary hebb
// does not own, or "" when hebb may self-replace.
func (m InstallMethod) AdviseCommand() string {
	switch m {
	case Homebrew:
		return "brew upgrade hebb"
	case GoInstall:
		return "go install github.com/cizer/hebb/cmd/hebb@latest"
	default:
		return ""
	}
}

// DetectInstallMethod classifies the binary at exePath (symlinks resolved).
func DetectInstallMethod(exePath string) InstallMethod {
	p := exePath
	if rp, err := filepath.EvalSymlinks(exePath); err == nil {
		p = rp
	}
	switch {
	case strings.Contains(p, "/Cellar/") || strings.Contains(p, "/homebrew/"):
		return Homebrew
	case strings.Contains(p, "/go/bin/") || strings.Contains(p, "/go/pkg/"):
		return GoInstall
	default:
		if gobin := os.Getenv("GOBIN"); gobin != "" && strings.HasPrefix(p, gobin) {
			return GoInstall
		}
		return SelfManaged
	}
}

// ParseVersion parses vMAJOR.MINOR.PATCH (leading v optional; any pre-release or
// build suffix is ignored). ok is false for non-release strings (e.g. a dev
// build like "0.0.0-dev (abc123)"), so callers can refuse to compare them.
func ParseVersion(s string) (maj, min, patch int, ok bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	// Cut at the first space or '-' (pre-release / dev-revision suffix).
	if i := strings.IndexAny(s, " -+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	var nums [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return 0, 0, 0, false
		}
		nums[i] = n
	}
	return nums[0], nums[1], nums[2], true
}

// NewerAvailable reports whether latest is a strictly higher release than
// current. A dev build (version containing "dev") or any unparseable version
// returns false, so a dev binary is never auto-replaced by a release.
func NewerAvailable(current, latest string) bool {
	if strings.Contains(strings.ToLower(current), "dev") {
		return false
	}
	cMaj, cMin, cPatch, cok := ParseVersion(current)
	lMaj, lMin, lPatch, lok := ParseVersion(latest)
	if !cok || !lok {
		return false
	}
	switch {
	case lMaj != cMaj:
		return lMaj > cMaj
	case lMin != cMin:
		return lMin > cMin
	default:
		return lPatch > cPatch
	}
}

// AssetName is the release asset for a version/platform, matching GoReleaser and
// install.sh (e.g. hebb_0.1.1_darwin_arm64.tar.gz).
func AssetName(version, goos, goarch string) string {
	return fmt.Sprintf("hebb_%s_%s_%s.tar.gz", strings.TrimPrefix(version, "v"), goos, goarch)
}

// VerifyChecksum checks the sha256 of data against the entry for assetName in a
// GoReleaser checksums.txt body (lines of "<hex>  <name>").
func VerifyChecksum(data []byte, checksums, assetName string) error {
	want := ""
	for _, line := range strings.Split(checksums, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[1] == assetName {
			want = strings.ToLower(f[0])
			break
		}
	}
	if want == "" {
		return fmt.Errorf("no checksum for %s", assetName)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != want {
		return fmt.Errorf("checksum mismatch for %s: got %s, want %s", assetName, got, want)
	}
	return nil
}

// BinaryFromArchive extracts the file named "hebb" from a .tar.gz release asset.
func BinaryFromArchive(targz []byte) ([]byte, error) {
	gz, err := gzip.NewReader(strings.NewReader(string(targz)))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("no 'hebb' binary in archive")
		}
		if err != nil {
			return nil, err
		}
		if filepath.Base(hdr.Name) == "hebb" && hdr.Typeflag == tar.TypeReg {
			return io.ReadAll(tr)
		}
	}
}

// ReplaceBinary atomically replaces the file at path with data: it writes a
// temp file in the same directory, makes it executable, and renames it over the
// target. Replacing the path of a running binary is safe on Unix (the running
// process keeps the old inode).
func ReplaceBinary(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".hebb-update-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once renamed
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// Updater fetches release metadata and assets. URLs are fields so tests can
// point them at a local server; defaults target GitHub. When UseGH is set and
// the gh CLI is available and authed, it is used (works for a private repo),
// mirroring install.sh.
type Updater struct {
	Repo         string
	APIBase      string // default https://api.github.com
	DownloadBase string // default https://github.com
	HTTP         *http.Client
	UseGH        bool
}

// NewUpdater returns an Updater with GitHub defaults and gh enabled.
func NewUpdater() Updater {
	return Updater{
		Repo:         DefaultRepo,
		APIBase:      "https://api.github.com",
		DownloadBase: "https://github.com",
		HTTP:         &http.Client{Timeout: 30 * time.Second},
		UseGH:        true,
	}
}

// LatestTag returns the latest release tag (e.g. "v0.1.1").
func (u Updater) LatestTag() (string, error) {
	if u.UseGH && ghAvailable() {
		out, err := exec.Command("gh", "release", "view", "--repo", u.Repo, "--json", "tagName", "--jq", ".tagName").Output()
		if err == nil {
			if tag := strings.TrimSpace(string(out)); tag != "" {
				return tag, nil
			}
		}
		// fall through to HTTP on gh failure
	}
	body, err := u.get(u.APIBase + "/repos/" + u.Repo + "/releases/latest")
	if err != nil {
		return "", err
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &rel); err != nil {
		return "", err
	}
	if rel.TagName == "" {
		return "", fmt.Errorf("no tag_name in latest release response")
	}
	return rel.TagName, nil
}

// DownloadBinary fetches the asset and checksums for tag/platform, verifies the
// checksum, and returns the extracted hebb binary.
func (u Updater) DownloadBinary(tag, goos, goarch string) ([]byte, error) {
	ver := strings.TrimPrefix(tag, "v")
	asset := AssetName(ver, goos, goarch)
	targz, err := u.asset(tag, asset)
	if err != nil {
		return nil, err
	}
	sums, err := u.asset(tag, "checksums.txt")
	if err == nil {
		if err := VerifyChecksum(targz, string(sums), asset); err != nil {
			return nil, err
		}
	}
	return BinaryFromArchive(targz)
}

func (u Updater) asset(tag, name string) ([]byte, error) {
	return u.get(fmt.Sprintf("%s/%s/releases/download/%s/%s", u.DownloadBase, u.Repo, tag, name))
}

func (u Updater) get(url string) ([]byte, error) {
	c := u.HTTP
	if c == nil {
		c = http.DefaultClient
	}
	resp, err := c.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func ghAvailable() bool {
	if _, err := exec.LookPath("gh"); err != nil {
		return false
	}
	return exec.Command("gh", "auth", "status").Run() == nil
}
