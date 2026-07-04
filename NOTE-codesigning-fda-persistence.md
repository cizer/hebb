# NOTE: ad-hoc signing breaks Full Disk Access across updates

Filed 2026-06-21. Companion to the launchd/TCC work that removed the shell
wrapper from the scheduled jobs (see `install/doctor.go` `checkLaunchdTCC`).

## Problem

The `hebb` binary is **ad-hoc signed** (linker-signed, no Developer ID):

```
$ codesign -dv ~/.local/bin/hebb
Identifier=a.out
CodeDirectory ... flags=0x20002(adhoc,linker-signed)
Signature=adhoc
TeamIdentifier=not set
```

The scheduled launchd jobs (daily-digest, action-review, update-check) now run
the `hebb` binary directly rather than through a shell wrapper, so they read the
vault under a TCC-protected location (`~/Documents/...`). That read requires the
binary to hold Full Disk Access (or Documents-folder access).

Because the binary is ad-hoc signed, macOS TCC cannot key the grant to a stable
signing identity (there is no Developer ID / Team designated requirement). It
keys the grant to the binary's **cdhash** instead. Every `hebb update` replaces
the binary with a fresh build, the cdhash changes, and the existing grant stops
applying. The System Settings entry can still appear enabled while no longer
matching the new binary, so the failure is silent: the next 08:00 scheduled run
blocks on the first protected read with `getcwd: Operation not permitted` and the
digest never updates.

Path stability is fine: `~/.local/bin/hebb` is a real file at a fixed path (not a
versioned symlink), so updates do not create a new TCC entry. The entry just
needs refreshing (toggle off/on, or remove and re-add) after each update.

## Impact

- Every `hebb update` can silently break the scheduled jobs on macOS until the
  user re-grants Full Disk Access.
- The break is invisible without inspecting the job log or noticing a stale
  digest, which is poor for an unattended automation.

## Fix

Sign `hebb` with a **Developer ID Application** certificate and **notarise** the
release binaries. Then TCC matches the grant against the stable designated
requirement (identifier + team), and the grant survives in-place updates: no
re-granting, ever.

Work involved:
- Apple Developer ID Application certificate in the release/CI signing keychain.
- `codesign --options runtime --sign "Developer ID Application: ..."` the binary
  during release (see `RELEASING.md`).
- Notarise (`notarytool submit --wait`) and `stapler staple` the artifact.
- Verify: `spctl -a -vvv -t install` / `codesign -dv` shows a Developer ID
  authority and a Team identifier, not `adhoc`.

## Interim mitigations (until signed)

- `hebb doctor` now has a `full-disk-access` check that warns when the running
  process cannot read a TCC-protected location, so a broken post-update state
  surfaces on the next `hebb doctor` rather than silently (added 2026-06-21).
- After any `hebb update` on macOS, re-grant Full Disk Access to
  `~/.local/bin/hebb` if the scheduled digest stops refreshing.

## References

- `install/doctor.go` — `checkLaunchdTCC` (shell-wrapper lint) and
  `checkFullDiskAccess` (grant probe).
- Daily digest job: `local.hebb.onevault.daily-digest`, Mon-Fri 08:00.
