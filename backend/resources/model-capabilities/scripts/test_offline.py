"""CLI regression tests use a disposable, explicitly synthetic release tree."""
import copy
from pathlib import Path
import runpy
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

import catalog
import release


class OfflineCLITests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.workspace = Path(self.temporary.name)
        self.backend = self.workspace / 'backend'
        self.scripts = self.backend / 'resources/model-capabilities/scripts'
        self.scripts.mkdir(parents=True)
        for name in ('catalog.py', 'evidence.py', 'release.py', 'inputs.json', 'reviewed.json'):
            shutil.copyfile(catalog.ROOT / name, self.scripts / name)
        self.output = self.workspace / '.pi/results'
        self.inputs = self.workspace / '.pi/inputs.json'
        self.inputs.parent.mkdir()
        shutil.copyfile(catalog.INPUTS, self.inputs)
        for name in release.VERSION_FILES:
            target = self.backend / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(b'// Synthetic dependency for offline release tests.\n')
        prices = self.backend / 'resources/model-pricing/model_prices_and_context_window.json'
        prices.parent.mkdir(parents=True)
        prices.write_bytes(b'{}\n')
        self.value = catalog.read(catalog.CATALOG)
        self.resource = self.scripts.parent
        catalog.write(self.resource / 'catalog.json', self.value)
        with patch.object(release, 'BACKEND', self.backend):
            self.manifest = release.manifest_for(self.value, catalog.read(catalog.REVIEWED))
        catalog.write(self.resource / 'release-manifest.json', self.manifest)
        catalog.write(self.resource / 'release-pin.json', {
            'approved_generation': self.manifest['approved_generation'],
            'manifest_sha256': catalog.digest(catalog.encode(self.manifest))})
        self.pin = (self.resource / 'release-pin.json').read_bytes()

    def run_command(self, command, *arguments):
        result = subprocess.run([sys.executable, str(self.scripts / 'catalog.py'), command,
                                 '--inputs', str(self.inputs), '--output-dir', str(self.output),
                                 *map(str, arguments)], cwd=self.workspace,
                                capture_output=True, text=True)
        self.assertEqual(self.pin, (self.resource / 'release-pin.json').read_bytes())
        return result

    def prepare_report(self):
        result = self.run_command('report')
        self.assertEqual(0, result.returncode, result.stderr)

    def test_candidate_report_and_checked_release_are_reproducible(self):
        self.assertEqual(0, self.run_command('candidate').returncode)
        self.assertEqual(catalog.encode(self.value), (self.output / 'catalog.json').read_bytes())
        self.prepare_report()
        before = {path.name: path.read_bytes() for path in self.output.iterdir()}
        result = self.run_command('validate', '--check-artifacts')
        self.assertEqual(0, result.returncode, result.stderr)
        self.prepare_report()
        self.assertEqual(before, {path.name: path.read_bytes() for path in self.output.iterdir()})
        self.assertFalse((self.scripts / 'reports').exists())
        self.assertFalse((self.scripts / 'candidate').exists())

    def test_missing_and_changed_release_dependencies_are_all_reported(self):
        self.prepare_report()
        (self.backend / release.VERSION_FILES[0]).unlink()
        (self.backend / release.VERSION_FILES[1]).write_bytes(b'// Changed fixture.\n')
        result = self.run_command('validate', '--check-artifacts')
        self.assertEqual(1, result.returncode)
        self.assertIn('missing release dependency: ' + release.VERSION_FILES[0], result.stderr)
        self.assertIn('code_versions.' + release.VERSION_FILES[1], result.stderr)

    def test_required_catalog_rejects_drift_before_go_and_resumes_after_restore(self):
        runner = self.backend / 'scripts/required-round-tests.py'
        runner.parent.mkdir()
        shutil.copyfile(release.BACKEND / 'scripts/required-round-tests.py', runner)
        run = runpy.run_path(str(runner))['run']
        dependency = self.backend / release.VERSION_FILES[0]
        original = dependency.read_bytes()
        dependency.write_bytes(original + b'// Unreviewed drift.\n')
        execute = subprocess.run

        def offline_then_listing(command, **kwargs):
            if command[0] == 'go':
                raise RuntimeError('Go listing reached after offline verification')
            return execute(command, **kwargs, capture_output=True, text=True)

        with patch('subprocess.run', side_effect=offline_then_listing) as commands:
            with self.assertRaises(subprocess.CalledProcessError) as rejected:
                run('catalog')
            self.assertIn('code_versions.' + release.VERSION_FILES[0], rejected.exception.stderr)
            self.assertEqual(1, commands.call_count)
            self.assertEqual(self.pin, (self.resource / 'release-pin.json').read_bytes())

            dependency.write_bytes(original)
            commands.reset_mock()
            with self.assertRaisesRegex(RuntimeError, 'Go listing reached after offline verification'):
                run('catalog')
            self.assertEqual(2, commands.call_count)
            self.assertEqual(self.pin, (self.resource / 'release-pin.json').read_bytes())

    def test_price_only_changes_do_not_invalidate_reviewed_release(self):
        self.prepare_report()
        prices = self.backend / 'resources/model-pricing/model_prices_and_context_window.json'
        prices.write_text('{"model":{"input_cost_per_token":123.456}}\n')
        result = self.run_command('validate', '--check-artifacts')
        self.assertEqual(0, result.returncode, result.stderr)

    def test_stale_and_missing_report_artifacts_are_rejected(self):
        self.prepare_report()
        coverage = self.output / 'coverage.json'
        coverage.write_bytes(b'{}\n')
        result = self.run_command('validate', '--check-artifacts')
        self.assertEqual(1, result.returncode)
        self.assertIn('stale coverage.json', result.stderr)
        coverage.unlink()
        self.assertEqual(1, self.run_command('validate', '--check-artifacts').returncode)

    def test_changed_review_inputs_and_pin_are_rejected(self):
        self.prepare_report()
        review_path = self.scripts / 'reviewed.json'
        review = catalog.read(review_path)
        review['approval_basis'] = 'unreviewed change'
        catalog.write(review_path, review)
        result = self.run_command('validate', '--check-artifacts')
        self.assertEqual(1, result.returncode)
        self.assertIn('review_sha256', result.stderr)
        shutil.copyfile(catalog.REVIEWED, review_path)
        inputs = catalog.read(self.inputs)
        inputs['sources'][0]['content_hash'] = 'a' * 64
        catalog.write(self.inputs, inputs)
        self.assertEqual(0, self.run_command('candidate').returncode)
        result = self.run_command('report')
        self.assertEqual(1, result.returncode)
        self.assertIn('not approved', result.stderr)
        shutil.copyfile(catalog.INPUTS, self.inputs)
        self.prepare_report()
        manifest = copy.deepcopy(self.manifest)
        manifest['rules_version'] = 'unreviewed'
        catalog.write(self.resource / 'release-manifest.json', manifest)
        result = self.run_command('validate', '--check-artifacts')
        self.assertEqual(1, result.returncode)
        self.assertIn('untrusted release pin', result.stderr)

    def test_explicit_scope_evidence_is_optional_and_checked(self):
        arguments = ('--scope', catalog.ROOT / 'testdata/scope.json',
                     '--registry', catalog.ROOT / 'testdata/registry.json')
        self.assertEqual(0, self.run_command('report', *arguments).returncode)
        result = self.run_command('validate', '--check-artifacts', *arguments)
        self.assertEqual(0, result.returncode, result.stderr)
        (self.output / 'handoff.json').write_bytes(b'{}\n')
        result = self.run_command('validate', '--check-artifacts', *arguments)
        self.assertEqual(1, result.returncode)
        self.assertIn('stale handoff.json', result.stderr)
        result = self.run_command('report', '--scope', catalog.ROOT / 'testdata/scope.json')
        self.assertEqual(1, result.returncode)
        self.assertIn('supplied together', result.stderr)

    def test_unknown_completeness_cannot_pass(self):
        result = self.run_command('validate', '--require-complete')
        self.assertEqual(1, result.returncode)
        self.assertIn('incomplete records', result.stderr)


if __name__ == '__main__':
    unittest.main()
