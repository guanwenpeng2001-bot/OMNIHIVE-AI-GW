#!/usr/bin/env python3
"""Print bounded test diagnostics; the caller preserves the test exit status.

Full logs remain available as the CI artifact. This script never evaluates or
overrides the strict test gate and does not turn a skipped test into a pass.
"""

from pathlib import Path
import re
import sys


def summarize(path: Path) -> str:
    lines = path.read_text(encoding="utf-8", errors="replace").splitlines(keepends=True)
    selected = set(range(max(0, len(lines) - 80), len(lines)))
    failure = re.compile(r"^(?:--- FAIL:|FAIL(?:\s|$)|panic:|go-test-strict: FAILED)")
    for index, line in enumerate(lines):
        if failure.search(line):
            selected.update(range(max(0, index - 22), min(len(lines), index + 30)))
    parts = []
    previous = -2
    for index in sorted(selected):
        if index != previous + 1:
            parts.append("\n... full intervening output is retained in the CI log artifact ...\n")
        parts.append(lines[index])
        previous = index
    output = "".join(parts)
    if len(output) > 180000:
        output = output[:90000] + "\n... remaining diagnostics are in the CI log artifact ...\n" + output[-90000:]
    return output


if __name__ == "__main__":
    if len(sys.argv) != 2:
        raise SystemExit("usage: summarize-go-test-log.py LOG_FILE")
    sys.stdout.write(summarize(Path(sys.argv[1])))
