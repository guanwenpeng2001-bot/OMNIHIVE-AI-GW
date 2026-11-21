#!/usr/bin/env python3
"""Check Compose shutdown grace against the application shutdown budget."""

from pathlib import Path
import re

import yaml


REPO_ROOT = Path(__file__).resolve().parents[2]
MAIN = (REPO_ROOT / "backend/cmd/server/main.go").read_text(encoding="utf-8")
BUDGET = re.search(
    r"context\.WithTimeout\(context\.Background\(\),\s*(\d+)\s*\*\s*time\.Second\)",
    MAIN,
)
assert BUDGET, "application shutdown budget is missing"
minimum_grace = int(BUDGET.group(1)) + 10

for compose_path in sorted((REPO_ROOT / "deploy").glob("docker-compose*.yml")):
    document = yaml.safe_load(compose_path.read_text(encoding="utf-8"))
    application = document["services"]["omnihive"]
    grace = application.get("stop_grace_period")
    assert grace is not None, f"{compose_path.name}: omnihive stop_grace_period is missing"
    match = re.fullmatch(r"(\d+)s", str(grace))
    assert match, f"{compose_path.name}: invalid stop_grace_period {grace!r}"
    assert int(match.group(1)) >= minimum_grace, (
        f"{compose_path.name}: {grace} is shorter than the {minimum_grace}s "
        "application budget plus headroom"
    )
    print(f"PASS {compose_path.name}: stop_grace_period={grace}")
