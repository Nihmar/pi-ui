#!/usr/bin/env python3
"""Regenerates packaging/linux/piui.png.

The icon is drawn here rather than stored as a mystery blob: the plate, the rounded corners
and the π are a few lines of arithmetic, and the result is the 256×256 PNG the AppImage's
desktop entry points at.

    python3 packaging/linux/make_icon.py
"""
import pathlib
import struct
import zlib

SIZE = 256
ACCENT = (0x4C, 0x8D, 0xF6)
WHITE = (0xFF, 0xFF, 0xFF)
TRANSPARENT = (0, 0, 0, 0)


def inside_rounded(x: int, y: int, left: int, top: int, right: int, bottom: int, radius: int) -> bool:
    if x < left or x > right or y < top or y > bottom:
        return False
    for cx, cy in (
        (left + radius, top + radius),
        (right - radius, top + radius),
        (left + radius, bottom - radius),
        (right - radius, bottom - radius),
    ):
        if (x < left + radius or x > right - radius) and (y < top + radius or y > bottom - radius):
            if (x - cx) ** 2 + (y - cy) ** 2 > radius ** 2:
                return False
    return True


def pixel(x: int, y: int) -> tuple:
    if not inside_rounded(x, y, 8, 8, SIZE - 9, SIZE - 9, 48):
        return TRANSPARENT
    # The π: a crossbar and two legs.
    if 64 <= y <= 88 and 56 <= x <= 200:
        return WHITE
    if 88 < y <= 196 and (72 <= x <= 100 or 156 <= x <= 184):
        return WHITE
    return ACCENT


def chunk(kind: bytes, data: bytes) -> bytes:
    body = struct.pack(">I", len(data)) + kind + data
    return body + struct.pack(">I", zlib.crc32(kind + data) & 0xFFFFFFFF)


def main() -> None:
    raw = bytearray()
    for y in range(SIZE):
        raw.append(0)  # filter: none
        for x in range(SIZE):
            raw.extend(pixel(x, y))

    png = b"\x89PNG\r\n\x1a\n"
    png += chunk(b"IHDR", struct.pack(">IIBBBBB", SIZE, SIZE, 8, 6, 0, 0, 0))
    png += chunk(b"IDAT", zlib.compress(bytes(raw), 9))
    png += chunk(b"IEND", b"")

    path = pathlib.Path(__file__).with_name("piui.png")
    path.write_bytes(png)
    print(f"wrote {path} ({len(png)} bytes)")


if __name__ == "__main__":
    main()
