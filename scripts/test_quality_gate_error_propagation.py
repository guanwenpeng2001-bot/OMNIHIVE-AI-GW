#!/usr/bin/env python3
"""Exercise the real gate functions without invoking Go, Node or test services."""

from __future__ import annotations

import contextlib
import os
from pathlib import Path
import signal
import subprocess
import tempfile
import unittest


GATE = Path(__file__).resolve().parent / "quality-gate.sh"
DRIVER_MARKER = '\necho "quality gate ${RUN_ID}" | tee "$SUMMARY"\n'


class QualityGateErrorPropagationTest(unittest.TestCase):
    @contextlib.contextmanager
    def run_gate(self, setup: str, stages: str):
        # Use the production definitions and summary driver. Compiler/network
        # commands are shell stubs in the cases below, never real tool invocations.
        source = GATE.read_text()
        self.assertEqual(1, source.count(DRIVER_MARKER))
        prelude, driver = source.split(DRIVER_MARKER)
        with tempfile.TemporaryDirectory(prefix="omnihive-gate-test-") as directory:
            root = Path(directory)
            (root / "scripts").mkdir()
            (root / "backend").mkdir()
            (root / "frontend" / "node_modules").mkdir(parents=True)
            (root / "scripts" / "gate-library.sh").write_text(prelude)
            (root / "scripts" / "release_compatibility.py").write_text('print("compatibility checked")\n')
            harness = root / "harness.sh"
            harness.write_text(
                'source "${FIXTURE_ROOT}/scripts/gate-library.sh"\n'
                'git() { printf "fixture\\n"; }\n'
                'trace() { printf "%s\\n" "$*" >>"${ROOT}/trace"; }\n'
                # Unexpected external tool invocation is an immediate test
                # failure even when a local machine has a real compiler in PATH.
                'go() { trace "UNEXPECTED go $*"; return 91; }\n'
                'pnpm() { trace "UNEXPECTED pnpm $*"; return 92; }\n'
                'npm() { trace "UNEXPECTED npm $*"; return 93; }\n'
                'curl() { trace "UNEXPECTED curl $*"; return 94; }\n'
                'stage_after() {\n'
                '  [[ -z "${STAGE_TEST_EXPORT:-}" ]]\n'
                '  [[ "$PWD" == "$ROOT" ]]\n'
                '  trace "after completed"\n'
                '}\n'
                + setup
                + f'\nSELECTED=({stages})\n'
                + DRIVER_MARKER
                + driver
            )
            environment = os.environ.copy()
            environment["FIXTURE_ROOT"] = str(root)
            environment["QUALITY_GATE_LOG_DIR"] = str(root / "logs")
            try:
                result = subprocess.run(
                    ["bash", str(harness)], env=environment,
                    text=True, capture_output=True, timeout=20, check=False,
                )
                yield root, result
            finally:
                # A broken cleanup implementation must not leak the harmless
                # background sleep processes used by the cleanup regression test.
                pid_file = root / "pids"
                if pid_file.exists():
                    for raw in pid_file.read_text().split():
                        try:
                            os.kill(int(raw), signal.SIGTERM)
                        except ProcessLookupError:
                            pass

    def assert_failed_then_continued(self, root: Path, result: subprocess.CompletedProcess, stage: str):
        self.assertEqual(1, result.returncode, result.stdout + result.stderr)
        self.assertTrue((root / "logs" / "summary.txt").exists(), result.stdout + result.stderr)
        summary = (root / "logs" / "summary.txt").read_text()
        self.assertRegex(summary, rf"(?m)^{stage}\s+FAIL\s")
        self.assertRegex(summary, r"(?m)^after\s+PASS\s")
        self.assertIn("after completed", (root / "trace").read_text())
        self.assertNotIn("all selected stages passed", result.stdout)

    def test_failed_command_stops_stage_and_next_stage_runs(self):
        setup = '''
stage_probe() {
  cd "$ROOT/backend"
  export STAGE_TEST_EXPORT=must_not_escape
  trace "probe started"
  false
  trace "must not execute after failure"
}
'''
        with self.run_gate(setup, "probe after") as (root, result):
            self.assert_failed_then_continued(root, result, "probe")
            self.assertNotIn("must not execute", (root / "trace").read_text())

    def test_command_substitution_failure_cannot_be_overwritten_by_success(self):
        setup = '''
stage_substitution() {
  local value
  value="$(false; trace 'must not execute in substitution'; printf false-green)"
  trace "must not execute after substitution"
}
'''
        with self.run_gate(setup, "substitution after") as (root, result):
            self.assert_failed_then_continued(root, result, "substitution")
            self.assertNotIn("must not execute", (root / "trace").read_text())

    def test_generated_drift_stops_on_generator_failure_before_clean_diff(self):
        setup = '''
go() { trace "go $*"; return 23; }
npm() { trace "must not execute npm"; return 0; }
git() {
  if [[ "${1:-}" == diff ]]; then trace "must not execute clean git diff"; fi
  printf "fixture\\n"
}
'''
        with self.run_gate(setup, "generated-drift after") as (root, result):
            self.assert_failed_then_continued(root, result, "generated-drift")
            trace = (root / "trace").read_text()
            self.assertIn("go generate ./cmd/server", trace)
            self.assertNotIn("must not execute", trace)
            self.assertNotIn("go run", trace)

    def test_smoke_frontend_failure_prevents_services_and_binary_build(self):
        setup = '''
openssl() { trace "must not execute openssl"; return 0; }
install_jq() { trace "jq fixture ready"; }
pnpm() { trace "pnpm $*"; return 24; }
ensure_services() { trace "must not execute service startup"; }
go() { trace "must not execute go build"; return 0; }
'''
        with self.run_gate(setup, "smoke after") as (root, result):
            self.assert_failed_then_continued(root, result, "smoke")
            trace = (root / "trace").read_text()
            self.assertIn("pnpm run build", trace)
            self.assertNotIn("must not execute", trace)

    def test_failure_cleans_up_started_smoke_and_redis_processes(self):
        setup = '''
stage_cleanup() {
  SMOKE_BUILD_DIR="$ROOT/smoke-state"
  REDIS_TEMP="$ROOT/redis-state"
  mkdir -p "$SMOKE_BUILD_DIR" "$REDIS_TEMP"
  sleep 30 & SMOKE_BOOTSTRAP_PID=$!
  sleep 30 & SMOKE_SERVER_PID=$!
  sleep 30 & REDIS_PID=$!
  printf '%s\\n' "$SMOKE_BOOTSTRAP_PID" "$SMOKE_SERVER_PID" "$REDIS_PID" >"$ROOT/pids"
  false
}
'''
        with self.run_gate(setup, "cleanup after") as (root, result):
            self.assert_failed_then_continued(root, result, "cleanup")
            self.assertFalse((root / "smoke-state").exists())
            self.assertFalse((root / "redis-state").exists())
            for pid in (root / "pids").read_text().split():
                with self.assertRaises(ProcessLookupError):
                    os.kill(int(pid), 0)

    def test_explicit_failure_collection_still_runs_each_command(self):
        setup = '''
stage_collect() {
  local status=0
  false || status=1
  trace "collected first failure"
  false || status=1
  trace "collected second failure"
  return "$status"
}
'''
        with self.run_gate(setup, "collect after") as (root, result):
            self.assert_failed_then_continued(root, result, "collect")
            trace = (root / "trace").read_text()
            self.assertIn("collected first failure", trace)
            self.assertIn("collected second failure", trace)


if __name__ == "__main__":
    unittest.main(verbosity=2)
