// Package launchd renders parameterised macOS launchd job definitions (plist
// files) and writes them to a LaunchAgents directory. It is the mechanism;
// callers supply the per-vault Job specs.
package launchd

import (
	"bytes"
	_ "embed"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

//go:embed job.plist.tmpl
var plistTemplate string

// xmlEscaper escapes the five XML metacharacters. Job fields (label, vault
// path, program args, env values, log path) flow into plist element content
// unescaped by text/template, so a vault path or name containing &, <, >, " or
// ' would otherwise produce malformed XML that launchctl rejects. All such
// characters are legal in macOS/Linux directory names.
var xmlEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	`"`, "&quot;",
	"'", "&apos;",
)

var tmpl = template.Must(template.New("plist").
	Funcs(template.FuncMap{"xml": xmlEscaper.Replace}).
	Parse(plistTemplate))

// EnvVar is one EnvironmentVariables entry. A slice (not a map) keeps rendering
// deterministic for idempotent writes.
type EnvVar struct {
	Key, Value string
}

// CalInterval is one StartCalendarInterval entry. A field set to -1 is omitted,
// so {Weekday: -1, Hour: 7, Minute: 3} means "07:03 every day".
type CalInterval struct {
	Weekday, Hour, Minute int
}

// Job is a single launchd agent definition.
type Job struct {
	Label      string
	Program    []string // ProgramArguments
	WorkingDir string
	EnvVars    []EnvVar
	RunAtLoad  bool
	KeepAlive  bool
	Throttle   int           // ThrottleInterval seconds; 0 omits the key
	Schedule   []CalInterval // 0 = none, 1 = single dict, >1 = array
	LogPath    string
}

// Render produces the plist bytes for a job.
func Render(j Job) ([]byte, error) {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, j); err != nil {
		return nil, err
	}
	out := buf.Bytes()
	if len(out) == 0 || out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	return out, nil
}

// WriteJobs renders each job to <dstDir>/<label>.plist, creating dstDir if
// needed. It returns the labels whose plist was created or changed (idempotent:
// unchanged files are left alone).
func WriteJobs(jobs []Job, dstDir string) ([]string, error) {
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return nil, err
	}
	var changed []string
	for _, j := range jobs {
		want, err := Render(j)
		if err != nil {
			return changed, err
		}
		path := filepath.Join(dstDir, j.Label+".plist")
		if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, want) {
			continue
		}
		if err := os.WriteFile(path, want, 0o644); err != nil {
			return changed, err
		}
		changed = append(changed, j.Label)
	}
	return changed, nil
}
