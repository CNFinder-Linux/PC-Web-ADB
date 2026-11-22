#!/usr/bin/env bash
# Build the deterministic GPL corresponding-source archive used by both the
# Debian package (embedded copy) and the Windows distribution (dist copy).
# Usage: packaging/make-source.sh [output.tar.gz]
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="${1:-$ROOT/dist/pcwebadb-1.1.0-source.tar.gz}"
FIRST_LINE="$(head -n 1 "$ROOT/debian/changelog")"
VERSION=""
if [[ "$FIRST_LINE" =~ ^[^\(]*\(([^\)]*)\) ]]; then VERSION="${BASH_REMATCH[1]}"; fi
[ -n "$VERSION" ] || { echo "ERROR: cannot parse version from debian/changelog" >&2; exit 1; }
SRC_VERSION="${VERSION%%-*}"
mkdir -p "$(dirname "$OUT")"
TMP_OUT="${OUT}.tmp"

# --mtime/@0 + --sort=name + gzip -n make repeated builds byte-identical, so
# the copy embedded in the .deb and the one in dist/ really are the same file.
# Documentation screenshots are not build inputs; omitting them keeps the NSIS
# datablock under this toolchain's ~16 MiB limit.
tar --sort=name --owner=0 --group=0 --numeric-owner --mtime=@0 \
  --exclude='./.tmp-go' --exclude='./.tmp' --exclude='./dist' \
  --exclude='./packaging/root' --exclude='./reference' \
  --exclude='./vendor/platform-tools' --exclude='./.sysroot' \
  --exclude='./docs/screenshot-*.png' \
  --transform="s,^\.,pcwebadb-${SRC_VERSION}," \
  -C "$ROOT" -cf - . | gzip -n -9 > "$TMP_OUT"
mv -f "$TMP_OUT" "$OUT"
echo "$OUT"
