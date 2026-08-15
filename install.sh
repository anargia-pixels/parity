#!/usr/bin/env bash
# Install the parity binary from the repo's release folder into
# ~/.local/bin (or $PARITY_INSTALL_DIR when set).
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/Doctorthe113/parity/main/install.sh | bash

set -euo pipefail

REPO="Doctorthe113/parity"
RELEASE_URL="https://raw.githubusercontent.com/${REPO}/main/release/parity"
INSTALL_DIR="${PARITY_INSTALL_DIR:-$HOME/.local/bin}"

if [ "$(uname -s)" != "Linux" ]; then
  echo "error: parity only ships a Linux binary" >&2
  exit 1
fi

if [ "$(uname -m)" != "x86_64" ]; then
  echo "error: parity only ships an x86_64 binary" >&2
  exit 1
fi

mkdir -p "$INSTALL_DIR"

echo "downloading parity to $INSTALL_DIR/parity"
curl -fsSL "$RELEASE_URL" -o "$INSTALL_DIR/parity"
chmod +x "$INSTALL_DIR/parity"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo "note: add $INSTALL_DIR to your PATH to run 'parity' from anywhere" ;;
esac

"$INSTALL_DIR/parity" --help > /dev/null
echo "parity installed"
