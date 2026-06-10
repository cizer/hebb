package cli

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"runtime"
	"strings"

	hebbassets "github.com/cizer/hebb"
	"github.com/cizer/hebb/install"
	"github.com/spf13/cobra"
)

func updateCmd(version string) *cobra.Command {
	var checkOnly, force, skillsOnly bool
	var home, claudeSkillsDir, codexSkillsDir string
	c := &cobra.Command{
		Use:   "update",
		Short: "Check for and install a newer hebb release",
		Long: "Compare this binary to the latest GitHub release and, unless --check,\n" +
			"install it: download the matching asset, verify its checksum, and\n" +
			"atomically replace the binary, then re-apply any updated agent skills to\n" +
			"the skills dirs where they are already installed. Only self-replaces a\n" +
			"binary hebb owns (e.g. installed via install.sh); a Homebrew or 'go\n" +
			"install' binary is left to its package manager (--force overrides).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			if home == "" {
				home, _ = os.UserHomeDir()
			}

			// --skills-only: re-apply updated skills and stop. This is what the
			// freshly-installed binary is re-exec'd to do after a self-replace
			// (the updating process is still the old binary, so it cannot itself
			// materialise the new embedded skills).
			if skillsOnly {
				return refreshSkills(out, home, claudeSkillsDir, codexSkillsDir)
			}

			u := install.NewUpdater()
			tag, err := u.LatestTag()
			if err != nil {
				return fmt.Errorf("check for updates: %w", err)
			}
			if !install.NewerAvailable(version, tag) {
				fmt.Fprintf(out, "hebb %s is up to date (latest release %s)\n", version, tag)
				return nil
			}
			fmt.Fprintf(out, "a newer hebb is available: %s (current %s)\n", tag, version)
			if checkOnly {
				fmt.Fprintln(out, "run 'hebb update' to install it")
				return nil
			}

			exe, err := os.Executable()
			if err != nil {
				return err
			}
			if method := install.DetectInstallMethod(exe); method != install.SelfManaged && !force {
				fmt.Fprintf(out, "this hebb is %s-managed; update with:\n  %s\n", method, method.AdviseCommand())
				fmt.Fprintln(out, "(or re-run 'hebb update --force' to self-replace anyway)")
				return nil
			}

			bin, err := u.DownloadBinary(tag, runtime.GOOS, runtime.GOARCH)
			if err != nil {
				return fmt.Errorf("download %s: %w", tag, err)
			}
			if err := install.ReplaceBinary(exe, bin); err != nil {
				return fmt.Errorf("replace binary: %w", err)
			}
			fmt.Fprintf(out, "updated hebb %s -> %s (%s)\n", version, tag, exe)

			// Re-exec the new binary to apply any updated skills to the dirs that
			// already have them. Best-effort: a failure here doesn't undo the
			// upgrade, it just means skills need a manual re-install.
			refresh := exec.Command(exe, "update", "--skills-only", "--home", home)
			refresh.Stdout, refresh.Stderr = out, out
			if err := refresh.Run(); err != nil {
				fmt.Fprintln(out, "note: couldn't refresh skills automatically; run 'hebb install' (and 'hebb codex') to update them")
			}
			fmt.Fprintln(out, "skills also ship via the Claude Code plugin: run '/plugin update' to match.")
			fmt.Fprintln(out, "re-run 'hebb install' in a vault to refresh its launchd jobs and automation.")
			return nil
		},
	}
	c.Flags().BoolVar(&checkOnly, "check", false, "only report whether a newer release exists; don't install")
	c.Flags().BoolVar(&force, "force", false, "self-replace even a package-manager-managed binary")
	c.Flags().BoolVar(&skillsOnly, "skills-only", false, "only re-apply updated skills to where they are installed (used internally after a self-replace)")
	c.Flags().StringVar(&home, "home", "", "home dir holding the skills dirs (default: user home)")
	c.Flags().StringVar(&claudeSkillsDir, "claude-skills-dir", "", "Claude skills dir (default: <home>/.claude/skills)")
	c.Flags().StringVar(&codexSkillsDir, "codex-skills-dir", "", "Codex skills dir (default: <home>/.agents/skills)")
	_ = c.Flags().MarkHidden("skills-only")
	_ = c.Flags().MarkHidden("home")
	_ = c.Flags().MarkHidden("claude-skills-dir")
	_ = c.Flags().MarkHidden("codex-skills-dir")
	return c
}

// refreshSkills re-applies the embedded skills to the Claude and Codex skills
// dirs, but only where a skill is already installed (so it never newly installs
// for an agent the user doesn't use, or one they opted out of).
func refreshSkills(out io.Writer, home, claudeDir, codexDir string) error {
	skillsFS, err := fs.Sub(hebbassets.Assets, "plugin/skills")
	if err != nil {
		return err
	}
	if claudeDir == "" {
		claudeDir = install.ClaudeSkillsDir(home)
	}
	if codexDir == "" {
		codexDir = install.CodexSkillsDir(home)
	}
	for _, t := range []struct{ label, dir string }{{"claude", claudeDir}, {"codex", codexDir}} {
		names, err := install.UpdateManagedSkills(skillsFS, t.dir)
		if err != nil {
			return err
		}
		if len(names) > 0 {
			fmt.Fprintf(out, "  %-16s %s (%s)\n", t.label+" skills", strings.Join(names, ", "), t.dir)
		}
	}
	return nil
}
