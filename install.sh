#!/bin/sh
# Installs the latest mootd release for people without Homebrew.
# Usage: curl -fsSL https://raw.githubusercontent.com/joeuk89/mootd/main/install.sh | sh
# Set MOOTD_INSTALL_DIR to install somewhere other than ~/.local/bin.
set -eu

repo=joeuk89/mootd
dir=${MOOTD_INSTALL_DIR:-$HOME/.local/bin}

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case $os in
  darwin | linux) ;;
  *) echo "mootd supports macOS and Linux, not $os." >&2; exit 1 ;;
esac

arch=$(uname -m)
case $arch in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) echo "mootd has no build for $arch." >&2; exit 1 ;;
esac

version=$(curl -fsSL "https://api.github.com/repos/$repo/releases/latest" |
  sed -n 's/.*"tag_name": *"v\([^"]*\)".*/\1/p')
if [ -z "$version" ]; then
  echo "Could not find the latest mootd release." >&2
  exit 1
fi

file=mootd_${version}_${os}_${arch}.tar.gz
base=https://github.com/$repo/releases/download/v$version
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

curl -fsSL -o "$tmp/$file" "$base/$file"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt"

cd "$tmp"
grep " $file\$" checksums.txt > expected.txt
if command -v sha256sum >/dev/null; then
  sha256sum -c expected.txt >/dev/null
else
  shasum -a 256 -c expected.txt >/dev/null
fi

tar -xzf "$file" mootd
mkdir -p "$dir"
install -m 755 mootd "$dir/mootd"

echo "Installed mootd $version to $dir/mootd"
case ":$PATH:" in
  *":$dir:"*) ;;
  *) echo "Add $dir to your PATH, then:" ;;
esac
echo 'Run "mootd init" to set it up.'
