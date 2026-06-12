package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cizer/hebb/core"
	"github.com/spf13/cobra"
)

// notifyClient is the HTTP client used by the notify subcommand. It is a
// package-level variable so tests can substitute an httptest transport.
var notifyClient = &http.Client{Timeout: 30 * time.Second}

// notifyCmd is the headless notification sender: it resolves the webhook URL
// from $HEBB_NOTIFY_URL or the vault's [notify] url, then POSTs a short
// summary text as application/json with a "text" field. It exits non-zero on
// any non-2xx response so callers can log the failure. Summary text is read
// from args joined with spaces, or from stdin when no args are given.
//
// Wire contract: POST application/json, body {"text": "..."}, single attempt,
// no retry. The URL and the body text are never echoed to logs or stdout.
func notifyCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "notify [summary text]",
		Short: "Post a short summary to a configured webhook",
		Long: "Resolve the webhook URL from $HEBB_NOTIFY_URL or the vault's [notify]\n" +
			"url, then POST a {\"text\": \"...\"} JSON body. Summary text is read from\n" +
			"args (joined) or stdin. Exits non-zero on HTTP failure. The URL and\n" +
			"body are never echoed to logs or standard output.",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Resolve the vault config to get [notify] settings.
			vc, _, _ := core.LoadVaultConfig(resolveVaultPath())
			url := vc.Notify.ResolveURL()
			if url == "" {
				return fmt.Errorf("no webhook URL: set $HEBB_NOTIFY_URL or [notify] url in .hebb/config.toml")
			}
			if !vc.Notify.Enabled {
				// Allow the command to be called explicitly even when enabled=false, but
				// only when a URL is present (the check above already gates on a URL).
				// The enabled flag gates automatic calls from digest/update; a direct
				// invocation like `hebb notify "msg"` should work regardless.
			}

			// Resolve text from args or stdin.
			var text string
			if len(args) > 0 {
				text = strings.Join(args, " ")
			} else {
				b, err := io.ReadAll(cmd.InOrStdin())
				if err != nil {
					return fmt.Errorf("read stdin: %w", err)
				}
				text = strings.TrimSpace(string(b))
			}

			return SendNotification(url, text)
		},
	}
	return c
}

// SendNotification POSTs {"text": text} as application/json to url using the
// notifyClient. It is a single attempt with no retry. The URL and text are
// never logged. A non-2xx response is an error so callers can choose to log
// and continue rather than abort their own work.
func SendNotification(url, text string) error {
	body, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return fmt.Errorf("notify: marshal body: %w", err)
	}
	resp, err := notifyClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("notify: POST failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("notify: server returned %s", resp.Status)
	}
	return nil
}

// resolveVaultPath returns the effective vault path for notify: flagVault if
// set, otherwise the flagDB-derived path (same resolution openVault uses, but
// without opening the DB since notify does not need the index).
func resolveVaultPath() string {
	if flagVault != "" {
		return flagVault
	}
	return ""
}
