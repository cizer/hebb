package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestPromptAgentsReadsAndParses(t *testing.T) {
	var out bytes.Buffer
	sel := promptAgents(strings.NewReader("1,3\n"), &out)
	if !sel.Codex || sel.ClaudeDesktop || !sel.MCPJSON {
		t.Errorf("got %+v, want codex+mcpjson", sel)
	}
	if !strings.Contains(out.String(), "Codex CLI") || !strings.Contains(out.String(), "skip") {
		t.Errorf("prompt should list options and a skip hint, got:\n%s", out.String())
	}
}

func TestParseAgentSelection(t *testing.T) {
	cases := []struct {
		in                      string
		codex, desktop, mcpjson bool
	}{
		{"", false, false, false},
		{"1", true, false, false},
		{"2", false, true, false},
		{"3", false, false, true},
		{"1,2", true, true, false},
		{" 1 3 ", true, false, true},
		{"1,2,3", true, true, true},
		{"9,foo", false, false, false}, // unknown ignored
		{"2,2", false, true, false},    // dupes fine
	}
	for _, c := range cases {
		got := parseAgentSelection(c.in)
		if got.Codex != c.codex || got.ClaudeDesktop != c.desktop || got.MCPJSON != c.mcpjson {
			t.Errorf("parseAgentSelection(%q) = %+v, want codex=%v desktop=%v mcpjson=%v",
				c.in, got, c.codex, c.desktop, c.mcpjson)
		}
	}
}
