package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Codex (the OpenAI CLI) consumes MCP servers from a user-global
// ~/.codex/config.toml under [mcp_servers.<name>]. Unlike Claude's plugin, that
// file is a single hand-maintained config, so hebb registers one vault per
// named server and merges surgically: it replaces or appends only its own block
// and leaves every other server, comment, and top-level key untouched. The
// vault is pinned via env.HEBB_VAULT (which ResolveVault honours over cwd) plus
// cwd, so the entry is deterministic regardless of where Codex launches it.

// DefaultCodexStartupTimeoutSec gives the pure-Go SQLite index time to open on
// a cold first run, above Codex's 10s default.
const DefaultCodexStartupTimeoutSec = 20

// RenderCodexServer returns the TOML block for a [mcp_servers.<name>] stdio
// entry pinned to vaultPath. env is an inline table so the whole entry is one
// contiguous block (no trailing sub-table a merge could orphan).
func RenderCodexServer(name, command, vaultPath string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[mcp_servers.%s]\n", codexKey(name))
	fmt.Fprintf(&b, "command = %s\n", tomlString(command))
	b.WriteString("args = [\"mcp\"]\n")
	fmt.Fprintf(&b, "cwd = %s\n", tomlString(vaultPath))
	fmt.Fprintf(&b, "env = { HEBB_VAULT = %s }\n", tomlString(vaultPath))
	fmt.Fprintf(&b, "startup_timeout_sec = %d\n", DefaultCodexStartupTimeoutSec)
	return b.String()
}

// WriteCodexConfig merges the hebb server block into the Codex config at
// configPath, creating the file (and parent dir) if absent. It is idempotent
// and non-destructive. Returns "created", "updated", or "unchanged".
func WriteCodexConfig(configPath, name, command, vaultPath string) (string, error) {
	block := RenderCodexServer(name, command, vaultPath)

	existing, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
			return "", err
		}
		header := "# Codex config. hebb manages the [mcp_servers." + codexKey(name) + "] block below.\n\n"
		if err := os.WriteFile(configPath, []byte(header+block), 0o644); err != nil {
			return "", err
		}
		return "created", nil
	}
	if err != nil {
		return "", err
	}

	merged, status := mergeCodexBlock(string(existing), name, block)
	if status == "unchanged" {
		return status, nil
	}
	if err := os.WriteFile(configPath, []byte(merged), 0o644); err != nil {
		return "", err
	}
	return status, nil
}

// mergeCodexBlock replaces an existing [mcp_servers.<name>] block, or appends
// one if absent, preserving everything else byte-for-byte. The block runs from
// its table header to the next table header ("[..." at the start of a line) or
// EOF. SplitAfter keeps the newlines, so join is exact and idempotent.
func mergeCodexBlock(content, name, block string) (string, string) {
	if !strings.HasSuffix(block, "\n") {
		block += "\n"
	}
	// Each part retains its trailing "\n"; the final part is "" when content
	// ends in a newline. Join("") reconstructs content exactly.
	parts := strings.SplitAfter(content, "\n")

	start := -1
	for i, p := range parts {
		if isCodexServerHeader(strings.TrimSuffix(p, "\n"), name) {
			start = i
			break
		}
	}

	if start == -1 {
		// Append after the existing content, separated by one blank line.
		base := content
		if base != "" && !strings.HasSuffix(base, "\n") {
			base += "\n"
		}
		if base != "" && !strings.HasSuffix(base, "\n\n") {
			base += "\n"
		}
		return base + block, "created"
	}

	// End at the next table header after the block, else EOF.
	end := len(parts)
	for i := start + 1; i < len(parts); i++ {
		if strings.HasPrefix(strings.TrimSpace(parts[i]), "[") {
			end = i
			break
		}
	}

	before := strings.Join(parts[:start], "")
	after := strings.Join(parts[end:], "")
	// Keep a blank line between our block and a following section (the old
	// separator lived inside the replaced range).
	sep := ""
	if strings.TrimSpace(after) != "" {
		sep = "\n"
	}

	merged := before + block + sep + after
	if merged == content {
		return content, "unchanged"
	}
	return merged, "updated"
}

// isCodexServerHeader reports whether a line is the table header for our server,
// in either bare or quoted form ([mcp_servers.hebb] / [mcp_servers."hebb"]).
func isCodexServerHeader(line, name string) bool {
	t := strings.TrimSpace(line)
	return t == "[mcp_servers."+name+"]" || t == "[mcp_servers.\""+name+"\"]"
}

// codexKey returns the table-key form of a server name. Bare keys (the common
// case, e.g. "hebb") are used as-is; anything with dots or unusual characters
// is quoted so the header stays valid TOML.
func codexKey(name string) string {
	bare := true
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			bare = false
			break
		}
	}
	if bare && name != "" {
		return name
	}
	return tomlString(name)
}

// tomlString renders a TOML basic string, escaping the characters that would
// otherwise break out of the quotes. Vault paths rarely contain these, but a
// path with a space, quote, or backslash must still produce valid TOML.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
