#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

COMMIT="$(git rev-parse --verify HEAD)"
VERSION="v1-provenance-test"

VERSION="$VERSION" GIT_COMMIT="$COMMIT" OUT="$TMP" ./scripts/build-release.sh linux

test -f "$TMP/shajretail-pos-${VERSION}-linux-amd64"
test -f "$TMP/SHA256SUMS.txt"
test -f "$TMP/RELEASE-MANIFEST.txt"

grep -Fx "version=${VERSION}" "$TMP/RELEASE-MANIFEST.txt"
grep -Fx "git_commit=${COMMIT}" "$TMP/RELEASE-MANIFEST.txt"
grep -Fx "checksums=SHA256SUMS.txt" "$TMP/RELEASE-MANIFEST.txt"
(
  cd "$TMP"
  sha256sum -c SHA256SUMS.txt
)

if VERSION="$VERSION" GIT_COMMIT="not-a-commit" OUT="$TMP/invalid" ./scripts/build-release.sh linux >/dev/null 2>&1; then
  echo "build-release.sh accepted invalid source provenance" >&2
  exit 1
fi

echo "V1 POS deployment artifact provenance acceptance passed"
