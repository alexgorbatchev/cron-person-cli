#!/usr/bin/env python3
"""Build lossless hook-generation cases; output is consumed by rust-anything."""
import json
import random
from pathlib import Path

rows = []


def case(function, *args):
    rows.append({"fn": function, "args": args})


for shell in ("bash", "zsh", "fish", "", "sh", "BASH", "bash\n", "powershell"):
    for binary in ("", "cron-person", "/opt/custom tool", "'\"$HOME`id`%\\\n", "雪/💩"):
        case("GenerateHookScript", shell, binary)

for shell in (b"bash", b"zsh", b"fish"):
    for binary in (bytes(range(256)), b"\x00\xff\xc0\x80\xed\xa0\x80", b"", b"a" * 8192):
        case("GenerateHookBytes", shell.hex(), binary.hex())

for value in range(256):
    case("GenerateHookBytes", bytes([value]).hex(), "")

# Printable categories, control escapes, Unicode separators, private/unassigned
# characters, noncharacters and Unicode 15 additions versus newer assignments.
points = [0x85, 0xA0, 0xAD, 0x300, 0x600, 0x61C, 0x1680, 0x180E,
          0x2000, 0x2007, 0x200B, 0x2028, 0x2029, 0x202F, 0x2060,
          0x3000, 0xD7FF, 0xE000, 0xF8FF, 0xFFFD, 0xFFFF, 0x10000,
          0x1FAE8, 0x2EBF0, 0x31350, 0xF0000, 0x10FFFF]
for point in points:
    case("GenerateHookBytes", chr(point).encode().hex(), "")
case("GenerateHookBytes", "zz", "")
case("GenerateHookBytes", "0", "")
case("GenerateHookBytes", "62617368", "zz")
case("GenerateHookBytes", "62617368", "0")
# Valid hex accepts either case, including the decoded binary.
case("GenerateHookBytes", "7A7368", "FF00aB")

rng = random.Random(1089)
for _ in range(128):
    value = bytes(rng.randrange(256) for _ in range(rng.randrange(1, 64)))
    case("GenerateHookBytes", value.hex(), "")

Path(__file__).with_suffix(".jsonl").write_text(
    "".join(json.dumps(row, ensure_ascii=True) + "\n" for row in rows)
)
