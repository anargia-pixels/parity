#!/usr/bin/env bash
# Install or update the parity binary for this machine from the latest
# GitHub release into ~/.local/bin (or $PARITY_INSTALL_DIR when set).
# Set PARITY_FORCE=1 to reinstall the current version.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/anargia-pixels/parity/main/install.sh | bash

set -euo pipefail

REPO="anargia-pixels/parity"
INSTALL_DIR="${PARITY_INSTALL_DIR:-$HOME/.local/bin}"
TARGET="$INSTALL_DIR/parity"
OS_NAME=$(uname -s)
MACHINE=$(uname -m)

case "$OS_NAME" in
  Linux) OS="linux" ;;
  Darwin) OS="darwin" ;;
  *)
    echo "error: unsupported operating system $OS_NAME" >&2
    exit 1
    ;;
esac

case "$MACHINE" in
  x86_64) ARCH="x64" ;;
  aarch64 | arm64) ARCH="arm64" ;;
  *)
    echo "error: unsupported architecture $MACHINE" >&2
    exit 1
    ;;
esac

# Release tags are parity versions. Binaries are gzip-compressed.
RELEASES_URL="https://github.com/${REPO}/releases"
ASSET="parity-${OS}-${ARCH}.gz"

format_size() {
  awk -v bytes="$1" 'BEGIN { printf "%.1f MB", bytes / 1048576 }'
}

# Prints the installed version, or nothing for builds that do not report one.
installed_version() {
  local output
  output=$("$1" --version 2>/dev/null) || return 0
  case "$output" in
    "parity "[0-9]*) echo "${output#parity }" ;;
  esac
}

# releases/latest redirects to the newest release's tag page.
latest_url=$(curl -fsSL --retry 3 -o /dev/null -w '%{url_effective}' "$RELEASES_URL/latest" || true)
latest=${latest_url##*/tag/}
if [ -z "$latest_url" ] || [ "$latest" = "$latest_url" ]; then
  echo "error: could not read the latest parity version from GitHub; try again later" >&2
  exit 1
fi
current=""
if [ -x "$TARGET" ]; then
  current=$(installed_version "$TARGET")
fi

if [ -n "$current" ] && [ "$current" = "$latest" ] && [ "${PARITY_FORCE:-}" != 1 ]; then
  echo "parity $latest is already up to date ($TARGET)"
  exit 0
fi

if [ "$current" = "$latest" ]; then
  echo "Reinstalling parity $latest (${OS}-${ARCH})"
elif [ -n "$current" ]; then
  echo "Updating parity $current → $latest (${OS}-${ARCH})"
elif [ -e "$TARGET" ]; then
  echo "Updating parity (older build without version info) → $latest (${OS}-${ARCH})"
else
  echo "Installing parity $latest (${OS}-${ARCH})"
fi

ASSET_URL="$RELEASES_URL/download/$latest/$ASSET"
download_size=$(curl -fsSIL --retry 3 "$ASSET_URL" | tr -d '\r' | awk 'tolower($1) == "content-length:" { size = $2 } END { print size }' || true)
if [ -n "$download_size" ]; then
  echo "Downloading $ASSET ($(format_size "$download_size"))"
else
  echo "Downloading $ASSET"
fi

mkdir -p "$INSTALL_DIR"
# Build the new binary beside the old one, then rename it into place. Writing
# over a running binary fails on Linux; a rename does not.
tmp_gz=$(mktemp "${TMPDIR:-/tmp}/parity-download.XXXXXX")
tmp_binary=$(mktemp "$INSTALL_DIR/.parity-new.XXXXXX")
trap 'rm -f "$tmp_gz" "$tmp_binary"' EXIT
curl -fL --retry 3 --progress-bar -o "$tmp_gz" "$ASSET_URL"
gunzip -c "$tmp_gz" > "$tmp_binary"
chmod 755 "$tmp_binary"

new_version=$(installed_version "$tmp_binary")
if [ "$new_version" != "$latest" ]; then
  echo "error: downloaded binary reports version '${new_version:-unknown}', expected $latest" >&2
  exit 1
fi
mv -f "$tmp_binary" "$TARGET"
binary_size=$(wc -c < "$TARGET" | tr -d ' ')
echo "Installed parity $latest to $TARGET ($(format_size "$binary_size"))"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo "note: add $INSTALL_DIR to your PATH to run 'parity' from anywhere" ;;
esac

# A running watcher keeps the old binary until it restarts.
if [ "$OS" = linux ] && systemctl --user is-active --quiet parity 2>/dev/null; then
  systemctl --user restart parity
  echo "Restarted the parity systemd service"
elif "$TARGET" status > /dev/null 2>&1; then
  echo "note: a parity watcher is still running the old version; restart it with: parity stop && parity watch"
fi
