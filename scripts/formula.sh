#!/bin/sh
# Prints the Homebrew formula for a release.
# Usage: scripts/formula.sh <version> <checksums.txt>
set -eu

version=$1
checksums=$2
base=https://github.com/joeuk89/mootd/releases/download/v$version

sum() {
  awk -v file="mootd_${version}_$1.tar.gz" '$2 == file { print $1 }' "$checksums"
}

cat <<FORMULA
class Mootd < Formula
  desc "AI-written terminal greetings: topical jokes and colour text art"
  homepage "https://github.com/joeuk89/mootd"
  version "$version"
  license "MIT"

  on_macos do
    on_arm do
      url "$base/mootd_${version}_darwin_arm64.tar.gz"
      sha256 "$(sum darwin_arm64)"
    end
    on_intel do
      url "$base/mootd_${version}_darwin_amd64.tar.gz"
      sha256 "$(sum darwin_amd64)"
    end
  end

  on_linux do
    on_arm do
      url "$base/mootd_${version}_linux_arm64.tar.gz"
      sha256 "$(sum linux_arm64)"
    end
    on_intel do
      url "$base/mootd_${version}_linux_amd64.tar.gz"
      sha256 "$(sum linux_amd64)"
    end
  end

  def install
    bin.install "mootd"
  end

  def caveats
    <<~EOS
      Run this once to set mootd up:
        mootd init
    EOS
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/mootd version")
  end
end
FORMULA
