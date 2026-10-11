#!/usr/bin/env bash
# Build stripped + compressed glow binary.
# Usage: ./build.sh [output]  (default: ./glow)
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"

out="${1:-glow}"

rm -f -- "$out"
go build -trimpath -ldflags='-s -w' -o "$out" .

if command -v upx >/dev/null 2>&1; then
  upx --best --lzma "$out"
else
  echo "upx not found, skipping compression" >&2
fi

ls -lh "$out"
