#!/usr/bin/env bash
# Builds release binaries into dist/. Requires Go 1.25+ and, for the first
# build, Node (to embed the UI fonts).
set -euo pipefail
cd "$(dirname "$0")"

VERSION="${VERSION:-$(git describe --tags --always 2>/dev/null || echo dev)}"
LDFLAGS="-s -w -X main.version=$VERSION"

if [ ! -f internal/web/static/fonts.css ]; then
  echo "Fetching UI fonts (one time)"
  node scripts/fetch-fonts.mjs
fi

mkdir -p dist
for target in linux/amd64 linux/arm64 windows/amd64; do
  GOOS="${target%/*}"
  GOARCH="${target#*/}"
  out="dist/craftpanel-$GOOS-$GOARCH"
  [ "$GOOS" = "windows" ] && out="$out.exe"
  echo "Building $out"
  CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" go build -trimpath -ldflags "$LDFLAGS" -o "$out" .
done
echo "Done: $(ls dist)"
