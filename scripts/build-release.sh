#!/usr/bin/env bash
set -euo pipefail

VERSION="${VERSION:-dev}"
OUT="${OUT:-dist}"
mkdir -p "$OUT"

build() {
  local goos="$1" goarch="$2" ext="$3"
  local target="$OUT/shajretail-pos-${VERSION}-${goos}-${goarch}${ext}"
  echo "building $target"
  CGO_ENABLED=1 GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o "$target" ./cmd/posservice
}

case "${1:-all}" in
  linux) build linux amd64 "" ;;
  windows) build windows amd64 ".exe" ;;
  darwin) build darwin amd64 "" ;;
  all)
    build linux amd64 ""
    build windows amd64 ".exe"
    build darwin amd64 ""
    ;;
  *) echo "usage: $0 [linux|windows|darwin|all]" >&2; exit 2 ;;
esac

( cd "$OUT" && sha256sum shajretail-pos-* > SHA256SUMS.txt )
