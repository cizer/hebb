#!/bin/sh
# hebb installer: download the matching release binary and put it on your PATH.
#
# Public repo (anonymous):
#   curl -fsSL https://raw.githubusercontent.com/cizer/hebb/main/install.sh | sh
# Private repo (uses your gh auth to fetch both this script and the binary):
#   gh api repos/cizer/hebb/contents/install.sh -H "Accept: application/vnd.github.raw" | sh
#
# Env overrides:
#   HEBB_VERSION=v1.2.3    install a specific tag (default, or "latest": newest release)
#   HEBB_INSTALL_DIR=DIR   install location (default: ~/.local/bin)
set -eu

REPO="cizer/hebb"
BIN="hebb"
INSTALL_DIR="${HEBB_INSTALL_DIR:-$HOME/.local/bin}"

err() { echo "hebb-install: $*" >&2; exit 1; }

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
	x86_64 | amd64) arch=amd64 ;;
	arm64 | aarch64) arch=arm64 ;;
	*) err "unsupported architecture: $arch" ;;
esac
case "$os" in
	darwin | linux) ;;
	*) err "unsupported OS: $os" ;;
esac

# Prefer gh when it's installed and authed (works for a private repo); otherwise
# fall back to anonymous curl (public repo).
have_gh=0
if command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then have_gh=1; fi

tag="${HEBB_VERSION:-}"
# Empty or the literal "latest" both mean "resolve the newest release", so
# HEBB_VERSION=latest works (as a one-liner override or in a vault bootstrap.sh).
if [ -z "$tag" ] || [ "$tag" = "latest" ]; then
	if [ "$have_gh" -eq 1 ]; then
		tag=$(gh release view --repo "$REPO" --json tagName --jq .tagName 2>/dev/null) || true
	else
		tag=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" |
			sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)
	fi
fi
[ -n "$tag" ] || err "could not determine the latest release (set HEBB_VERSION, or check repo access)"

ver=${tag#v}
asset="${BIN}_${ver}_${os}_${arch}.tar.gz"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "hebb-install: fetching $asset ($tag)"
if [ "$have_gh" -eq 1 ]; then
	gh release download "$tag" --repo "$REPO" --pattern "$asset" --dir "$tmp" --clobber ||
		err "download failed (is asset $asset in release $tag?)"
else
	curl -fsSL "https://github.com/$REPO/releases/download/$tag/$asset" -o "$tmp/$asset" ||
		err "download failed; if the repo is private, install gh (gh auth login) and re-run"
fi

tar xzf "$tmp/$asset" -C "$tmp" || err "extract failed"
mkdir -p "$INSTALL_DIR"
install -m 0755 "$tmp/$BIN" "$INSTALL_DIR/$BIN"
echo "hebb-install: installed $tag -> $INSTALL_DIR/$BIN"
"$INSTALL_DIR/$BIN" --version 2>/dev/null || true
case ":$PATH:" in
	*":$INSTALL_DIR:"*) ;;
	*) echo "hebb-install: add $INSTALL_DIR to your PATH (e.g. export PATH=\"$INSTALL_DIR:\$PATH\")" ;;
esac
