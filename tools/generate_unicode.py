#!/usr/bin/env python3
"""Regenerate pinned Unicode scopes; downloads are hash-checked, never in builds."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import urllib.request

ROOT = Path(__file__).resolve().parents[1]

def records(text):
    for line in text.splitlines():
        line = line.split("#", 1)[0].strip()
        if line:
            yield [part.strip() for part in line.split(";")]

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source-dir", type=Path, help="Use already downloaded pinned source files")
    parser.add_argument("--gofmt", default="gofmt")
    args = parser.parse_args()
    metadata = json.loads((ROOT / "internal/engine/unicode-sources.json").read_text(encoding="utf-8"))
    texts = {}
    for name, source in metadata["sources"].items():
        if args.source_dir:
            data = (args.source_dir / name).read_bytes()
        else:
            with urllib.request.urlopen(source["url"], timeout=60) as response:
                data = response.read(4 * 1024 * 1024)
        if hashlib.sha256(data).hexdigest() != source["sha256"]:
            raise SystemExit(f"{name}: source hash mismatch")
        texts[name] = data.decode("utf-8")
    aliases = {row[2]: row[1] for row in records(texts["PropertyValueAliases.txt"]) if row[0] == "sc"}
    names = {value: key for key, value in aliases.items()}
    def ranges(name, convert=lambda value: value, selected=None):
        rows = []
        for row in records(texts[name]):
            if selected and row[1] != selected:
                continue
            bounds = row[0].split("..")
            rows.append((int(bounds[0], 16), int(bounds[-1], 16), convert(row[1])))
        rows.sort()
        merged = []
        for lo, hi, value in rows:
            if merged and lo <= merged[-1][1]:
                raise ValueError(f"{name}: overlapping ranges")
            if merged and lo == merged[-1][1] + 1 and value == merged[-1][2]:
                merged[-1] = (merged[-1][0], hi, value)
            else:
                merged.append((lo, hi, value))
        return merged
    output = [f'// Code generated from Unicode {metadata["version"]}; DO NOT EDIT.',
              '// Source hashes and license are in unicode-sources.json and UNICODE-LICENSE.txt.',
              'package engine', 'type unicodeScriptRange struct { lo, hi uint32; scripts string }']
    for variable, rows in [
        ("unicodeScripts", ranges("Scripts.txt", lambda value: aliases[value])),
        ("unicodeScriptExtensions", ranges("ScriptExtensions.txt")),
        ("unicodeEmoji", ranges("emoji-data.txt", lambda _: "Emoji", "Emoji")),
    ]:
        output.append(f"var {variable} = [...]unicodeScriptRange{{")
        output.extend(f'{{0x{lo:X}, 0x{hi:X}, {json.dumps(value)}}},' for lo, hi, value in rows)
        output.append("}")
    output.append("var scriptNames = map[string]string{")
    output.extend(f'{json.dumps(key)}: {json.dumps(value)},' for key, value in sorted(names.items()))
    output.append("}")
    target = ROOT / "internal/engine/unicode_data.go"
    target.write_text("\n".join(output) + "\n", encoding="utf-8", newline="\n")
    subprocess.run([args.gofmt, "-w", str(target)], check=True)

if __name__ == "__main__":
    main()
