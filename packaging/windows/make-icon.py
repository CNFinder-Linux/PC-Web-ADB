#!/usr/bin/env python3
"""Generate the PC Web ADB (PWA) desktop icons (pure stdlib, no image library).

Outputs:
  * packaging/windows/pcwebadb.ico - 16x16 + 32x32 32-bit BGRA ICO. Windows
    builds go:embed it and turn the RT_ICON blob into an HICON with
    CreateIconFromResourceEx(), so no .res / windres step is needed.
  * packaging/pcwebadb-tray.png - 32x32 RGBA PNG. Linux builds go:embed it
    and write it to the config dir so the GTK status icon is correct even when
    the .deb is not installed (icon theme lookup would then fail).

The design is a blue/violet rounded square with a white "P" monogram; the 32px
image adds a small "WA" underneath, mirroring the Web UI brand mark "P|WA".
The 16px tray image keeps only the "P" so it stays legible at taskbar size.

ICO layout: ICONDIR + N * ICONDIRENTRY, each entry pointing at an RT_ICON
blob = BITMAPINFOHEADER (biHeight = 2 * height) + bottom-up BGRA XOR bits +
bottom-up 1bpp AND mask (1 = transparent).
"""

import math
import os
import struct
import zlib

HERE = os.path.dirname(os.path.abspath(__file__))
OUT_ICO = os.path.join(HERE, "pcwebadb.ico")
OUT_PNG = os.path.abspath(os.path.join(HERE, "..", "pcwebadb-tray.png"))


def segment_distance(px, py, ax, ay, bx, by):
    dx, dy = bx - ax, by - ay
    denom = dx * dx + dy * dy
    t = 0.0 if denom == 0 else max(0.0, min(1.0, ((px - ax) * dx + (py - ay) * dy) / denom))
    return math.hypot(px - (ax + t * dx), py - (ay + t * dy))


def strokes_for(size):
    """White glyph strokes in normalised top-down coordinates.

    32px+: a large "P" with a small "WA" underneath (the PWA monogram).
    16px: only the "P", thick enough to survive taskbar scaling.
    """
    if size < 24:
        half = 0.062
        p = (
            (0.335, 0.80, 0.335, 0.16),   # stem
            (0.335, 0.18, 0.575, 0.18),   # bowl top
            (0.575, 0.18, 0.695, 0.29),   # bowl upper-right
            (0.695, 0.29, 0.695, 0.44),   # bowl right
            (0.695, 0.44, 0.575, 0.55),   # bowl lower-right
            (0.575, 0.55, 0.335, 0.55),   # bowl bottom
        )
        return p, half
    half = 0.045
    p = (
        (0.235, 0.66, 0.235, 0.13),   # stem
        (0.235, 0.15, 0.435, 0.15),   # bowl top
        (0.435, 0.15, 0.535, 0.25),   # bowl upper-right
        (0.535, 0.25, 0.535, 0.37),   # bowl right
        (0.535, 0.37, 0.435, 0.47),   # bowl lower-right
        (0.435, 0.47, 0.235, 0.47),   # bowl bottom
    )
    small_half = 0.026
    wa = (
        # W
        (0.255, 0.66, 0.300, 0.88),
        (0.300, 0.88, 0.340, 0.74),
        (0.340, 0.74, 0.380, 0.88),
        (0.380, 0.88, 0.425, 0.66),
        # A
        (0.475, 0.88, 0.520, 0.66),
        (0.520, 0.66, 0.565, 0.88),
        (0.490, 0.80, 0.550, 0.80),
    )
    return tuple(p) + tuple(wa), (half, small_half)


def stroke_hits(u, v, strokes, half):
    if isinstance(half, tuple):
        # strokes are ordered: the first N belong to the big glyph
        big = strokes[:6]
        small = strokes[6:]
        if min(segment_distance(u, v, *sg) for sg in small) < half[1]:
            return True
        return min(segment_distance(u, v, *sg) for sg in big) < half[0]
    return min(segment_distance(u, v, *s) for s in strokes) < half


def rounded_rect_sdf(u, v, radius=0.22):
    qx = abs(u - 0.5) - (0.46 - radius)
    qy = abs(v - 0.5) - (0.46 - radius)
    return math.hypot(max(qx, 0.0), max(qy, 0.0)) + min(max(qx, qy), 0.0) - radius


def pixel(u, v, size, strokes, half):
    d = rounded_rect_sdf(u, v)
    if d > 0.006:
        return 0, 0, 0, 0
    alpha = 255 if d < -0.006 else int(255 * (0.006 - d) / 0.012)

    # Diagonal gradient: #2E6CE6 -> #623ABE, slightly brighter at top-left.
    t = max(0.0, min(1.0, 0.35 * u + 0.65 * v))
    r = int(46 + (98 - 46) * t)
    g = int(110 + (58 - 110) * t)
    b = int(230 + (190 - 230) * t)
    glow = int(18 * max(0.0, 1.0 - math.hypot(u - 0.18, v - 0.12) / 0.75))
    r, g, b = min(255, r + glow), min(255, g + glow), min(255, b + glow)

    # Thin inner rim for a bit of depth on small sizes.
    if -0.030 < d < -0.012:
        r, g, b = min(255, r + 26), min(255, g + 26), min(255, b + 26)

    if stroke_hits(u, v, strokes, half):
        feather = 1.0 if size >= 32 else 0.75  # keep 16x16 legible
        r = int(r + (255 - r) * feather)
        g = int(g + (255 - g) * feather)
        b = int(b + (255 - b) * feather)
    return b, g, r, alpha  # BGRA


def image_blob(size):
    strokes, half = strokes_for(size)
    xor = bytearray()
    and_rows = []
    for row in range(size):  # top-down, later reversed for bottom-up DIB
        v = (row + 0.5) / size
        bits = bytearray(((size + 31) // 32) * 4)
        for col in range(size):
            u = (col + 0.5) / size
            bgra = pixel(u, v, size, strokes, half)
            xor.extend(bgra)
            if bgra[3] < 128:
                bits[col // 8] |= 0x80 >> (col % 8)
        and_rows.append(bytes(bits))

    stride = size * 4
    xor_bottom_up = b"".join(
        bytes(xor[i * stride:(i + 1) * stride]) for i in range(size - 1, -1, -1)
    )
    and_bottom_up = b"".join(reversed(and_rows))
    payload = xor_bottom_up + and_bottom_up
    header = struct.pack(
        "<IiiHHIIiiII",
        40,             # biSize
        size,           # biWidth
        size * 2,       # biHeight = XOR + AND
        1,              # biPlanes
        32,             # biBitCount
        0,              # BI_RGB
        len(payload),   # biSizeImage
        0, 0, 0, 0,
    )
    return header + payload


def png_chunk(kind, data):
    body = kind + data
    return struct.pack(">I", len(data)) + body + struct.pack(">I", zlib.crc32(body) & 0xFFFFFFFF)


def png_blob(size):
    raw = bytearray()
    for row in range(size):
        raw.append(0)  # filter type: none
        strokes, half = strokes_for(size)
        for col in range(size):
            b, g, r, a = pixel((col + 0.5) / size, (row + 0.5) / size, size, strokes, half)
            raw.extend((r, g, b, a))
    ihdr = struct.pack(">IIBBBBB", size, size, 8, 6, 0, 0, 0)
    return (b"\x89PNG\r\n\x1a\n" + png_chunk(b"IHDR", ihdr) +
            png_chunk(b"IDAT", zlib.compress(bytes(raw), 9)) +
            png_chunk(b"IEND", b""))


def main():
    sizes = (16, 32)
    blobs = {n: image_blob(n) for n in sizes}
    entries = b""
    images = b""
    offset = 6 + 16 * len(sizes)
    for n in sizes:
        data = blobs[n]
        entries += struct.pack(
            "<BBBBHHII",
            n % 256, n % 256, 0, 0, 1, 32, len(data), offset,
        )
        images += data
        offset += len(data)
    ico = struct.pack("<HHH", 0, 1, len(sizes)) + entries + images
    with open(OUT_ICO, "wb") as fh:
        fh.write(ico)
    with open(OUT_PNG, "wb") as fh:
        fh.write(png_blob(32))
    print("wrote %s (%d bytes, %s)" %
          (OUT_ICO, len(ico), ", ".join("%dx%d" % (n, n) for n in sizes)))
    print("wrote %s (%d bytes, 32x32 RGBA)" %
          (OUT_PNG, os.path.getsize(OUT_PNG)))


if __name__ == "__main__":
    main()
