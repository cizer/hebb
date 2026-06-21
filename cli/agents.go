package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// agentSel is which per-vault agent wirings to apply. The Claude Code *plugin*
// is deliberately not here: it installs once, user-level, via the marketplace,
// not per vault. Option 3 is the plugin-less per-vault .mcp.json fallback.
type agentSel struct {
	Codex         bool
	ClaudeDesktop bool
	MCPJSON       bool
}

func (s agentSel) any() bool { return s.Codex || s.ClaudeDesktop || s.MCPJSON }

// parseAgentSelection turns a line like "1,3" or "1 2" into a selection. Unknown
// tokens are ignored; empty input selects nothing.
func parseAgentSelection(in string) agentSel {
	var s agentSel
	for _, tok := range strings.FieldsFunc(in, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
		switch strings.TrimSpace(tok) {
		case "1":
			s.Codex = true
		case "2":
			s.ClaudeDesktop = true
		case "3":
			s.MCPJSON = true
		}
	}
	return s
}

// promptAgents prints the picker and reads one line of input. It is the only
// interactive bit; the parsing it delegates to is pure and unit-tested.
func promptAgents(in io.Reader, out io.Writer) agentSel {
	fmt.Fprintln(out, "\nWire this vault to your agents? (the Claude Code plugin installs once, separately)")
	fmt.Fprintln(out, "  1) Codex CLI        (~/.codex/config.toml)")
	fmt.Fprintln(out, "  2) Claude Desktop   (claude_desktop_config.json)")
	fmt.Fprintln(out, "  3) Claude Code      (per-vault .mcp.json, plugin-less)")
	fmt.Fprint(out, "Enter numbers (e.g. 1,2), or press Enter to skip: ")
	line, _ := bufio.NewReader(in).ReadString('\n')
	// Separate the prompt from the install report that follows. A piped/captured
	// stdin echoes no newline of its own, so without this the report's first line
	// runs onto the prompt line.
	fmt.Fprintln(out)
	return parseAgentSelection(line)
}

// stdinIsInteractive reports whether stdin is a terminal, so we never block a
// piped/CI/headless `hebb install` waiting on a prompt.
func stdinIsInteractive() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
