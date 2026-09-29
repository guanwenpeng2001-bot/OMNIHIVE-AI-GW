"""Collect unapproved inputs through the shared Go parsers available to runtime enrichment.

Only this explicit command can fetch public registries. Existing candidate,
report, capture and validate commands remain offline and never publish pins.
"""
import argparse
from datetime import date
import json
from pathlib import Path
import subprocess

import catalog


SOURCES = ("models.dev", "openrouter", "litellm")
BACKEND = catalog.ROOT.parents[2]


def collect(source, fixture=None, verified_at=None):
    catalog.require(source in SOURCES, "unsupported capability source")
    command = ["go", "run", "./cmd/capability-collect", "--source", source]
    if fixture is not None:
        date.fromisoformat(verified_at)
        command += ["--input", str(Path(fixture).resolve()), "--verified-at", verified_at]
    else:
        command += ["--network"]
    result = subprocess.run(command, cwd=BACKEND, capture_output=True, text=True,
                            encoding="utf-8", timeout=120, check=True)
    return json.loads(result.stdout)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", choices=SOURCES, required=True)
    parser.add_argument("--input", type=Path)
    parser.add_argument("--verified-at")
    parser.add_argument("--network", action="store_true")
    parser.add_argument("--dry-run", action="store_true")
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    output = args.output.resolve()
    process_root = (BACKEND.parent / ".pi").resolve()
    catalog.require(output.is_relative_to(process_root), "collection output must be in this checkout's .pi directory")
    catalog.require(bool(args.input) != args.network, "choose local input or explicit --network")
    if args.dry_run:
        catalog.write(output, {"source": args.source, "network": args.network,
                               "registered": False, "approved": False,
                               "action": "collect candidate inputs only"})
        return
    value = collect(args.source, args.input, args.verified_at)
    # All parsing must succeed before replacing a prior candidate input file.
    catalog.write(output, value)


if __name__ == "__main__":
    main()
