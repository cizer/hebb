# Releasing hebb

How a version of hebb is built and distributed. Test strategy is in
[TESTING.md](TESTING.md); this is the deploy step (Phase 4 Stage 3).

## Versioning

- **Dev builds** (`go build`) derive the version from Go's embedded VCS info:
  `0.0.0-dev (<short-rev>[-dirty])`. No manual bumping; each commit is
  distinguishable. (See `cli/version.go`.)
- **Releases** stamp the clean tag via `-ldflags "-X main.version=vX.Y.Z"`,
  which GoReleaser sets from the git tag. `hebb --version` then shows `vX.Y.Z`.

## Cut a release

1. Make sure `main` is green (CI Stage 1 + acceptance ran on the commit).
2. Tag and push:
   ```sh
   git tag v0.1.0
   git push origin v0.1.0
   ```
3. The `v*` tag triggers `.github/workflows/ci.yml`. Its `release` job is gated
   on `needs: [build, acceptance]`, so it runs only after Stage 1 and Stage 2
   pass on the tagged commit (the release never runs in parallel with, or ahead
   of, CI). The job then runs GoReleaser (`release --clean`).

GoReleaser (`.goreleaser.yaml`) cross-compiles `darwin`/`linux` × `amd64`/`arm64`
(CGO disabled — pure-Go SQLite), builds `.tar.gz` archives + `checksums.txt`, and
publishes a GitHub Release with an auto changelog. Everything runs on one Linux
runner because there is no cgo.

Dry-run locally without publishing:
```sh
goreleaser check                       # validate the config
goreleaser release --snapshot --clean  # build artifacts into ./dist, no upload
```

## How users install

- **Install script (one-liner):** `install.sh` at the repo root detects OS/arch,
  downloads the matching release archive, and installs to `~/.local/bin`. Public:
  `curl -fsSL https://raw.githubusercontent.com/cizer/hebb/main/install.sh | sh`.
  Private: `gh api repos/cizer/hebb/contents/install.sh -H "Accept: application/vnd.github.raw" | sh`
  (the script uses `gh` for the asset too). `HEBB_VERSION` / `HEBB_INSTALL_DIR` override.
- **Go users:** `go install github.com/cizer/hebb/cmd/hebb@latest` (set
  `GOPRIVATE=github.com/cizer/*` while private). Works the moment a tag exists.
- **Binary:** download the archive for their platform from the GitHub Release and
  put `hebb` on `PATH`.
- **Homebrew (optional):** disabled until set up. To enable: create a public
  `cizer/homebrew-hebb` repo, add a `HOMEBREW_TAP_GITHUB_TOKEN` secret (PAT with
  `repo` scope on the tap), uncomment the `brews:` block in `.goreleaser.yaml`
  and the `HOMEBREW_TAP_GITHUB_TOKEN` env line in the `release` job of
  `.github/workflows/ci.yml`. Then `brew install cizer/hebb/hebb`.
- **Claude Code plugin:** `/plugin marketplace add cizer/hebb` then
  `/plugin install hebb@hebb` (delivers MCP + skill config; still needs the
  binary on `PATH`).

## Before the first public release

- **Licence (Legal):** a public repo with no `LICENSE` is all-rights-reserved by
  default, so others can see but not freely use it. Choosing and adding a licence
  (and confirming redistribution of the Go dependencies' licences in the binary)
  is a decision for Legal, not something to set here.
- macOS Gatekeeper: unsigned binaries warn on download. For non-technical users,
  Apple Developer ID signing + notarization is a later add (GoReleaser supports
  it); `brew`/`go install` users are unaffected.
