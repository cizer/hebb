package cli

import "testing"

func TestFormatDevVersion(t *testing.T) {
	cases := []struct {
		name     string
		base     string
		rev      string
		modified bool
		want     string
	}{
		{"release is left untouched", "v1.2.3", "deadbeefcafe", true, "v1.2.3"},
		{"dev with no vcs info falls back to base", devVersion, "", false, devVersion},
		{"dev gets the short revision", devVersion, "abcdef1234567890", false, devVersion + " (abcdef123456)"},
		{"short revision is not over-truncated", devVersion, "abc123", false, devVersion + " (abc123)"},
		{"dirty tree is marked", devVersion, "abcdef1234567890", true, devVersion + " (abcdef123456-dirty)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := formatDevVersion(c.base, c.rev, c.modified); got != c.want {
				t.Errorf("formatDevVersion(%q,%q,%v) = %q, want %q", c.base, c.rev, c.modified, got, c.want)
			}
		})
	}
}
