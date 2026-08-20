#!/usr/bin/env bash
set -euo pipefail

VERSION="${VERSION:-dev}"
OUT="${OUT:-dist}"
GIT_COMMIT="${GIT_COMMIT:-$(git rev-parse --verify HEAD 2>/dev/null || true)}"

if [[ -z "$VERSION" ]]; then
  echo "VERSION must not be empty" >&2
  exit 2
fi
if [[ ! "$GIT_COMMIT" =~ ^[0-9a-fA-F]{40}$ ]]; then
  echo "GIT_COMMIT must be an exact 40-character Git commit SHA" >&2
  exit 2
fi

mkdir -p "$OUT"

build() {
  local goos="$1" goarch="$2" ext="$3"
  local target="$OUT/shajretail-pos-${VERSION}-${goos}-${goarch}${ext}"
  echo "building $target"
  CGO_ENABLED=1 GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags "-s -w -X main.version=${VERSION} -X main.gitCommit=${GIT_COMMIT}" -o "$target" ./cmd/posservice
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

(
  cd "$OUT"
  sha256sum shajretail-pos-* > SHA256SUMS.txt
  cat > RELEASE-MANIFEST.txt <<EOF
version=${VERSION}
git_commit=${GIT_COMMIT}
checksums=SHA256SUMS.txt
EOF
)
