#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 Paul Fleischmann
#
# Add or verify SPDX-License-Identifier headers across all hand-written
# source files under code/.
#
# Usage:
#   python3 scripts/add-spdx-headers.py          # add missing headers
#   python3 scripts/add-spdx-headers.py --check  # exit 1 if any header is missing

import argparse
import os
import sys

ROOT = os.path.join(os.path.dirname(__file__), "..", "code")

HEADER_SLASH = (
    "// SPDX-License-Identifier: Apache-2.0\n"
    "// Copyright (c) 2026 Paul Fleischmann\n"
)
HEADER_HASH = (
    "# SPDX-License-Identifier: Apache-2.0\n"
    "# Copyright (c) 2026 Paul Fleischmann\n"
)

# Directories that contain only generated or vendored files
SKIP_DIRS = {"node_modules", "__pycache__", ".venv", "dist", "target", "gen"}

# File suffixes that indicate generated code
SKIP_SUFFIXES = ("_pb2.py", "_pb2_grpc.py")

EXT_MAP = {
    ".go": HEADER_SLASH,
    ".rs": HEADER_SLASH,
    ".ts": HEADER_SLASH,
    ".py": HEADER_HASH,
}


def is_generated(path: str) -> bool:
    parts = path.split(os.sep)
    if any(p in SKIP_DIRS for p in parts):
        return True
    return any(path.endswith(s) for s in SKIP_SUFFIXES)


def process(path: str, header: str, check: bool) -> str:
    """Return 'added', 'missing' (check mode), 'skip', or 'generated'."""
    if is_generated(path):
        return "generated"

    with open(path, encoding="utf-8") as fh:
        content = fh.read()

    if "SPDX-License-Identifier" in content:
        return "skip"

    if check:
        return "missing"

    with open(path, "w", encoding="utf-8") as fh:
        fh.write(header + "\n" + content)
    return "added"


def main() -> int:
    parser = argparse.ArgumentParser(description="Manage SPDX headers in source files.")
    parser.add_argument(
        "--check",
        action="store_true",
        help="Exit with code 1 if any file is missing its SPDX header.",
    )
    args = parser.parse_args()

    added, missing, skipped, generated = [], [], [], []

    for dirpath, dirnames, filenames in os.walk(os.path.abspath(ROOT)):
        dirnames[:] = [d for d in dirnames if d not in SKIP_DIRS]
        for fname in filenames:
            ext = os.path.splitext(fname)[1]
            if ext not in EXT_MAP:
                continue
            full = os.path.join(dirpath, fname)
            rel = os.path.relpath(full, os.path.join(ROOT, ".."))
            result = process(full, EXT_MAP[ext], args.check)
            {"added": added, "missing": missing, "skip": skipped, "generated": generated}[result].append(rel)

    if args.check:
        if missing:
            print(f"ERROR: {len(missing)} file(s) missing SPDX header:")
            for f in sorted(missing):
                print(f"  {f}")
            return 1
        print(f"OK: all {len(skipped)} file(s) have SPDX headers ({len(generated)} generated files skipped).")
        return 0

    if added:
        print(f"Added ({len(added)}):")
        for f in sorted(added):
            print(f"  + {f}")
    if skipped:
        print(f"Already present ({len(skipped)}) — no change.")
    if generated:
        print(f"Ignored ({len(generated)}) — generated files.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
