#!/bin/sh
# Builds the release archives for every supported platform into dist/.
# Usage: scripts/build-release.sh <version>
set -eu

version=$1
rm -rf dist
mkdir -p dist

for target in darwin/arm64 darwin/amd64 linux/arm64 linux/amd64; do
  os=${target%/*}
  arch=${target#*/}
  stage=dist/stage
  mkdir -p "$stage"
  GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.version=$version" -o "$stage/mootd" ./cmd/mootd
  cp LICENSE README.md "$stage/"
  tar -C "$stage" -czf "dist/mootd_${version}_${os}_${arch}.tar.gz" mootd LICENSE README.md
  rm -r "$stage"
done

cd dist
if command -v sha256sum >/dev/null; then
  sha256sum mootd_*.tar.gz > checksums.txt
else
  shasum -a 256 mootd_*.tar.gz > checksums.txt
fi
