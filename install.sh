#!/usr/bin/env bash
# Install the parity binary for this machine's architecture from the repo's
# release folder into ~/.local/bin (or $PARITY_INSTALL_DIR when set).
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/Doctorthe113/parity/main/install.sh | bash

set -euo pipefail

REPO="Doctorthe113/parity"
INSTALL_DIR="${PARITY_INSTALL_DIR:-$HOME/.local/bin}"

if [ "$(uname -s)" != "Linux" ]; then
  echo "error: parity only ships Linux binaries" >&2
  exit 1
fi

case "$(uname -m)" in
  x86_64) ARCH="x64" ;;
  aarch64 | arm64) ARCH="arm64" ;;
  *)
    echo "error: unsupported architecture $(uname -m)" >&2
    exit 1
    ;;
esac

# The raw.githubusercontent.com endpoint refuses files this large, so fetch
# the blob through the API instead.
RELEASE_URL="https://api.github.com/repos/${REPO}/contents/release/parity-linux-${ARCH}"

mkdir -p "$INSTALL_DIR"

echo "downloading parity (linux-${ARCH}) to $INSTALL_DIR/parity"
curl -fsSL --retry 3 -H "Accept: application/vnd.github.raw" \
  "$RELEASE_URL" -o "$INSTALL_DIR/parity"
chmod +x "$INSTALL_DIR/parity"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo "note: add $INSTALL_DIR to your PATH to run 'parity' from anywhere" ;;
esac

"$INSTALL_DIR/parity" --help > /dev/null
echo "parity installed"
