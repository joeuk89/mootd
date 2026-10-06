#!/usr/bin/env python3
"""Throwaway prototype: print a batch the way the real printer will.

Usage: preview.py [batch.json] [--plain] [--kept | --cut] [--id g07]
"""

import argparse
import json
import os
import shutil
import sys
import textwrap
from pathlib import Path

RESET = "\x1b[0m"
DIM = "\x1b[2m"
BUBBLE_MAX_WIDTH = 56


def rgb(hex_colour):
    return tuple(int(hex_colour[i : i + 2], 16) for i in (1, 3, 5))


def fg(colour):
    r, g, b = colour
    return f"\x1b[38;2;{r};{g};{b}m"


def blend(stops, t):
    if len(stops) == 1:
        return stops[0]
    scaled = min(max(t, 0.0), 1.0) * (len(stops) - 1)
    i = min(int(scaled), len(stops) - 2)
    frac = scaled - i
    return tuple(round(a + (b - a) * frac) for a, b in zip(stops[i], stops[i + 1]))


def colour_art(greeting, plain):
    art = greeting["art"]
    if plain:
        return art
    colour = greeting["colour"]
    height = len(art)
    width = max(len(line) for line in art)
    out = []
    if colour["mode"] == "lines":
        for line, hex_colour in zip(art, colour["lines"]):
            out.append(fg(rgb(hex_colour)) + line + RESET)
        return out

    stops = [rgb(c) for c in colour["stops"]]
    for y, line in enumerate(art):
        cells = []
        for x, ch in enumerate(line):
            tx = x / max(width - 1, 1)
            ty = y / max(height - 1, 1)
            t = {"horizontal": tx, "vertical": ty, "diagonal": (tx + ty) / 2}[colour["direction"]]
            cells.append(fg(blend(stops, t)) + ch)
        out.append("".join(cells) + RESET)
    return out


def bubble(message, speaker_col, text_colour, plain, width):
    lines = textwrap.wrap(message, width=width) or [""]
    inner = max(len(line) for line in lines)
    paint = (lambda s: s) if plain else (lambda s: fg(rgb(text_colour)) + s + RESET)
    out = ["╭" + "─" * (inner + 2) + "╮"]
    for line in lines:
        out.append("│ " + paint(line.ljust(inner)) + " │")
    tail_col = min(max(speaker_col + 1, 2), inner)
    bottom = "╰" + "─" * (inner + 2) + "╯"
    out.append(bottom[:tail_col] + "┬" + bottom[tail_col + 1 :])
    out.append(" " * tail_col + "│")
    return out


def source_line(source, plain):
    text = f"re: {source['context']} · {source['outlet']}"
    if plain:
        return text
    return f"\x1b]8;;{source['url']}\x1b\\{DIM}{text}{RESET}\x1b]8;;\x1b\\"


def render(greeting, plain, columns):
    width = min(BUBBLE_MAX_WIDTH, columns - 6)
    lines = bubble(
        greeting["message"], greeting["speaker_col"], greeting["message_colour"], plain, width
    )
    lines += colour_art(greeting, plain)
    if greeting.get("source"):
        lines += ["", source_line(greeting["source"], plain)]
    return "\n".join("  " + line for line in lines)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("batch", nargs="?", default=str(Path(__file__).parent / "out/latest.json"))
    parser.add_argument("--plain", action="store_true")
    parser.add_argument("--kept", action="store_true")
    parser.add_argument("--cut", action="store_true")
    parser.add_argument("--id")
    args = parser.parse_args()

    plain = args.plain or not sys.stdout.isatty() or "NO_COLOR" in os.environ
    columns = shutil.get_terminal_size((80, 24)).columns
    batch = json.loads(Path(args.batch).read_text())

    for g in batch["greetings"]:
        if args.id and g["id"] != args.id:
            continue
        if args.kept and not g.get("kept"):
            continue
        if args.cut and g.get("kept"):
            continue
        header = f"{g['id']} · {g['category']} · {g['art_subject']}"
        if "score" in g:
            s = g["score"]
            verdict = "KEPT" if g["kept"] else "CUT"
            header += f" · humour {s['humour']} art {s['art']} · {verdict} · {s['reason']}"
        print(header if plain else DIM + header + RESET)
        print()
        print(render(g, plain, columns))
        print()
        print()


if __name__ == "__main__":
    main()
