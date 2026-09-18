#!/usr/bin/env python3
"""Run the round's required unit-tagged suites, requiring executed PASS events."""

import json
import os
from pathlib import Path
import re
import subprocess
import sys
from urllib.parse import urlparse


# Prefix selection includes future cases; sentinels also catch deleted/renamed gates.
SUITES = {
    "usage-coverage": {
        "repository": (
            r"^Test(UsageDashboard|UsageCoverage|DashboardPublication)",
            {"TestUsageDashboardPostgres", "TestUsageDashboardChunkingPostgres",
             "TestUsageDashboardPartitionDropWaitsForCoverageReader",
             "TestUsageCoverageDoesNotTreatRetainedHourlyGapsAsZero",
             "TestDashboardPublicationRollsBackWithoutPublishingOnRebuildFailure"},
        ),
    },
    "catalog": {
        "service": (
            r"^Test(AccountModelCatalog|ModelCatalog|ModelCapability|PricingCapability|PlatformModel|PlatformPresetCoverage)",
            {"TestAccountModelCatalogConcurrentApplySameBaselineHasOneWinner",
             "TestModelCatalogAllowlistAndCredentialRevocations",
             "TestModelCapabilityV2RequiredFacts"},
        ),
        "handler/admin": (
            r"^Test(GetModelCatalog|ModelCatalog|CapabilityDefaults)",
            {"TestModelCatalogAllowlistConfirmationHTTP",
             "TestCapabilityDefaultsIdentityBindingAndReconfirmation"},
        ),
        "repository": (
            r"^TestModelCatalog",
            {"TestModelCatalogProjectionReadsOnlyEvidenceAndRules"},
        ),
    },
    "affinity": {
        "repository": (
            r"^TestAffinity",
            {"TestAffinityRealRedisCAS", "TestAffinityOpenAIRequestPlanRealRedisCAS"},
        ),
        "service": (
            r"^Test(GatewayRequestPlan|Affinity)",
            {"TestGatewayRequestPlanExitMatrix", "TestGatewayRequestPlanAffinityIngressMatrix",
             "TestAffinityIdentityTrustedScope"},
        ),
    },
}


def run(suite):
    env = os.environ.copy()
    if suite == "usage-coverage":
        dsn = urlparse(env.get("USAGE_STATS_TEST_DSN", ""))
        if dsn.scheme not in {"postgres", "postgresql"} or dsn.path != "/stats_test":
            raise SystemExit("USAGE_STATS_TEST_DSN must be a disposable stats_test database URL")
    if suite == "affinity":
        address = env.get("AFFINITY_REDIS_TEST_ADDR")
        if not address:
            raise SystemExit("AFFINITY_REDIS_TEST_ADDR must identify an isolated real Redis")

    backend = Path(__file__).resolve().parents[1]
    totals = {"pass": 0, "fail": 0, "skip": 0}
    errors = []
    for package, (pattern, sentinels) in SUITES[suite].items():
        command = ["go", "test", "-mod=readonly", "-tags=unit", "./internal/" + package]
        listing = subprocess.run(command + ["-list", pattern], cwd=backend, env=env,
                                 text=True, capture_output=True)
        if listing.returncode:
            raise SystemExit(listing.stdout + listing.stderr)
        required = {line for line in listing.stdout.splitlines() if re.match(pattern, line)}
        if not sentinels <= required:
            raise SystemExit(f"{package}: missing required tests: {sorted(sentinels - required)}")
        started, passed = set(), set()
        process = subprocess.Popen(command + ["-json", "-run", pattern, "-count=1", "-timeout=10m"],
                                   cwd=backend, env=env, stdout=subprocess.PIPE,
                                   text=True, encoding="utf-8")
        for line in process.stdout:
            print(line, end="", flush=True)
            event = json.loads(line)
            name, action = event.get("Test"), event["Action"]
            if not name:
                continue
            if action == "run":
                started.add(name)
            if action == "pass":
                passed.add(name)
            if action in totals:
                totals[action] += 1
            if action in {"skip", "fail"}:
                errors.append(f"{package}: {action}: {name}")
        if process.wait():
            errors.append(f"{package}: go test exited {process.returncode}")
        missing = (required | started) - (started & passed)
        if missing:
            errors.append(f"{package}: missing executed PASS events: {sorted(missing)}")
    print(f"{suite}: test events (including subtests): {totals}", flush=True)
    if errors:
        raise SystemExit("\n".join(errors))


if __name__ == "__main__":
    if len(sys.argv) != 2 or sys.argv[1] not in SUITES:
        raise SystemExit("Usage: required-round-tests.py " + "|".join(SUITES))
    run(sys.argv[1])
