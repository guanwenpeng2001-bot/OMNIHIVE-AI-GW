import contextlib
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import check_pnpm_audit_exceptions as audit


class AuditReportTests(unittest.TestCase):
    def run_report(self, report, exceptions="version: 1\nexceptions:\n"):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "audit.json").write_text(json.dumps(report), encoding="utf-8")
            (root / "exceptions.yml").write_text(exceptions, encoding="utf-8")
            output = io.StringIO()
            with patch("sys.argv", ["audit", "--audit", str(root / "audit.json"),
                                    "--exceptions", str(root / "exceptions.yml")]), \
                    contextlib.redirect_stdout(output), contextlib.redirect_stderr(output):
                result = audit.main()
            return result, output.getvalue()

    def test_clean_supported_reports(self):
        for key in ("advisories", "vulnerabilities"):
            with self.subTest(key=key):
                self.assertEqual(self.run_report({key: {}})[0], 0)

    def test_scanner_failures_and_unknown_schemas_fail_closed(self):
        for report in ({}, [], None, {"error": {"code": "ECONNRESET"}},
                       {"advisories": {}, "error": "registry unavailable"},
                       {"vulnerabilities": {}, "errors": ["bad response"]},
                       {"advisories": []}, {"vulnerabilities": {"x": None}}):
            with self.subTest(report=report):
                code, output = self.run_report(report)
                self.assertEqual(code, 1)
                self.assertIn("Invalid audit report", output)

    def test_high_severity_without_via_cannot_disappear(self):
        for via in ([], None):
            report = {"vulnerabilities": {"unsafe": {"severity": "high", "via": via}}}
            code, output = self.run_report(report)
            self.assertEqual(code, 1)
            self.assertIn("missing advisory id", output)

    def test_high_severity_summary_requires_findings(self):
        report = {"advisories": {}, "metadata": {"vulnerabilities": {"high": 1}}}
        self.assertEqual(self.run_report(report)[0], 1)

    def test_invalid_findings_fail_closed(self):
        for finding in ({"module_name": "x"}, {"severity": "high"},
                        {"module_name": "x", "severity": "unknown"}):
            with self.subTest(finding=finding):
                self.assertEqual(self.run_report({"advisories": {"1": finding}})[0], 1)

    def test_numeric_advisory_source_is_reported(self):
        report = {"vulnerabilities": {"unsafe": {"severity": "critical",
                  "via": [{"source": 12345}]}}}
        code, output = self.run_report(report)
        self.assertEqual(code, 1)
        self.assertIn("12345", output)

    def test_current_exception_passes_and_expired_exception_fails(self):
        report = {"advisories": {"1": {"module_name": "unsafe", "severity": "high",
                  "github_advisory_id": "GHSA-example"}}}
        for expiry, expected in (("2999-01-01", 0), ("2000-01-01", 1)):
            exceptions = ("version: 1\nexceptions:\n"
                          "  - package: unsafe\n    advisory: GHSA-example\n"
                          "    severity: high\n    mitigation: restricted use\n"
                          f"    expires_on: {expiry}\n")
            self.assertEqual(self.run_report(report, exceptions)[0], expected)

    def test_unknown_advisory_is_not_covered_by_same_package_exception(self):
        report = {"advisories": {"1": {"module_name": "unsafe", "severity": "high",
                  "github_advisory_id": "GHSA-new"}}}
        exceptions = ("version: 1\nexceptions:\n  - package: unsafe\n"
                      "    advisory: GHSA-old\n    severity: high\n"
                      "    mitigation: restricted use\n    expires_on: 2999-01-01\n")
        self.assertEqual(self.run_report(report, exceptions)[0], 1)


if __name__ == "__main__":
    unittest.main()
