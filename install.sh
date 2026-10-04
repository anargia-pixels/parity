#!/usr/bin/env bash
# Install or update the parity binary for this machine from the repo's
# release folder into ~/.local/bin (or $PARITY_INSTALL_DIR when set).
# Set PARITY_FORCE=1 to reinstall the current version.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/Doctorthe113/parity/main/install.sh | bash

set -euo pipefail

REPO="Doctorthe113/parity"
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

# Read release files through the API: raw.githubusercontent.com caches files
# for several minutes after a push. Binaries are gzip-compressed.
CONTENTS_URL="https://api.github.com/repos/${REPO}/contents/release"
ASSET="parity-${OS}-${ARCH}.gz"
RAW_HEADER="Accept: application/vnd.github.raw"

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

if ! latest=$(curl -fsSL --retry 3 -H "$RAW_HEADER" "$CONTENTS_URL/version"); then
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

download_size=$(curl -fsSL --retry 3 "$CONTENTS_URL/$ASSET" | grep -o '"size": *[0-9]*' | grep -o '[0-9]*$' || true)
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
curl -fL --retry 3 --progress-bar -H "$RAW_HEADER" -o "$tmp_gz" "$CONTENTS_URL/$ASSET"
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
