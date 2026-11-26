#!/usr/bin/env bash
# Packages the binaries from bin/ into release archives in dist/, copies the SBOMs, writes the
# reference of the building blocks and expressions (roadmap E15a) and SHA256SUMS.
# Usage: scripts/package.sh <version>   (expects "make dist notices sbom" to have run)
set -euo pipefail

cd "$(dirname "$0")/.."

version="${1:?usage: $0 <version>}"
v="${version#v}"

rm -rf dist
mkdir -p dist/stage

stage() { # stage <name> <binary-source> <binary-target>
  local dir="dist/stage/$1"
  mkdir -p "$dir"
  cp "$2" "$dir/$3"
  cp LICENSE README.md bin/THIRD_PARTY_NOTICES.txt "$dir/"
}

win="runnel_${v}_windows_amd64"
stage "$win" bin/runnel-windows-amd64.exe runnel.exe
(cd dist/stage && zip -qr "../$win.zip" "$win")
cp bin/runnel-windows-amd64.cdx.json "dist/$win.cdx.json"

linux="runnel_${v}_linux_amd64"
stage "$linux" bin/runnel-linux-amd64 runnel
tar -czf "dist/$linux.tar.gz" -C dist/stage "$linux"
cp bin/runnel-linux-amd64.cdx.json "dist/$linux.cdx.json"

# Machine-readable reference of this version, from the Linux binary (the release runs on Linux).
bin/runnel-linux-amd64 catalog --json > dist/runnel-catalog.json
bin/runnel-linux-amd64 catalog --expressions --json > dist/runnel-expressions.json

rm -rf dist/stage
(cd dist && sha256sum ./*.zip ./*.tar.gz ./*.cdx.json ./runnel-catalog.json ./runnel-expressions.json | sed 's# \./# #' > SHA256SUMS)

ls -l dist
