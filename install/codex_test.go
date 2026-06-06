package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// TestCodexConfigParsesAsValidTOML proves the hand-rendered block (inline env
// table, quoted paths) is accepted by a real TOML parser and decodes to the
// pinned values, even when the vault path contains a space.
func TestCodexConfigParsesAsValidTOML(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.toml")
	vault := "/vaults/My Vault"
	if _, err := WriteCodexConfig(cfg, "hebb", "hebb", vault); err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		MCPServers map[string]struct {
			Command string            `toml:"command"`
			Args    []string          `toml:"args"`
			Cwd     string            `toml:"cwd"`
			Env     map[string]string `toml:"env"`
			Timeout int               `toml:"startup_timeout_sec"`
		} `toml:"mcp_servers"`
	}
	if _, err := toml.DecodeFile(cfg, &parsed); err != nil {
		t.Fatalf("rendered config is not valid TOML: %v", err)
	}
	s, ok := parsed.MCPServers["hebb"]
	if !ok {
		t.Fatalf("hebb server not decoded; got %v", parsed.MCPServers)
	}
	if s.Command != "hebb" || len(s.Args) != 1 || s.Args[0] != "mcp" {
		t.Errorf("command/args wrong: %q %v", s.Command, s.Args)
	}
	if s.Cwd != vault || s.Env["HEBB_VAULT"] != vault {
		t.Errorf("vault not pinned: cwd=%q env=%v", s.Cwd, s.Env)
	}
	if s.Timeout != DefaultCodexStartupTimeoutSec {
		t.Errorf("startup_timeout_sec = %d, want %d", s.Timeout, DefaultCodexStartupTimeoutSec)
	}
}

func TestRenderCodexServerBlock(t *testing.T) {
	block := RenderCodexServer("hebb", "hebb", "/vaults/work")
	for _, want := range []string{
		"[mcp_servers.hebb]",
		`command = "hebb"`,
		`args = ["mcp"]`,
		`cwd = "/vaults/work"`,
		`HEBB_VAULT = "/vaults/work"`,
		"startup_timeout_sec",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("rendered block missing %q:\n%s", want, block)
		}
	}
	// The block is self-contained: env is an inline table, so nothing trails
	// into a sub-table that a surgical merge could orphan.
	if strings.Contains(block, "[mcp_servers.hebb.env]") {
		t.Errorf("env should be an inline table, got a sub-table:\n%s", block)
	}
}

func TestWriteCodexConfigCreatesFile(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "nested", "config.toml") // parent absent
	status, err := WriteCodexConfig(cfg, "hebb", "hebb", "/vaults/work")
	if err != nil {
		t.Fatalf("WriteCodexConfig: %v", err)
	}
	if status != "created" {
		t.Errorf("status = %q, want created", status)
	}
	b, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.Contains(string(b), "[mcp_servers.hebb]") {
		t.Errorf("config missing server block:\n%s", b)
	}
}

func TestWriteCodexConfigIdempotent(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if _, err := WriteCodexConfig(cfg, "hebb", "hebb", "/vaults/work"); err != nil {
		t.Fatal(err)
	}
	status, err := WriteCodexConfig(cfg, "hebb", "hebb", "/vaults/work")
	if err != nil {
		t.Fatal(err)
	}
	if status != "unchanged" {
		t.Errorf("second write status = %q, want unchanged", status)
	}
}

// The defining test: a user's hand-maintained config (other servers, comments,
// top-level keys) must survive. Only the hebb block is touched.
func TestWriteCodexConfigPreservesExistingContent(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.toml")
	original := `# my codex config
model = "o4-mini"

[mcp_servers.github]
command = "github-mcp-server"
args = ["stdio"]

# keep this comment
[mcp_servers.linear]
command = "linear-mcp"
`
	if err := os.WriteFile(cfg, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	status, err := WriteCodexConfig(cfg, "hebb", "hebb", "/vaults/work")
	if err != nil {
		t.Fatal(err)
	}
	if status != "created" { // hebb block newly added
		t.Errorf("status = %q, want created", status)
	}

	got, _ := os.ReadFile(cfg)
	s := string(got)
	for _, want := range []string{
		`model = "o4-mini"`,
		"[mcp_servers.github]",
		`command = "github-mcp-server"`,
		"# keep this comment",
		"[mcp_servers.linear]",
		"[mcp_servers.hebb]", // ours appended
	} {
		if !strings.Contains(s, want) {
			t.Errorf("merged config lost/omitted %q:\n%s", want, s)
		}
	}
}

// Re-pointing an existing hebb block to a new vault replaces only that block.
func TestWriteCodexConfigRepointsOwnBlock(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.toml")
	original := `[mcp_servers.hebb]
command = "hebb"
args = ["mcp"]
cwd = "/vaults/old"
env = { HEBB_VAULT = "/vaults/old" }
startup_timeout_sec = 20

[mcp_servers.other]
command = "other"
`
	if err := os.WriteFile(cfg, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	status, err := WriteCodexConfig(cfg, "hebb", "hebb", "/vaults/new")
	if err != nil {
		t.Fatal(err)
	}
	if status != "updated" {
		t.Errorf("status = %q, want updated", status)
	}
	got, _ := os.ReadFile(cfg)
	s := string(got)
	if strings.Contains(s, "/vaults/old") {
		t.Errorf("old vault path should be gone:\n%s", s)
	}
	if !strings.Contains(s, `cwd = "/vaults/new"`) {
		t.Errorf("new vault path missing:\n%s", s)
	}
	if !strings.Contains(s, "[mcp_servers.other]") {
		t.Errorf("sibling server block must survive:\n%s", s)
	}
	// Exactly one hebb header (no duplication).
	if n := strings.Count(s, "[mcp_servers.hebb]"); n != 1 {
		t.Errorf("expected exactly one hebb block, got %d:\n%s", n, s)
	}
}
