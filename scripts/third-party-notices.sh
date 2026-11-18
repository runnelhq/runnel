#!/usr/bin/env bash
# Prints the license texts of all third-party Go modules compiled into the runnel binary.
# Required by the MIT license of those dependencies (see docs/adr/0004-lizenz-mit.md).
set -euo pipefail

cd "$(dirname "$0")/.."

cat <<'HEADER'
Third-party software notices for Runnel
=======================================

Runnel includes the following third-party software. Their license texts follow.
HEADER

# Union of the dependencies of all release targets (some are platform-specific).
modules() {
  for goos in windows linux; do
    GOOS=$goos GOARCH=amd64 go list -deps \
      -f '{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}} {{.Dir}}{{end}}{{end}}' ./cmd/runnel
  done
  # The Go standard library and runtime are compiled into every binary as well.
  echo "Go-standard-library $(go env GOVERSION) $(go env GOROOT)"
}

modules |
  sort -u |
  while read -r path version dir; do
    [[ -z "$path" ]] && continue
    printf '\n\n--------------------------------------------------------------------------------\n'
    printf '%s %s\n' "$path" "$version"
    printf -- '--------------------------------------------------------------------------------\n\n'
    found=0
    for f in "$dir"/LICENSE* "$dir"/LICENCE* "$dir"/COPYING* "$dir"/NOTICE*; do
      [[ -f "$f" ]] || continue
      cat "$f"
      found=1
    done
    if [[ $found -eq 0 ]]; then
      echo "ERROR: no license file found for $path $version" >&2
      exit 1
    fi
  done
