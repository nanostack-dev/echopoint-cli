#!/usr/bin/env python3
"""Strip codegen-incompatible annotations from an OpenAPI spec for CLI generation.

The prod contract (https://api.echopoint.dev/openapi.yaml) maps many schemas to
backend/framework Go types via `x-go-type` + `x-go-type-import`. Those import
paths (echopoint/internal/..., nanostack-framework/pkg/..., echopoint-runner/...)
are not importable from this module, so oapi-codegen would emit code that does
not compile. Removing them falls back to plain enums / base types.

Kept on purpose: `x-go-type-skip-optional-pointer` and `x-enum-varnames` — no
imports, and they improve the generated client.

Usage:
    python3 scripts/strip_gotype.py <input.yaml> <output.yaml>
"""

import sys


def indent_of(line: str) -> int:
    return len(line) - len(line.lstrip(" "))


def strip(lines: list[str]) -> tuple[list[str], int, int]:
    out: list[str] = []
    skip_until_indent = None  # indent of an x-go-type-import block being skipped
    removed_type = removed_import = 0
    i = 0
    while i < len(lines):
        line = lines[i]
        stripped = line.lstrip(" ")

        # Skip nested children of an x-go-type-import block.
        if skip_until_indent is not None:
            if line.strip() == "" or indent_of(line) > skip_until_indent:
                i += 1
                continue
            skip_until_indent = None  # block ended; reprocess this line below

        if stripped.startswith("x-go-type-import:"):
            skip_until_indent = indent_of(line)
            removed_import += 1
            i += 1
            continue
        if stripped.startswith("x-go-type:"):
            removed_type += 1
            i += 1
            continue

        out.append(line)
        i += 1
    return out, removed_type, removed_import


def main() -> int:
    if len(sys.argv) != 3:
        print(__doc__)
        return 2
    src, dst = sys.argv[1], sys.argv[2]
    with open(src) as f:
        lines = f.readlines()
    out, removed_type, removed_import = strip(lines)
    with open(dst, "w") as f:
        f.writelines(out)
    print(f"stripped x-go-type: {removed_type}, x-go-type-import: {removed_import}")
    print(f"lines: {len(lines)} -> {len(out)} -> {dst}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
