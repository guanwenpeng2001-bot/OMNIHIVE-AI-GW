#!/usr/bin/env python3
"""Skip/coverage gate for `go test -json` (used by scripts/go-test-strict.sh).

The gate consumes the JSON event stream produced by `go test -json` and fails
when:

  1. a ``Test*`` event reports ``"Action": "skip"`` that is not covered by an
     explicit allowlist entry;
  2. ``-run`` was supplied but a package executed zero tests (the historical
     "contract test is a no-op" bug), emitted ``[no test files]`` or
     ``testing: warning: no tests to run``;
  3. the stream contains a ``fail`` or ``build-fail`` event.

The allowlist is deliberately split in two:

  * ``BASE_SKIP_ALLOWLIST`` - OS/structural skips that can never execute as a
    standalone test (Windows-only semantics, subprocess helpers, interactive
    browser bridge, a fixture that exposes fewer than two URLs, an external TLS
    capture service, and a production-only legacy SQL row).
  * ``PROFILE_SKIP_ALLOWLIST`` - extra entries for a specific gate profile.

No database/Redis/DSN skip is ever allowlisted: when a suite needs a service,
the CI job must provide it or the gate fails.

Run ``python3 scripts/test_go_test_strict.py --self-test`` to exercise the
parser with synthetic event streams.
"""

from __future__ import annotations

import argparse
import contextlib
import io
import json
import os
import sys
import unittest
from typing import Iterable, Iterator


# Documented, deliberately minimal. Keys are exact top-level test names; a
# skipped subtest is matched through its parent (``Parent/subtest``).
BASE_SKIP_ALLOWLIST = {
    "TestRealtimeSIGTERMReceiptBeforeCleanup": (
        "OS-specific: Windows cannot deliver SIGTERM; the portable cancellation "
        "test TestRealtimeShutdownReceiptBeforeCleanup runs instead"
    ),
    "TestBatchImageRecoveryCrashChild": (
        "subprocess helper: only meaningful when spawned with BATCH_RECOVERY_CHILD"
    ),
    "TestQuotaEventCommitProcess": (
        "subprocess helper: only meaningful when spawned by TestUserPlatformQuotaEventsPostgres"
    ),
    "TestQuotaEventConsumerProcess": (
        "subprocess helper: only meaningful when spawned by TestUserPlatformQuotaEventsPostgres"
    ),
    "TestSubscriptionBulkLiveCrashWorker": (
        "subprocess helper: only meaningful when spawned by TestSubscriptionBulkLiveCrashWindows"
    ),
    "TestSubscriptionBulkLiveBrowserBridge": (
        "interactive browser bridge mode: requires a real browser extension host"
    ),
    "TestCrashProcess": (
        "subprocess helper: only meaningful when spawned with BE06_STAGE"
    ),
    "TestURLAvailability_GetAvailableURLs_标记一个不可用": (
        "fixture-data: the built-in BaseURLs list contains fewer than two entries"
    ),
    "TestDialerBasicConnection": (
        "external service: needs TLSFINGERPRINT_NETWORK_TESTS=1 and external HTTPS "
        "connectivity; this is not a database/Redis gate"
    ),
    "TestJA3Fingerprint": (
        "external service: needs TLSFINGERPRINT_NETWORK_TESTS=1 and a live JA3 "
        "fingerprinting endpoint; this is not a database/Redis gate"
    ),
    "TestAllProfiles": (
        "external service: dials live HTTPS endpoints for every TLS profile and "
        "skips in -short mode; this is not a database/Redis gate"
    ),
    "TestAuthPendingIdentityService_UpsertAdoptionDecision_ClearsLegacyNullSessionReference": (
        "production-history fixture: legacy NULL pending_auth_session_id rows are "
        "rejected by the sqlite unit schema"
    ),
}


# Untagged tests are compiled by both -tags=unit and -tags=integration, so these
# provider/performance/OS-capability entries are shared by both CI profiles.
EXTERNAL_ONLY_SKIPS = {
    "TestCatalogHTTPPerformance": (
        "performance sampling: requires CATALOG_HTTP_PERFORMANCE_SAMPLES and "
        "CATALOG_PERFORMANCE_POSTGRES_DSN"
    ),
    "TestCatalogPerformanceLatencyMatrix": (
        "performance sampling: requires CATALOG_PERFORMANCE_SAMPLES"
    ),
    "TestCatalogGatewayFirstContentPerformance": (
        "performance sampling: requires CATALOG_HTTP_PERFORMANCE_SAMPLES and "
        "CATALOG_PERFORMANCE_POSTGRES_DSN"
    ),
    "TestPublicEgressNetworkNamespace": (
        "OS capability: requires an isolated network namespace"
    ),
    "TestEstimateOpenAIInputTokens_CompareWithOpenAIAPI": (
        "external provider: requires OPENAI_API_KEY for a live comparison"
    ),
    "TestPluginRuntimeIntegration": (
        "external artifact: requires the plugin package archive variable"
    ),
}


PROFILE_SKIP_ALLOWLIST = {
    # CI unit job: every database/Redis variable is supplied, so the only
    # remaining skips are the external-mode harness self-test and the untagged
    # provider/performance opt-ins.
    "unit": {
        **EXTERNAL_ONLY_SKIPS,
        "TestExternalTestResourceMode": (
            "harness self-test: only meaningful with OMNIHIVE_TEST_EXTERNAL_SERVICES=1; "
            "the integration job runs it"
        ),
    },
    # `make test-unit` / quality-gate on a developer workstation without the
    # dedicated CI fixture topology. The CI unit job sets IP_GROUP_DEDICATED_TESTS,
    # the bulk-live opt-in flag and OMNIHIVE_TEST_EXTERNAL_SERVICES and therefore
    # does not rely on these entries.
    "local": {
        **EXTERNAL_ONLY_SKIPS,
        "TestExternalTestResourceMode": (
            "harness self-test: requires OMNIHIVE_TEST_EXTERNAL_SERVICES=1, which "
            "only the CI matrix exports"
        ),
        "TestAccountFirstSelectionDedicatedPostgres": (
            "fixture topology: needs the dedicated PostgreSQL on 127.0.0.1:55439 "
            "(provided by the CI unit job)"
        ),
        "TestProxyGroupRepositoryDedicatedPostgres": (
            "fixture topology: needs the dedicated PostgreSQL on 127.0.0.1:55439 "
            "(provided by the CI unit job)"
        ),
        "TestProxyGroupCacheDedicatedRedis": (
            "fixture topology: needs the dedicated Redis on 127.0.0.1:56389 "
            "(provided by the CI unit job)"
        ),
        "TestSubscriptionBulkLivePostgresRedis": (
            "fixture topology: opt-in PG17/Redis live suite (CI unit job sets "
            "the bulk-live flag with PostgreSQL on 127.0.0.1:55480)"
        ),
        "TestSubscriptionBulkLiveAssign": (
            "fixture topology: opt-in PG17/Redis live suite (CI unit job sets "
            "the bulk-live flag with PostgreSQL on 127.0.0.1:55480)"
        ),
        "TestSubscriptionBulkLiveLargeIDReplay": (
            "fixture topology: opt-in PG17/Redis live suite (CI unit job sets "
            "the bulk-live flag with PostgreSQL on 127.0.0.1:55480)"
        ),
        "TestSubscriptionBulkLiveCrashWindows": (
            "fixture topology: opt-in PG17/Redis live suite (CI unit job sets "
            "the bulk-live flag with PostgreSQL on 127.0.0.1:55480)"
        ),
    },
    # CI integration step. Everything that needs PostgreSQL/Redis receives an
    # explicit DSN from the workflow; the remaining entries are external
    # providers, performance sampling opt-ins and the external-mode harness
    # self-test.
    "integration": {
        **EXTERNAL_ONLY_SKIPS,
        "TestExternalTestResourceMode": (
            "harness self-test for OMNIHIVE_TEST_EXTERNAL_SERVICES mode; the "
            "integration suite itself runs in managed disposable mode"
        ),
        "TestDialerAgainstCaptureServer": (
            "external service: requires TLSFINGERPRINT_CAPTURE_URL"
        ),
        "TestJA3Fingerprint": (
            "external service: requires a live TLS fingerprinting endpoint"
        ),
        "TestAllProfiles": (
            "external service: requires a live TLS fingerprinting endpoint"
        ),
    },
    # Local quality-gate runs with OMNIHIVE_TEST_EXTERNAL_SERVICES=1 against a
    # private disposable PostgreSQL. External mode forbids CREATE/DROP DATABASE,
    # so the suites that create throwaway databases skip here; CI runs the same
    # suites in managed mode with a disposable service container instead.
    "local-integration": {
        "TestCursorPlatformMigrationFreshAndUpgrade": (
            "external-mode DDL restriction: creates a throwaway database (CI runs it managed)"
        ),
        "TestLegacyMonitorUpgradeChain": (
            "external-mode DDL restriction: creates a throwaway database (CI runs it managed)"
        ),
        "TestLegacyMonitorAppliedChecksums": (
            "external-mode DDL restriction: creates a throwaway database (CI runs it managed)"
        ),
        "TestLegacyMonitorChecksumDrift": (
            "external-mode DDL restriction: creates a throwaway database (CI runs it managed)"
        ),
        "TestLegacyMonitorUnknownAppliedChecksum": (
            "external-mode DDL restriction: creates a throwaway database (CI runs it managed)"
        ),
        "TestLegacyUpgradeDDLMeasurement": (
            "external-mode DDL restriction: creates a throwaway database (CI runs it managed)"
        ),
        "TestCursorPassiveInventoryPostgres": (
            "external-mode DDL restriction: creates a throwaway database (CI runs it managed)"
        ),
        "TestQianwenRealtimeReceiptPostgres": (
            "external-mode DDL restriction: creates a throwaway database (CI runs it managed)"
        ),
        "TestNewPlatformMigrations": (
            "external-mode DDL restriction: the fresh=true chain needs a throwaway database "
            "(CI runs it managed)"
        ),
    },
    # `make test-e2e-live` against a real deployment. The two Antigravity-only
    # tests are mode-gated by design and cannot run in mixed mode.
    "e2e": {
        "TestClaudeMessagesWithGeminiModel": (
            "mode-gated: only runs with ENDPOINT_PREFIX=/antigravity"
        ),
        "TestGeminiEndpointWithClaudeModel": (
            "mode-gated: only runs with ENDPOINT_PREFIX=/antigravity"
        ),
    },
}


class StrictGate:
    """Consumes `go test -json` events and records gate violations."""

    def __init__(self, profile: str, run_present: bool, extra_allow: Iterable[str] = ()) -> None:
        self.profile = profile
        self.run_present = run_present
        self.allow = set(BASE_SKIP_ALLOWLIST)
        self.profiles = [entry for entry in profile.replace(",", " ").split() if entry]
        if not self.profiles:
            self.profiles = ["unit"]
        for name in self.profiles:
            self.allow.update(PROFILE_SKIP_ALLOWLIST.get(name, {}))
        self.allow.update(entry for entry in extra_allow if entry)
        self.violations: list[str] = []
        self.skips: list[str] = []
        self.allowed_skips: list[str] = []
        self.failures: list[str] = []
        self.started: dict[str, set[str]] = {}
        self.packages: list[str] = []
        self.no_test_files: set[str] = set()
        self.no_tests_to_run: set[str] = set()
        self.top_level_runs = 0

    def _allowed(self, name: str) -> bool:
        if name in self.allow:
            return True
        parent = name.split("/", 1)[0]
        return parent in self.allow

    def feed(self, event: dict) -> None:
        action = event.get("Action", "")
        name = event.get("Test", "")
        package = event.get("Package", "") or event.get("ImportPath", "")

        # Newer Go versions interleave BuildEvents with TestEvents. These use
        # ImportPath instead of Package; dropping them hides the actual compiler
        # error behind the later generic package/setup failure.
        if action == "build-output":
            sys.stdout.write(event.get("Output", ""))
            return

        if action == "build-fail":
            label = package or "unknown build target"
            self.failures.append(f"{label}: build failed")
            self.violations.append(f"{label}: build failed")
            return

        if action == "start" and package:
            self.packages.append(package)
            return

        if action == "output":
            text = event.get("Output", "")
            sys.stdout.write(text)
            if "[no test files]" in text:
                self.no_test_files.add(package)
                if self.run_present:
                    self.violations.append(
                        f"{package}: [no test files] while -run was supplied"
                    )
            if "testing: warning: no tests to run" in text:
                self.no_tests_to_run.add(package)
                self.violations.append(
                    f"{package}: -run matched no tests (testing: warning: no tests to run)"
                )
            return

        if action == "run" and name:
            self.started.setdefault(package, set()).add(name)
            if "/" not in name:
                self.top_level_runs += 1
            return

        if action == "skip":
            if not name:
                # A package-level skip is emitted for packages without test
                # files; the output handler above decides whether that is fatal.
                return
            if self._allowed(name):
                self.allowed_skips.append(f"{package}: {name}")
            else:
                self.skips.append(f"{package}: {name}")
                self.violations.append(f"{package}: skipped {name}")
            return

        if action == "fail":
            if name:
                self.failures.append(f"{package}: {name}")
                self.violations.append(f"{package}: failed {name}")
            else:
                self.failures.append(package)
                self.violations.append(f"{package}: package failed")
                self._check_package_completion(package)
            return

        if action in {"pass", "skip"} and not name:
            self._check_package_completion(package)

    def _check_package_completion(self, package: str) -> None:
        if not self.run_present or not package:
            return
        if package in self.no_test_files:
            return
        if self.started.get(package):
            return
        message = f"{package}: -run executed zero tests (build tags or -run mismatch)"
        if message not in self.violations:
            self.violations.append(message)

    def finish(self) -> int:
        if self.run_present and self.top_level_runs == 0:
            self.violations.append("-run executed zero top-level tests")

        if self.skips:
            print("", file=sys.stderr)
            print("go-test-strict: unexpected skipped tests:", file=sys.stderr)
            for entry in self.skips:
                print(f"  - {entry}", file=sys.stderr)
            print(
                "  Provide the missing PostgreSQL/Redis/DSN environment instead of "
                "letting the suite skip, or add a documented allowlist entry only "
                "for OS/structural skips.",
                file=sys.stderr,
            )

        if self.failures:
            print("", file=sys.stderr)
            print("go-test-strict: failing tests:", file=sys.stderr)
            for entry in self.failures:
                print(f"  - {entry}", file=sys.stderr)

        if self.allowed_skips:
            print("", file=sys.stderr)
            print("go-test-strict: documented allowlisted skips:", file=sys.stderr)
            for entry in self.allowed_skips:
                print(f"  - {entry}", file=sys.stderr)

        if self.violations:
            print("", file=sys.stderr)
            print(
                f"go-test-strict: FAILED with {len(self.violations)} violation(s) "
                f"(profile={self.profile}, packages={len(self.packages)}, "
                f"top-level tests executed={self.top_level_runs})",
                file=sys.stderr,
            )
            for entry in self.violations:
                print(f"  - {entry}", file=sys.stderr)
            if os.environ.get("GITHUB_ACTIONS") == "true":
                print(
                    f"::error title=go-test-strict::{len(self.violations)} skip/coverage violation(s)",
                    file=sys.stderr,
                )
            return 1

        print(
            f"\ngo-test-strict: PASS (profile={self.profile}, "
            f"packages={len(self.packages)}, top-level tests executed="
            f"{self.top_level_runs}, allowlisted skips={len(self.allowed_skips)})",
            file=sys.stderr,
        )
        return 0


def run_check(profile: str, run_present: bool, extra_allow: Iterable[str], stream: Iterable[str]) -> int:
    gate = StrictGate(profile=profile, run_present=run_present, extra_allow=extra_allow)
    for line in stream:
        line = line.rstrip("\n")
        if not line:
            continue
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            # Build errors and toolchain chatter are not JSON; surface them.
            print(line)
            continue
        gate.feed(event)
    return gate.finish()


def synthetic(events: Iterable[dict]) -> list[dict]:
    return list(events)


def package_events(tests: Iterable[tuple[str, str]], package: str = "e/example") -> Iterator[dict]:
    """Yield run/pass (or run/skip) events for ``tests``."""
    for name, outcome in tests:
        yield {"Action": "output", "Package": package, "Output": f"=== RUN   {name}\n"}
        yield {"Action": "run", "Package": package, "Test": name}
        if outcome == "pass":
            yield {"Action": "output", "Package": package, "Output": f"--- PASS: {name} (0.00s)\n"}
            yield {"Action": "pass", "Package": package, "Test": name}
        else:
            yield {"Action": "output", "Package": package, "Output": f"--- SKIP: {name} (0.00s)\n"}
            yield {"Action": "skip", "Package": package, "Test": name}
    yield {"Action": "pass", "Package": package}


class StrictGateSelfTest(unittest.TestCase):
    def evaluate(
        self,
        events: Iterable[dict],
        *,
        profile: str = "unit",
        run_present: bool = False,
        extra_allow: Iterable[str] = (),
    ) -> int:
        lines = [json.dumps(event) for event in events]
        sink = io.StringIO()
        with contextlib.redirect_stdout(sink), contextlib.redirect_stderr(sink):
            return run_check(profile, run_present, extra_allow, lines)

    def test_pass_event_succeeds(self) -> None:
        events = package_events([("TestPasses", "pass")])
        self.assertEqual(0, self.evaluate(events))

    def test_unexpected_skip_fails(self) -> None:
        events = package_events([("TestBillingPostgres", "skip")])
        self.assertEqual(1, self.evaluate(events))

    def test_windows_only_skip_is_allowed(self) -> None:
        events = package_events([("TestRealtimeSIGTERMReceiptBeforeCleanup", "skip")])
        self.assertEqual(0, self.evaluate(events))

    def test_allowlisted_parent_covers_subtest_skip(self) -> None:
        events = package_events([("TestClaudeMessagesWithGeminiModel/model", "skip")], package="e/e2e")
        self.assertEqual(0, self.evaluate(events, profile="e2e"))

    def test_db_skip_is_not_allowlisted_in_integration_profile(self) -> None:
        events = package_events([("TestWorkspaceMigrationAndRepositoryIsolation", "skip")])
        self.assertEqual(1, self.evaluate(events, profile="integration"))

    def test_failure_event_fails(self) -> None:
        events = list(package_events([("TestPasses", "pass")]))
        events.append({"Action": "fail", "Package": "e/example", "Test": "TestPasses"})
        self.assertEqual(1, self.evaluate(events))

    def test_build_failure_fails_without_test_failure_event(self) -> None:
        events = list(package_events([("TestOtherPackagePasses", "pass")]))
        events.append({"Action": "build-fail", "ImportPath": "e/broken [e/broken.test]"})
        self.assertEqual(1, self.evaluate(events))

    def test_build_output_and_import_path_are_visible(self) -> None:
        output, errors = io.StringIO(), io.StringIO()
        events = [
            {"Action": "build-output", "ImportPath": "e/broken", "Output": "broken_test.go:9: undefined: missingFixture\n"},
            {"Action": "build-fail", "ImportPath": "e/broken"},
        ]
        with contextlib.redirect_stdout(output), contextlib.redirect_stderr(errors):
            result = run_check("unit", False, (), [json.dumps(event) for event in events])
        self.assertEqual(1, result)
        self.assertIn("undefined: missingFixture", output.getvalue())
        self.assertIn("e/broken: build failed", errors.getvalue())

    def test_build_output_does_not_count_as_executed_test(self) -> None:
        events = [{"Action": "build-output", "ImportPath": "e/example", "Output": "compiler diagnostic\n"}]
        self.assertEqual(1, self.evaluate(events, run_present=True))

    def test_build_output_alone_does_not_reject_a_successful_test(self) -> None:
        events = [{"Action": "build-output", "ImportPath": "e/example", "Output": "compiler warning\n"}]
        events.extend(package_events([("TestPasses", "pass")]))
        self.assertEqual(0, self.evaluate(events))

    def test_run_with_zero_tests_fails(self) -> None:
        events = [
            {"Action": "output", "Package": "e/example", "Output": "ok  \te/example\t0.01s\n"},
            {"Action": "pass", "Package": "e/example"},
        ]
        self.assertEqual(1, self.evaluate(events, run_present=True))

    def test_no_test_files_with_run_fails(self) -> None:
        events = [
            {"Action": "output", "Package": "e/empty", "Output": "?   \te/empty\t[no test files]\n"},
            {"Action": "skip", "Package": "e/empty"},
        ]
        self.assertEqual(1, self.evaluate(events, run_present=True))

    def test_no_tests_to_run_warning_fails(self) -> None:
        events = [
            {"Action": "output", "Package": "e/example", "Output": "testing: warning: no tests to run\n"},
            {"Action": "output", "Package": "e/example", "Output": "PASS\n"},
            {"Action": "pass", "Package": "e/example"},
        ]
        self.assertEqual(1, self.evaluate(events, run_present=True))

    def test_extra_allowlist_is_honoured(self) -> None:
        events = package_events([("TestLocalFixtureOnly", "skip")])
        self.assertEqual(
            0,
            self.evaluate(events, extra_allow=["TestLocalFixtureOnly"]),
        )

    def test_profile_merge_is_additive(self) -> None:
        events = package_events([("TestNewPlatformMigrations/fresh=true", "skip")])
        self.assertEqual(1, self.evaluate(events, profile="integration"))
        self.assertEqual(
            0,
            self.evaluate(events, profile="local,integration,local-integration"),
        )


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="parse go test -json from stdin")
    parser.add_argument("--self-test", action="store_true", help="run the parser self-test")
    parser.add_argument(
        "--profile",
        default=os.environ.get("GO_TEST_STRICT_PROFILE", "unit"),
        help="allowlist profile (unit, integration, e2e, local, contracts)",
    )
    parser.add_argument(
        "--run-present",
        choices=("0", "1"),
        default="0",
        help="whether -run was supplied to go test",
    )
    parser.add_argument(
        "--extra-allow",
        default=os.environ.get("GO_TEST_STRICT_EXTRA_ALLOW", ""),
        help="whitespace/comma separated extra allowlisted test names",
    )
    args = parser.parse_args(argv)

    if args.self_test:
        suite = unittest.defaultTestLoader.loadTestsFromTestCase(StrictGateSelfTest)
        result = unittest.TextTestRunner(verbosity=2).run(suite)
        return 0 if result.wasSuccessful() else 1

    if not args.check and not sys.stdin.isatty():
        args.check = True
    if not args.check:
        parser.error("nothing to do: pass --check or --self-test")

    extra = [entry for entry in args.extra_allow.replace(",", " ").split() if entry]
    return run_check(args.profile, args.run_present == "1", extra, sys.stdin)


if __name__ == "__main__":
    sys.exit(main())
