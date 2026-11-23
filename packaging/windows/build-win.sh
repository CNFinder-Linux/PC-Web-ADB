#!/usr/bin/env bash
# Build the Windows 7 x64 GUI binary, NSIS installer and source archive.
# x86 is intentionally no longer produced. All tools are used from private
# prefixes; no root and no system installs.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

GO_ROOT="${GO_ROOT:-/home/finder/WorkBuddy/WB-work/.toolchain/go}"
NSIS_TOOLS="${NSIS_TOOLS:-/home/finder/WorkBuddy/WB-work/putty-cn/.tools/root}"
PY="${PY:-/home/finder/.workbuddy/binaries/python/versions/3.13.12/bin/python3}"
GO="$GO_ROOT/bin/go"

echo "==> [1/7] Toolchain (x64 only)"
"$GO" version
case "$("$GO" version)" in
  *"go1.20."*) ;;
  *) echo "ERROR: Windows 7 requires Go 1.20.x (Go 1.21+ needs Windows 10)." >&2; exit 1 ;;
esac
[ -x "$NSIS_TOOLS/usr/bin/makensis" ] || { echo "ERROR: makensis not found at $NSIS_TOOLS" >&2; exit 1; }
[ -f "$NSIS_TOOLS/usr/share/nsis/Include/MUI2.nsh" ] || { echo "ERROR: MUI2.nsh missing" >&2; exit 1; }
mkdir -p dist .tmp .tmp-go/work .tmp-go/cache

export CGO_ENABLED=0
export TMPDIR="$ROOT/.tmp-go/work"
export GOCACHE="$ROOT/.tmp-go/cache"

echo "==> [2/7] go vet (windows/amd64) + unit tests"
GOOS=windows GOARCH=amd64 "$GO" vet .
CGO_ENABLED=0 "$GO" test ./...

echo "==> [3/7] Cross-compile x64 GUI exe (static, no cgo)"
# PE resources (file icon + VERSIONINFO) come from a windres-compiled COFF
# object. Go links *.syso files from the package dir; the object is
# architecture-specific, so build it, link the x64 exe, then move it aside.
WINDRES64="$NSIS_TOOLS/usr/bin/x86_64-w64-mingw32-windres"
SYSO="rsrc_windows.syso"
if [ -x "$WINDRES64" ]; then
  "$WINDRES64" -O coff -c 65001 -I "$ROOT/packaging/windows" \
    "$ROOT/packaging/windows/versioninfo.rc" -o "$ROOT/$SYSO"
else
  echo "  (windres not found - exe will carry no file icon)"
fi
GOOS=windows GOARCH=amd64 "$GO" build -trimpath -ldflags="-s -w -H windowsgui" \
  -o dist/PCWebADB-x64.exe .
if [ -f "$ROOT/$SYSO" ]; then
  # Keep a copy in .tmp instead of deleting (archive-cleaner policies forbid
  # bulk deletes of resource objects); it also documents what was linked.
  mv -f "$ROOT/$SYSO" "$ROOT/.tmp/rsrc_windows_amd64.syso"
fi
file dist/PCWebADB-x64.exe

# The three platform-tools files and the tray ICO are go:embed inputs. Prove
# the exe actually carries byte-identical copies, otherwise a standalone exe
# would boot and then fail with "adb.exe not found".
echo "==> [4/7] Embedded ADB + tray icon payload integrity"
"$PY" - <<'PY'
import hashlib, sys
exe = 'dist/PCWebADB-x64.exe'
files = ['adb.exe', 'AdbWinApi.dll', 'AdbWinUsbApi.dll']
sources = {n: open('vendor/platform-tools-win/' + n, 'rb').read() for n in files}
ico = open('packaging/windows/pcwebadb.ico', 'rb').read()
assert ico[:6] == b'\x00\x00\x01\x00\x02\x00', 'bad ICO header (expect 2 images)'
blob = open(exe, 'rb').read()
for n, data in sources.items():
    if blob.find(data) < 0:
        sys.exit('FAIL: %s does not contain an exact copy of %s' % (exe, n))
    print('  %-24s %-18s %d bytes  sha=%s' %
          (exe.split('/')[-1], n, len(data), hashlib.sha256(data).hexdigest()[:12]))
if blob.find(ico) < 0:
    sys.exit('FAIL: %s does not carry the embedded tray ICO' % exe)
print('  %-24s %-18s %d bytes  sha=%s' %
      (exe.split('/')[-1], 'pcwebadb.ico', len(ico), hashlib.sha256(ico).hexdigest()[:12]))
# PE .rsrc must contain the icon group and the UTF-16LE product name.
name16 = 'PC Web ADB (PWA)'.encode('utf-16-le')
if blob.find(name16) < 0 or b'.rsrc' not in blob:
    sys.exit('FAIL: %s lacks PE version resources (file icon / VERSIONINFO)' % exe)
print('  %-24s PE .rsrc present: icon + VERSIONINFO ok' % exe.split('/')[-1])
PY

echo "==> [5/7] PE / Windows 7 x64 compatibility checks"
"$PY" packaging/check_pe.py dist/PCWebADB-x64.exe --require-gui --label "PCWebADB-x64.exe"
"$PY" packaging/check_pe.py vendor/platform-tools-win/adb.exe --label "adb.exe 34.0.4 (32-bit WOW64 payload)"
"$PY" packaging/check_pe.py vendor/platform-tools-win/AdbWinApi.dll --label "AdbWinApi.dll"
"$PY" packaging/check_pe.py vendor/platform-tools-win/AdbWinUsbApi.dll --label "AdbWinUsbApi.dll"
REV=$(sed -n 's/^Pkg.Revision=//p' vendor/platform-tools-win/source.properties)
[ "$REV" = "34.0.4" ] || { echo "ERROR: expected platform-tools 34.0.4 (last Win7-compatible), got $REV" >&2; exit 1; }
echo "  platform-tools revision: $REV (OK)"

echo "==> [6/7] GPL source archive + NSIS installer (Unicode / SimpChinese)"
bash "$ROOT/packaging/make-source.sh" "$ROOT/dist/pcwebadb-1.1.0-source.tar.gz"
tar -tzf dist/pcwebadb-1.1.0-source.tar.gz > .tmp/source-archive-list.txt
for f in vendor/platform-tools-win/adb.exe \
         packaging/windows/pcwebadb.ico \
         packaging/pcwebadb-tray.png \
         packaging/windows/make-icon.py \
         tray_windows.go tray_icon_linux.go; do
  grep -q "$f" .tmp/source-archive-list.txt || {
    echo "ERROR: embedded build input missing from source archive: $f" >&2; exit 1; }
done

# makensis on Linux needs the script as UTF-8 with BOM; keep the repository
# copy BOM-free and generate a temporary BOM copy for compilation.
"$PY" - "$ROOT/packaging/windows/installer.nsi" "$ROOT/.tmp/installer-utf8bom.nsi" <<'PY'
import sys
src, dst = sys.argv[1], sys.argv[2]
text = open(src, encoding='utf-8').read()
open(dst, 'wb').write(b'\xef\xbb\xbf' + text.encode('utf-8'))
assert '创建桌面快捷方式' in text
assert '通知区域' in text
print('  installer.nsi: UTF-8 BOM copy written, Chinese text verified')
PY

export NSISDIR="$NSIS_TOOLS/usr/share/nsis"
export LD_LIBRARY_PATH="$NSIS_TOOLS/usr/lib/x86_64-linux-gnu:$NSIS_TOOLS/usr/lib:${LD_LIBRARY_PATH:-}"
MAKENSIS="$NSIS_TOOLS/usr/bin/makensis"
"$MAKENSIS" -PPO -INPUTCHARSET UTF8 -DROOT="$ROOT" "$ROOT/.tmp/installer-utf8bom.nsi" > "$ROOT/.tmp/installer-ppo.nsi"
"$PY" - "$ROOT/.tmp/installer-ppo.nsi" <<'PY'
import sys
text = open(sys.argv[1], encoding='utf-8').read()
for needle in ('创建桌面快捷方式', '已内嵌', '通用 C 运行库', '通知区域'):
    assert needle in text, 'missing Chinese string in preprocessed output: ' + needle
print('  PPO preprocess: Chinese strings decoded correctly')
PY
"$MAKENSIS" -INPUTCHARSET UTF8 -DROOT="$ROOT" "$ROOT/.tmp/installer-utf8bom.nsi" | tail -25
file dist/PCWebADB-1.1.0-win7-setup.exe
"$PY" packaging/check_pe.py dist/PCWebADB-1.1.0-win7-setup.exe --require-gui --label "NSIS installer stub"
# If 7-Zip is present, verify the NSIS datablock actually carries every file.
if command -v 7z >/dev/null 2>&1; then
  7z l dist/PCWebADB-1.1.0-win7-setup.exe > "$ROOT/.tmp/nsis-listing.txt"
  "$PY" - "$ROOT/.tmp/nsis-listing.txt" <<'PY'
import sys
text = open(sys.argv[1], encoding='utf-8', errors='replace').read()
required = ('PCWebADB.exe', 'LICENSE.txt', 'ADB-NOTICE.txt', 'WINDOWS.md',
            'README.md', 'UPSTREAM-REFERENCE.md', 'platform-tools.properties')
missing = [n for n in required if n not in text]
assert not missing, 'missing payload members: ' + ', '.join(missing)
# adb.exe / the two DLLs are embedded in PCWebADB.exe. Keeping them out avoids
# the ~16 MiB NSIS datablock limit. x86 must never reappear in an x64 installer.
for banned in ('adb.exe', 'AdbWinApi.dll', 'PCWebADB-x86.exe'):
    assert banned not in text, 'unexpected payload member: ' + banned
print('  NSIS payload listing: all %d required files present, no x86/duplicate adb' % len(required))
PY
fi

echo "==> [7/7] Checksums"
sha256sum \
  dist/PCWebADB-x64.exe \
  dist/PCWebADB-1.1.0-win7-setup.exe \
  dist/pcwebadb-1.1.0-source.tar.gz > dist/SHA256SUMS-windows.txt
cat dist/SHA256SUMS-windows.txt
echo
echo "DONE: dist/PCWebADB-1.1.0-win7-setup.exe (x64 only)"
