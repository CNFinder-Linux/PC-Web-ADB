#!/usr/bin/env bash
# Build pcwebadb and assemble an installable Debian binary package.
#
# Usage:  packaging/build-deb.sh [--arch amd64] [--go /path/to/go] [--no-verify]
# Requires: Go >= 1.20, gcc, libc headers (auto-prepared if missing),
#           dpkg-deb, gzip, tar, coreutils, findutils.
# No debhelper / GTK-WebKit dev packages / pkg-config / Node.js / Qt needed:
# GTK3 and WebKitGTK are dlopen()ed at runtime, not linked at build time.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

ARCH="amd64"
GOARCH="amd64"
GO_BIN=""
DO_VERIFY=1
while [ $# -gt 0 ]; do
  case "$1" in
    --arch) ARCH="$2"; shift 2 ;;
    --go) GO_BIN="$2"; shift 2 ;;
    --no-verify) DO_VERIFY=0; shift ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done
case "$ARCH" in
  amd64) GOARCH="amd64" ;;
  arm64) GOARCH="arm64" ;;
  *) echo "unsupported architecture: $ARCH" >&2; exit 2 ;;
esac

STAGE="$ROOT/packaging/root"
OUT="$ROOT/dist"
TMP="$ROOT/.tmp-go"
# The stage is rebuilt in place: every file below is installed/cat-overwritten
# on each run. Only legacy (pre-rename) paths are removed explicitly - a bulk
# rm -rf of the stage is neither needed nor allowed in restricted sandboxes.
rm -f "$STAGE/usr/share/applications/skyadb-pc.desktop" \
      "$STAGE/usr/share/icons/hicolor/scalable/apps/skyadb-pc.svg" \
      "$STAGE/usr/share/man/man1/skyadb-pc.1.gz" \
      "$STAGE/usr/bin/skyadb-pc"
rm -rf "$STAGE/usr/lib/skyadb-pc" "$STAGE/usr/share/skyadb-pc" \
       "$STAGE/usr/share/doc/skyadb-pc"
mkdir -p "$OUT" "$TMP/work" "$TMP/cache"

# ---------------------------------------------------------------- Go toolchain
if [ -z "$GO_BIN" ]; then
  for c in "$ROOT/../.toolchain/go/bin/go" "$(command -v go 2>/dev/null || true)" /usr/lib/go/bin/go /usr/local/go/bin/go; do
    if [ -n "$c" ] && [ -x "$c" ]; then GO_BIN="$c"; break; fi
  done
fi
[ -n "$GO_BIN" ] || { echo "ERROR: Go >= 1.20 not found (use --go)" >&2; exit 1; }
GOROOT_DETECTED="$("$GO_BIN" env GOROOT)"
export GOROOT="$GOROOT_DETECTED"
export PATH="$(dirname "$GO_BIN"):$PATH"
export GOOS=linux GOARCH="$GOARCH"
export TMPDIR="$TMP/work" GOCACHE="$TMP/cache"

echo "[1/7] Go toolchain: $("$GO_BIN" version)  (GOARCH=$GOARCH)"

# The bundled adb is an amd64 ELF; cross-building requires an explicit
# architecture-matched adb supplied via ADB_BIN.
ADB_BIN="${ADB_BIN:-$ROOT/vendor/platform-tools/adb}"
if [ "$GOARCH" != "amd64" ] && [ "$ADB_BIN" = "$ROOT/vendor/platform-tools/adb" ]; then
  echo "ERROR: vendor/platform-tools/adb is amd64. Supply ADB_BIN=<arm64 adb> for --arch arm64." >&2
  exit 2
fi
[ -x "$ADB_BIN" ] || { echo "ERROR: adb not found: $ADB_BIN" >&2; exit 1; }

# ------------------------------------------------------------------- version
FIRST_LINE="$(head -n 1 "$ROOT/debian/changelog")"
VERSION=""
if [[ "$FIRST_LINE" =~ ^[^\(]*\(([^\)]*)\) ]]; then VERSION="${BASH_REMATCH[1]}"; fi
[ -n "$VERSION" ] || { echo "ERROR: cannot parse version from debian/changelog" >&2; exit 1; }
DEB="$OUT/pcwebadb_${VERSION}_${ARCH}.deb"
echo "[2/7] Package version: $VERSION  ->  $(basename "$DEB")"

# --------------------------------------------------- native desktop shell / CGO
# The GTK/WebKit window needs a tiny cgo shim (dlopen only). On a normal
# Debian build host libc6-dev is present. On stripped-down hosts we unpack
# the matching dev packages into a project-local sysroot - no root needed.
echo "[3/7] Preparing native GTK/WebKit desktop shell…"
if [ "$GOARCH" = "amd64" ]; then
  command -v gcc >/dev/null 2>&1 || { echo "ERROR: gcc is required for the cgo shell" >&2; exit 1; }
  export CGO_ENABLED=1
  if [ ! -f /usr/include/stdlib.h ]; then
    SYSROOT="$ROOT/.sysroot"
    if [ ! -f "$SYSROOT/usr/include/stdlib.h" ]; then
      PY="$(command -v python3 || true)"
      [ -n "$PY" ] || PY=/home/finder/.workbuddy/binaries/python/versions/3.13.12/bin/python3
      "$PY" "$ROOT/packaging/prepare-sysroot.py" "$SYSROOT"
    fi
    export CGO_CFLAGS="-I$SYSROOT/usr/include -I$SYSROOT/usr/include/x86_64-linux-gnu"
    export CGO_LDFLAGS="-B$SYSROOT/usr/lib/x86_64-linux-gnu -L$SYSROOT/usr/lib/x86_64-linux-gnu -L/usr/lib/x86_64-linux-gnu"
    echo "      private libc sysroot: $SYSROOT"
  fi
  echo "      cgo enabled (GTK3/WebKitGTK loaded at runtime via dlopen)"
else
  export CGO_ENABLED=0
  echo "      $GOARCH: CGO disabled, browser app-window fallback only"
fi

# ------------------------------------------------------------------- compile
echo "[4/7] Compiling Go binary (cgo desktop shell, stdlib only)…"
"$GO_BIN" build -trimpath -ldflags="-s -w" -o "$OUT/pcwebadb" .
"$OUT/pcwebadb" --version

# -------------------------------------------------------------------- staging
echo "[5/7] Assembling staging tree…"
if [ -d "$STAGE" ]; then find "$STAGE" -mindepth 1 -delete; fi
mkdir -p "$STAGE/DEBIAN" \
  "$STAGE/usr/bin" "$STAGE/usr/lib/pcwebadb" "$STAGE/usr/share/pcwebadb/web" \
  "$STAGE/usr/share/applications" "$STAGE/usr/share/icons/hicolor/scalable/apps" \
  "$STAGE/usr/share/man/man1" "$STAGE/usr/share/doc/pcwebadb"

install -m 0755 "$OUT/pcwebadb"            "$STAGE/usr/bin/pcwebadb"
install -m 0755 "$ADB_BIN"                  "$STAGE/usr/lib/pcwebadb/adb"
install -m 0644 "$ROOT/web/index.html"      "$STAGE/usr/share/pcwebadb/web/index.html"
install -m 0644 "$ROOT/web/app.css"         "$STAGE/usr/share/pcwebadb/web/app.css"
install -m 0644 "$ROOT/web/app.js"          "$STAGE/usr/share/pcwebadb/web/app.js"
install -m 0644 "$ROOT/packaging/pcwebadb.desktop" "$STAGE/usr/share/applications/pcwebadb.desktop"
install -m 0644 "$ROOT/packaging/pcwebadb.svg"     "$STAGE/usr/share/icons/hicolor/scalable/apps/pcwebadb.svg"
install -m 0644 "$ROOT/debian/copyright"    "$STAGE/usr/share/doc/pcwebadb/copyright"
install -m 0644 "$ROOT/packaging/README.Debian"     "$STAGE/usr/share/doc/pcwebadb/README.Debian"
install -m 0644 "$ROOT/packaging/UPSTREAM-REFERENCE.md" "$STAGE/usr/share/doc/pcwebadb/UPSTREAM-REFERENCE.md"
install -m 0644 "$ROOT/vendor/platform-tools/NOTICE.txt" "$STAGE/usr/share/doc/pcwebadb/NOTICE.txt"
install -m 0644 "$ROOT/vendor/platform-tools/source.properties" "$STAGE/usr/share/doc/pcwebadb/ADB-VERSION.txt"

gzip -9 -n -c "$ROOT/debian/changelog"       > "$STAGE/usr/share/doc/pcwebadb/changelog.Debian.gz"
gzip -9 -n -c "$ROOT/packaging/pcwebadb.1"  > "$STAGE/usr/share/man/man1/pcwebadb.1.gz"
chmod 0644 "$STAGE/usr/share/man/man1/pcwebadb.1.gz" \
           "$STAGE/usr/share/doc/pcwebadb/changelog.Debian.gz"
# Debian packages use 0755 for every directory; the build host's umask
# (often 0002 on shared workspaces) would otherwise leak 0775 into the .deb.
find "$STAGE" -type d -exec chmod 0755 {} +

# Complete corresponding source of the GPL work (upstream Kotlin snapshot is
# deliberately excluded: it is not compiled into or shipped in this binary).
# Keep vendor/platform-tools-win: adb_bundle_windows.go embeds those files, so
# they are compile-time inputs for the Windows build of this same source tree.
SRC_VERSION="${VERSION%%-*}"
bash "$ROOT/packaging/make-source.sh" "$OUT/pcwebadb-${SRC_VERSION}-source.tar.gz"
cp -f "$OUT/pcwebadb-${SRC_VERSION}-source.tar.gz" \
      "$STAGE/usr/share/doc/pcwebadb/source.tar.gz"
# Avoid `tar | grep -q`: with `set -o pipefail` grep's early exit gives tar a
# SIGPIPE and a bogus "write error". Materialise the listing first instead.
tar -tzf "$STAGE/usr/share/doc/pcwebadb/source.tar.gz" > "$TMP/source-archive-list.txt"
for f in vendor/platform-tools-win/adb.exe \
         packaging/windows/pcwebadb.ico \
         packaging/pcwebadb-tray.png \
         packaging/windows/make-icon.py \
         gui_gtk_linux.go tray_windows.go tray_icon_linux.go; do
  grep -q "$f" "$TMP/source-archive-list.txt" || {
    echo "FAIL: embedded build input missing from deb source archive: $f" >&2; exit 1; }
done
chmod 0644 "$STAGE/usr/share/doc/pcwebadb/source.tar.gz"

# --------------------------------------------------------------- DEBIAN/control
awk -v ver="$VERSION" '
  /^Package:/ { p = 1 }
  p { print; if ($0 ~ /^Package:/) print "Version: " ver }
' "$ROOT/debian/control" > "$STAGE/DEBIAN/control"
ISIZE="$(du -sk "$STAGE" | awk '{print $1}')"
sed -i "s/^Installed-Size:.*/Installed-Size: ${ISIZE}/" "$STAGE/DEBIAN/control"

(cd "$STAGE" && find . -type f ! -path './DEBIAN*' -printf '%P\n' | LC_ALL=C sort | xargs md5sum) \
  > "$STAGE/DEBIAN/md5sums"
chmod 0644 "$STAGE/DEBIAN/control" "$STAGE/DEBIAN/md5sums"

cat > "$STAGE/DEBIAN/postinst" <<'EOF'
#!/bin/sh
set -e
if [ "$1" = "configure" ]; then
    if command -v update-desktop-database >/dev/null 2>&1; then
        update-desktop-database -q /usr/share/applications || true
    fi
    if command -v gtk-update-icon-cache >/dev/null 2>&1; then
        gtk-update-icon-cache -q -f -t /usr/share/icons/hicolor || true
    fi
fi
exit 0
EOF
cat > "$STAGE/DEBIAN/postrm" <<'EOF'
#!/bin/sh
set -e
if command -v update-desktop-database >/dev/null 2>&1; then
    update-desktop-database -q /usr/share/applications || true
fi
if command -v gtk-update-icon-cache >/dev/null 2>&1; then
    gtk-update-icon-cache -q -f -t /usr/share/icons/hicolor || true
fi
exit 0
EOF
chmod 0755 "$STAGE/DEBIAN/postinst" "$STAGE/DEBIAN/postrm"

# --------------------------------------------------------------------- build
echo "[6/7] Building .deb with dpkg-deb (xz data, root-owned files)…"
rm -f -- "$DEB"
dpkg-deb -Zxz --build --root-owner-group "$STAGE" "$DEB"

# ------------------------------------------------------------------- verify
echo "[7/7] Verifying…"
dpkg-deb -f "$DEB" Package Version Architecture Depends Installed-Size
if [ "$DO_VERIFY" = "1" ]; then
  echo "--- ar members:"
  ar t "$DEB"
  echo "--- payload tree:"
  dpkg-deb -c "$DEB" | sed -n '1,40p'
  # Read-only integrity check. `dpkg-deb -x` is deliberately avoided: on
  # hardened/sandboxed hosts GNU tar cannot call utime() and would fail even
  # though the package itself is perfectly valid.
  if command -v python3 >/dev/null 2>&1; then
    python3 - "$DEB" "$STAGE" <<'PY'
import hashlib, io, os, subprocess, sys, tarfile
deb, stage = sys.argv[1], sys.argv[2]
raw = subprocess.check_output(["dpkg-deb", "--fsys-tarfile", deb])
tf = tarfile.open(fileobj=io.BytesIO(raw), mode="r:*")
members = {}
for m in tf.getmembers():
    name = m.name[2:] if m.name.startswith("./") else m.name
    members[name] = m
checks = [
    "usr/bin/pcwebadb",
    "usr/lib/pcwebadb/adb",
    "usr/share/pcwebadb/web/index.html",
    "usr/share/doc/pcwebadb/source.tar.gz",
]
for rel in checks:
    m = members.get(rel)
    if m is None or not m.isfile():
        sys.exit("FAIL: missing payload member " + rel)
    payload = tf.extractfile(m).read()
    local = open(os.path.join(stage, rel), "rb").read()
    if hashlib.sha256(payload).digest() != hashlib.sha256(local).digest():
        sys.exit("FAIL: checksum mismatch for " + rel)
    print("  OK  %-46s %8d bytes  mode=%o  uid/gid=%d/%d" %
          (rel, m.size, m.mode, m.uid, m.gid))
print("  payload members:", len(members))
PY
  fi
fi
# Keep the Linux checksum manifest in sync with every rebuild. Paths are
# relative to the project root, matching SHA256SUMS-windows.txt.
(cd "$ROOT" && sha256sum \
  "dist/pcwebadb_${VERSION}_${ARCH}.deb" \
  "dist/pcwebadb-${SRC_VERSION}-source.tar.gz" \
  dist/pcwebadb > dist/SHA256SUMS)

echo
echo "DONE: $DEB"
ls -lh "$DEB"
