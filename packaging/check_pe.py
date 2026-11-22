#!/usr/bin/env python3
"""Read-only PE compatibility checker for Windows 7 targets.

Usage: check_pe.py <file.exe> [--require-gui] [--label NAME]

Checks DOS/NT headers, optional-header OS/subsystem versions, PE subsystem,
the static import table (DLLs AND imported function names) and a set of
Windows-8+/10-only API markers. It never writes to the input file.

Important distinction for the bundled adb.exe:
  * api-ms-win-crt-*.dll  -> Universal CRT. Not Win10-only: Win7 SP1 gets it
                             from KB2999226 / VC++ 2015-2019 redist. Reported
                             as a prerequisite, not a hard failure.
  * api-ms-win-core-* etc -> Win8+/Win10 API sets, absent on Win7. Hard fail.
"""
import os
import struct
import sys

# APIs that are statically importable only on Windows 8.1+ / Windows 10.
WIN8_PLUS_MARKERS = [
    b"GetSystemTimePreciseAsFileTime",  # Win8
    b"CreatePseudoConsole",             # Win10 1809
    b"SetProcessDpiAwareness",          # Win8.1 (Win7 only has SetProcessDPIAware)
    b"CreateAppContainerProfile",       # Win8
    b"SetThreadDescription",            # Win10 1607 (harmless if dynamically resolved)
    b"libwinpthread-1.dll",             # MinGW runtime that must be bundled
]


def u16(buf, off):
    return struct.unpack_from("<H", buf, off)[0]


def u32(buf, off):
    return struct.unpack_from("<I", buf, off)[0]


def rva_to_offset(sections, rva):
    for va, vsize, raw, rawsize in sections:
        if va <= rva < va + max(vsize, rawsize):
            return raw + (rva - va)
    return None


def cstr(buf, off):
    end = buf.find(b"\0", off)
    if end < 0:
        end = min(off + 256, len(buf))
    return buf[off:end].decode("ascii", "replace")


def inspect(path, require_gui=False, label=""):
    with open(path, "rb") as f:
        data = f.read()
    name = label or os.path.basename(path)
    print("=== %s (%s bytes) ===" % (name, len(data)))
    problems = []
    notes = []

    pe = u32(data, 0x3C)
    if data[pe:pe + 4] != b"PE\0\0":
        print("  FATAL: not a PE image")
        return 1

    coff = pe + 4
    machine = u16(data, coff)
    nsec = u16(data, coff + 2)
    optsize = u16(data, coff + 16)
    opt = coff + 20
    magic = u16(data, opt)
    pe32plus = magic == 0x20B
    ptr_size = 8 if pe32plus else 4
    ordinal_bit = 1 << (ptr_size * 8 - 1)

    major_os = u16(data, opt + 40)
    minor_os = u16(data, opt + 42)
    major_sub = u16(data, opt + 48)
    minor_sub = u16(data, opt + 50)
    subsystem = u16(data, opt + 68)

    machine_name = {0x014C: "i386 (PE32)", 0x8664: "amd64 (PE32+)", 0xAA64: "arm64"}.get(machine, hex(machine))
    subsystem_name = {2: "GUI", 3: "CONSOLE", 1: "NATIVE"}.get(subsystem, str(subsystem))
    print("  Machine            : %s" % machine_name)
    print("  Optional magic     : %s" % ("PE32+" if pe32plus else "PE32"))
    print("  OS version         : %d.%d" % (major_os, minor_os))
    print("  Subsystem version  : %d.%d" % (major_sub, minor_sub))
    print("  Subsystem          : %d (%s)" % (subsystem, subsystem_name))

    if major_os > 6 or major_sub > 6:
        problems.append("OS/subsystem version requires Windows 8+")
    if require_gui and subsystem != 2:
        problems.append("expected GUI subsystem (2), got %d" % subsystem)
    if magic not in (0x10B, 0x20B):
        problems.append("unexpected optional header magic")

    sec_off = opt + optsize
    sections = []
    for i in range(nsec):
        s = sec_off + 40 * i
        vsize, va, rawsize, raw = u32(data, s + 8), u32(data, s + 12), u32(data, s + 16), u32(data, s + 20)
        sections.append((va, vsize, raw, rawsize))

    dd = opt + (96 if not pe32plus else 112)
    imp_rva, imp_size = u32(data, dd + 8), u32(data, dd + 12)
    dlls = {}
    off = rva_to_offset(sections, imp_rva) if imp_rva else None
    if off is not None:
        for i in range(0, min(imp_size, 16384), 20):
            ent = off + i
            if ent + 20 > len(data):
                break
            int_rva = u32(data, ent)
            name_rva = u32(data, ent + 12)
            if int_rva == 0 and name_rva == 0:
                break
            if name_rva == 0:
                continue
            no = rva_to_offset(sections, name_rva)
            if no is None or no >= len(data):
                continue
            dll = cstr(data, no)
            funcs = []
            thunk_rva = int_rva or u32(data, ent + 16)
            to = rva_to_offset(sections, thunk_rva) if thunk_rva else None
            if to is not None:
                for j in range(0, 4096):
                    t = u32(data, to + j * ptr_size) if not pe32plus else struct.unpack_from("<Q", data, to + j * ptr_size)[0]
                    if t == 0:
                        break
                    if t & ordinal_bit:
                        continue  # imported by ordinal, no name
                    fo = rva_to_offset(sections, t)
                    if fo is None or fo + 2 >= len(data):
                        continue
                    funcs.append(cstr(data, fo + 2))
            dlls.setdefault(dll, []).extend(funcs)

    all_dlls = sorted(dlls)
    print("  Imported DLLs      : %s" % (", ".join(all_dlls) or "(none / dynamically loaded)"))
    for dll, funcs in sorted(dlls.items()):
        low = dll.lower()
        if low.startswith("api-ms-win-crt-") or low.startswith("ucrtbase"):
            notes.append("UCRT API set (%s...): Win7 SP1 needs KB2999226 / VC++ 2015-2019 redist" % low[:24])
            continue
        if low.startswith("api-ms-win-") or low.startswith("ext-ms-win-"):
            problems.append("Win8+/Win10 API-set import absent on Win7: %s" % dll)
        if "libwinpthread" in low or "libgcc" in low or "vcruntime140" in low or "msvcp140" in low:
            problems.append("extra runtime DLL must be bundled: %s" % dll)
        for fn in funcs:
            if fn in ("GetSystemTimePreciseAsFileTime", "CreatePseudoConsole",
                      "SetProcessDpiAwareness", "CreateAppContainerProfile", "SetThreadDescription"):
                problems.append("Win8+/Win10-only function STATICALLY imported: %s!%s" % (dll, fn))

    all_funcs = {fn for fs in dlls.values() for fn in fs}
    found = [m.decode("ascii", "replace") for m in WIN8_PLUS_MARKERS if m in data]
    dynamic_only = [m.decode("ascii", "replace") for m in WIN8_PLUS_MARKERS
                    if m in data and m.decode("ascii", "replace") not in all_funcs]
    print("  Win8+/10 markers   : %s" % (", ".join(found) or "none"))
    print("  (dynamic only)     : %s" % (", ".join(dynamic_only) or "none"))

    for note in sorted(set(notes)):
        print("  NOTE               : %s" % note)
    if problems:
        print("  RESULT             : FAIL")
        for p in problems:
            print("    - %s" % p)
        return 1
    print("  RESULT             : PASS (Win7-compatible PE characteristics)")
    return 0


def main():
    args = [a for a in sys.argv[1:] if not a.startswith("--")]
    require_gui = "--require-gui" in sys.argv
    label = ""
    if "--label" in sys.argv:
        i = sys.argv.index("--label")
        if i + 1 < len(sys.argv):
            label = sys.argv[i + 1]
            if label in args:
                args.remove(label)
    if not args:
        print("usage: check_pe.py <file.exe> [--require-gui] [--label NAME]")
        return 2
    return inspect(args[0], require_gui, label)


if __name__ == "__main__":
    sys.exit(main())
