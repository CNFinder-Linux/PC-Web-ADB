#!/usr/bin/env python3
"""Prepare a private libc header sysroot for CGO builds on minimal hosts.

Normal Debian/Ubuntu build machines already have libc6-dev installed, so
build-deb.sh never calls this script. It exists for stripped-down/sandboxed
hosts (no root, no /usr/include/stdlib.h): it downloads the matching dev
packages with `apt-get download`, unpacks them into the project, and patches
the libc.so linker script so GNU ld can find libc_nonshared.a locally.

No system directory is modified. Requires only Python 3 stdlib + apt-get.
"""
import glob
import io
import os
import re
import subprocess
import sys
import tarfile

DEBS = ("libc6-dev", "linux-libc-dev")


def unpack_deb(path, root):
    raw = open(path, "rb").read()
    if not raw.startswith(b"!<arch>\n"):
        raise RuntimeError("not an ar archive: " + path)
    pos = 8
    while pos + 60 <= len(raw):
        hdr = raw[pos:pos + 60]
        name = hdr[0:16].decode("ascii", "replace").strip()
        try:
            size = int(hdr[48:58].decode("ascii").strip())
        except ValueError:
            break
        data = raw[pos + 60:pos + 60 + size]
        pos += 60 + size + (size % 2)
        if not name.startswith("data.tar"):
            continue
        mode = "r:xz" if name.endswith(".xz") else ("r:gz" if name.endswith(".gz") else "r:*")
        tf = tarfile.open(fileobj=io.BytesIO(data), mode=mode)
        count = 0
        for m in tf.getmembers():
            if not (m.isfile() or m.isdir() or m.issym()):
                continue
            dst = os.path.join(root, m.name.lstrip("/"))
            os.makedirs(os.path.dirname(dst), exist_ok=True)
            if m.isdir():
                continue
            if m.issym():
                if not os.path.lexists(dst):
                    os.symlink(m.linkname, dst)
                continue
            f = tf.extractfile(m)
            if f:
                with open(dst, "wb") as out:
                    out.write(f.read())
            count += 1
        return count
    raise RuntimeError("no data.tar member in " + path)


def patch_linker_scripts(root):
    """glibc's libc.so is an ld script with absolute /usr/lib paths."""
    libdirs = glob.glob(os.path.join(root, "usr", "lib", "*"))
    changed = []
    for libdir in libdirs:
        if not os.path.isdir(libdir):
            continue
        for p in glob.glob(os.path.join(libdir, "*.so")):
            if os.path.islink(p) or os.path.getsize(p) > 4096:
                continue
            text = open(p, errors="replace").read()
            if "GNU ld script" not in text and "GROUP" not in text:
                continue
            # Only rewrite paths that START at /usr or /lib (not ones already
            # pointing inside our sysroot).
            def repl(m):
                return os.path.join(libdir, os.path.basename(m.group(0)))

            new = re.sub(r"(?<![\w/])/(?:usr|lib)/[^\s()]+\.a", repl, text)
            if new != text:
                open(p, "w").write(new)
                changed.append(os.path.basename(p))
    return changed


def main():
    if len(sys.argv) != 2:
        print("usage: prepare-sysroot.py <sysroot-dir>", file=sys.stderr)
        return 2
    root = os.path.abspath(sys.argv[1])
    if os.path.isfile(os.path.join(root, "usr", "include", "stdlib.h")):
        print("sysroot already prepared: " + root)
        return 0

    dl = os.path.join(os.path.dirname(root), ".tmp", "sysroot-dl")
    os.makedirs(dl, exist_ok=True)
    os.makedirs(root, exist_ok=True)
    print("downloading dev packages into " + dl)
    subprocess.check_call(["apt-get", "download", *DEBS], cwd=dl)
    for deb in glob.glob(os.path.join(dl, "*.deb")):
        print("  unpack %s -> %d files" % (os.path.basename(deb), unpack_deb(deb, root)))

    # libgcc static helpers are optional; ignore if the package name differs.
    try:
        major = subprocess.check_output(["gcc", "-dumpversion"], text=True).strip().split(".")[0]
        subprocess.check_call(["apt-get", "download", "libgcc-%s-dev" % major], cwd=dl)
        for deb in glob.glob(os.path.join(dl, "libgcc-%s-dev*.deb" % major)):
            unpack_deb(deb, root)
    except (subprocess.CalledProcessError, OSError) as exc:
        print("  (libgcc dev package skipped: %s)" % exc)

    patched = patch_linker_scripts(root)
    libc = os.path.join(root, "usr", "lib", "x86_64-linux-gnu", "libc.so")
    if os.path.isfile(libc) and "GNU ld script" in open(libc, errors="replace").read():
        # Deterministic rewrite: the generic regex above can be confused by
        # repeated runs, so emit the exact script for amd64.
        libdir = os.path.dirname(libc)
        open(libc, "w").write(
            "/* GNU ld script - patched for private sysroot */\n"
            "OUTPUT_FORMAT(elf64-x86-64)\n"
            "GROUP ( /lib/x86_64-linux-gnu/libc.so.6 "
            + os.path.join(libdir, "libc_nonshared.a")
            + " AS_NEEDED ( /lib/x86_64-linux-gnu/ld-linux-x86-64.so.2 ) )\n"
        )
        if "libc.so" not in patched:
            patched.append("libc.so")

    if not os.path.isfile(os.path.join(root, "usr", "include", "stdlib.h")):
        print("ERROR: stdlib.h still missing after unpacking", file=sys.stderr)
        return 1
    print("sysroot ready: %s (patched: %s)" % (root, ", ".join(patched) or "none"))
    return 0


if __name__ == "__main__":
    sys.exit(main())
