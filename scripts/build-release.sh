#!/bin/sh
set -eu

task_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$task_root"
mkdir -p release
task_build_dir=$(mktemp -d "${TMPDIR:-/tmp}/parity-release.XXXXXX")
trap 'rm -rf "$task_build_dir"' EXIT HUP INT TERM

# The version names the source commit, for example 2026.10.04-f930957.
# Built release files are ignored when checking for uncommitted changes.
release_version=$(TZ=UTC git log -1 --format=%cd --date=format-local:%Y.%m.%d)-$(git rev-parse --short=7 HEAD)
if [ -n "$(git status --porcelain -- . ':(exclude)release')" ]; then
  release_version="$release_version-dirty"
fi

for go_os in linux darwin; do
  for go_arch in amd64 arm64; do
    release_arch=$go_arch
    if [ "$go_arch" = amd64 ]; then
      release_arch=x64
    fi
    binary_name="parity-$go_os-$release_arch"
    CGO_ENABLED=0 GOOS="$go_os" GOARCH="$go_arch" \
      go build -trimpath \
      -ldflags="-s -w -X github.com/Doctorthe113/parity/internal/parity.version=$release_version" \
      -o "$task_build_dir/$binary_name" .
    gzip -n -9 -c "$task_build_dir/$binary_name" > "$task_build_dir/$binary_name.gz"
  done
done

for go_os in linux darwin; do
  for release_arch in x64 arm64; do
    mv "$task_build_dir/parity-$go_os-$release_arch.gz" release/
  done
done
# install.sh reads this file to show the version it installs.
echo "$release_version" > release/version
echo "built parity $release_version"
