package launchd

import (
	"encoding/xml"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func wellFormed(t *testing.T, b []byte) {
	t.Helper()
	d := xml.NewDecoder(strings.NewReader(string(b)))
	for {
		_, err := d.Token()
		if err != nil {
			if err.Error() == "EOF" {
				return
			}
			t.Fatalf("not well-formed XML: %v\n%s", err, b)
		}
	}
}

func TestRenderKeepAliveJob(t *testing.T) {
	j := Job{
		Label:      "local.hebb.work.web",
		Program:    []string{"/usr/local/bin/hebb", "serve", "--vault", "/v"},
		WorkingDir: "/v",
		EnvVars:    []EnvVar{{Key: "HEBB_WEB_PORT", Value: "4399"}},
		RunAtLoad:  true,
		KeepAlive:  true,
		Throttle:   10,
		LogPath:    "/logs/web.log",
	}
	b, err := Render(j)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	wellFormed(t, b)
	s := string(b)
	for _, want := range []string{
		"<string>local.hebb.work.web</string>",
		"<string>serve</string>",
		"<key>KeepAlive</key>",
		"<true/>",
		"<key>HEBB_WEB_PORT</key>",
		"<string>4399</string>",
		"<integer>10</integer>",
		"<string>/logs/web.log</string>",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("rendered plist missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "StartCalendarInterval") {
		t.Error("keepalive job should have no schedule")
	}
}

func TestRenderScheduledJobArray(t *testing.T) {
	var days []CalInterval
	for wd := 1; wd <= 5; wd++ {
		days = append(days, CalInterval{Weekday: wd, Hour: 8, Minute: 0})
	}
	j := Job{
		Label:    "local.hebb.work.daily-digest",
		Program:  []string{"/v/automation/digest.sh"},
		Schedule: days,
		LogPath:  "/logs/digest.log",
	}
	b, err := Render(j)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	wellFormed(t, b)
	s := string(b)
	if !strings.Contains(s, "<key>StartCalendarInterval</key>") {
		t.Error("missing schedule")
	}
	if !strings.Contains(s, "<array>") {
		t.Error("multi-entry schedule should render as an array")
	}
	if strings.Count(s, "<key>Weekday</key>") != 5 {
		t.Errorf("want 5 weekday entries, got %d", strings.Count(s, "<key>Weekday</key>"))
	}
	if !strings.Contains(s, "<false/>") {
		t.Error("RunAtLoad should default to false for scheduled job")
	}
}

func TestRenderSingleScheduleIsDict(t *testing.T) {
	j := Job{
		Label:    "local.hebb.work.action-review",
		Program:  []string{"python3", "/v/automation/review.py"},
		Schedule: []CalInterval{{Weekday: -1, Hour: 7, Minute: 3}},
		LogPath:  "/logs/review.log",
	}
	b, err := Render(j)
	if err != nil {
		t.Fatal(err)
	}
	wellFormed(t, b)
	s := string(b)
	if strings.Contains(s, "<array>\n        <dict>") {
		t.Error("single schedule should be a bare <dict>, not an array")
	}
	if strings.Contains(s, "<key>Weekday</key>") {
		t.Error("Weekday=-1 should be omitted")
	}
	if !strings.Contains(s, "<key>Hour</key><integer>7</integer>") {
		t.Errorf("missing hour:\n%s", s)
	}
}

// TestRenderedPlistsPassPlutil validates output against the real plist parser
// where available (macOS), so we know launchd itself will accept it.
func TestRenderedPlistsPassPlutil(t *testing.T) {
	plutil, err := exec.LookPath("plutil")
	if err != nil {
		t.Skip("plutil not available")
	}
	jobs := []Job{
		{Label: "ka", Program: []string{"hebb", "serve"}, RunAtLoad: true, KeepAlive: true, Throttle: 10, EnvVars: []EnvVar{{"P", "1"}}, LogPath: "/l"},
		{Label: "daily", Program: []string{"s.sh"}, Schedule: []CalInterval{{1, 8, 0}, {2, 8, 0}}, LogPath: "/l"},
		{Label: "once", Program: []string{"r.py"}, Schedule: []CalInterval{{-1, 7, 3}}, LogPath: "/l"},
	}
	dir := t.TempDir()
	for _, j := range jobs {
		b, err := Render(j)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, j.Label+".plist")
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command(plutil, "-lint", path).CombinedOutput(); err != nil {
			t.Errorf("plutil rejected %s: %v\n%s\n%s", j.Label, err, out, b)
		}
	}
}

// TestRenderEscapesXMLMetacharacters guards against malformed plists when a
// vault path or name contains XML metacharacters (all legal in directory
// names). The raw values must not appear; the escaped forms must.
func TestRenderEscapesXMLMetacharacters(t *testing.T) {
	path := "/Users/me/R&D <vault> \"x\" 'y'"
	j := Job{
		Label:      "local.hebb.r-d.web",
		Program:    []string{"hebb", "serve", "--vault", path},
		WorkingDir: path,
		EnvVars:    []EnvVar{{Key: "HEBB_VAULT", Value: path}},
		RunAtLoad:  true,
		LogPath:    path + "/web.log",
	}
	b, err := Render(j)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	wellFormed(t, b) // would fail on unescaped & or <
	s := string(b)
	if strings.Contains(s, "R&D <vault>") {
		t.Errorf("raw XML metacharacters leaked into plist:\n%s", s)
	}
	for _, want := range []string{"R&amp;D", "&lt;vault&gt;", "&quot;x&quot;", "&apos;y&apos;"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing escaped sequence %q:\n%s", want, s)
		}
	}
}

func TestWriteJobsIdempotent(t *testing.T) {
	dir := t.TempDir()
	jobs := []Job{{
		Label:   "local.hebb.work.web",
		Program: []string{"hebb", "serve"},
		LogPath: "/logs/web.log",
	}}
	changed, err := WriteJobs(jobs, dir)
	if err != nil {
		t.Fatalf("WriteJobs: %v", err)
	}
	if len(changed) != 1 {
		t.Errorf("first write changed = %v, want 1 entry", changed)
	}
	path := filepath.Join(dir, "local.hebb.work.web.plist")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("plist not written: %v", err)
	}
	changed, err = WriteJobs(jobs, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 0 {
		t.Errorf("re-write changed = %v, want none (idempotent)", changed)
	}
}
