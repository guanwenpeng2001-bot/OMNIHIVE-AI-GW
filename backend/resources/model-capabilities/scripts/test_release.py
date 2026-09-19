import copy
import subprocess
import sys
import tempfile
import unittest
from argparse import Namespace
from pathlib import Path

import catalog
import evidence
import release


class ReleaseTests(unittest.TestCase):
    def test_capture_candidate_preserves_structured_control_and_task_fields(self):
        raw = {'model': {'litellm_provider':'native', 'mode':'chat',
                        'supports_reasoning':True, 'control':{'kind':'budget','budget_min':0,'budget_max':1024},
                        'supported_methods':['chat_completions'], 'embedding_dimensions':128}}
        with tempfile.TemporaryDirectory() as tmp:
            source, target = Path(tmp)/'raw.json', Path(tmp)/'inputs.json'
            catalog.write(source, raw)
            catalog.capture(Namespace(source=[('native-fixture',source)], tier='local-data',
                            verified_at='2026-09-17', output=target))
            review = catalog.read(catalog.REVIEWED)
            review['records'] = []
            candidate, _, _ = catalog.generate(catalog.read(target), review)
            record = candidate['records'][0]
            self.assertEqual(0, record['reasoning_control']['budget_min'])
            self.assertEqual(['chat_completions'], record['supported_methods'])
            self.assertEqual(128, record['embedding_dimensions'])
            self.assertNotIn('control', record)

    def test_observation_does_not_approve_or_change_generation(self):
        inputs = catalog.read(catalog.INPUTS)
        inputs['sources'][0]['content_hash'] = 'a' * 64
        candidate, _, _ = catalog.generate(inputs)
        manifest = catalog.read(release.MANIFEST)
        pin = catalog.read(release.PIN)
        self.assertEqual(candidate['generated_at'], catalog.read(catalog.CATALOG)['generated_at'])
        with self.assertRaises(ValueError):
            release.verify(candidate, manifest, pin)
        changed = copy.deepcopy(manifest)
        changed['catalog_sha256'] = catalog.digest(catalog.encode(candidate))
        candidate_pin = {'approved_generation': changed['approved_generation'],
                         'manifest_sha256': catalog.digest(catalog.encode(changed))}
        with self.assertRaisesRegex(ValueError, 'same generation'):
            release.verify(candidate, changed, candidate_pin, manifest)

    def test_pin_rules_and_offline_working_directory(self):
        value = catalog.read(catalog.CATALOG)
        manifest = catalog.read(release.MANIFEST)
        changed = copy.deepcopy(manifest)
        changed['rules_version'] = 'unreviewed'
        with self.assertRaisesRegex(ValueError, 'pin'):
            release.verify(value, changed, catalog.read(release.PIN))
        with tempfile.TemporaryDirectory() as tmp:
            process = subprocess.run([sys.executable, str(catalog.ROOT/'catalog.py'), 'validate'],
                                     cwd=tmp, capture_output=True, text=True)
            self.assertEqual(0, process.returncode, process.stderr)

    def test_older_generation_is_rejected(self):
        value = catalog.read(catalog.CATALOG)
        manifest = catalog.read(release.MANIFEST)
        previous = copy.deepcopy(manifest)
        previous['approved_generation'] += 1
        with self.assertRaisesRegex(ValueError, 'rollback'):
            release.verify(value, manifest, catalog.read(release.PIN), previous)

    def test_control_forms_are_lossless_and_conflicts_rejected(self):
        for kind in ('none', 'unknown', 'fixed', 'adaptive', 'toggle', 'budget', 'effort'):
            control = {'kind': kind}
            if kind == 'budget': control.update(budget_min=0, budget_max=1024)
            if kind == 'effort': control.update(effort_values=['low', 'high'], default='low')
            if kind == 'toggle': control.update(toggle_values=[False, True])
            public = {'reasoning_control': control}
            self.assertEqual(public, catalog.public_record(catalog.internal_record(public)))
        for control in ({'kind':'effort'}, {'kind':'budget','budget_min':True,'budget_max':10},
                        {'kind':'toggle','toggle_values':['false']}, {'kind':'magic'},
                        {'kind':'unknown','invented':1}):
            with self.assertRaises(ValueError):
                catalog.public_record({'reasoning_control':control})
        with self.assertRaises(ValueError):
            catalog.public_record({'reasoning_control':'fixed','control':{'kind':'unknown'}})

    def test_registry_capture_retains_rejections_without_prices(self):
        raw = {'data':[{'id':'author/model','context_length':0,'pricing':{'prompt':'10'},
                        'architecture':{'output_modalities':['image']},
                        'supported_parameters':['reasoning_effort']}]}
        with tempfile.TemporaryDirectory() as tmp:
            source, target = Path(tmp)/'source.json', Path(tmp)/'capture.json'
            catalog.write(source, raw)
            args = Namespace(source=[('https://openrouter.ai/api/v1/models?output_modalities=all',source)],
                             verified_at='2026-09-17',output=target)
            evidence.capture_registry(args)
            result = catalog.read(target)
            self.assertFalse(result['approved'])
            record = result['records'][0]
            self.assertEqual('image', record['task_group'])
            self.assertIn('context_window', record['rejected_fields'])
            self.assertNotIn('reasoning_control', record['field_evidence'])
            self.assertNotIn('pricing', target.read_text())
            first = target.read_bytes()
            evidence.capture_registry(args)
            self.assertEqual(first, target.read_bytes())
            raw['has_more'] = True
            catalog.write(source, raw)
            with self.assertRaisesRegex(ValueError, 'pagination'):
                evidence.capture_registry(args)

    def test_small_historical_scope_keeps_denominators_and_rejects_aliases(self):
        report = evidence.generate_handoff(catalog.ROOT/'testdata/scope.json', catalog.ROOT/'testdata/registry.json')
        self.assertEqual(1, report['historical_count'])
        self.assertEqual(1, len({(r['profile'], r['model_id']) for r in report['models'] if r['set']=='H485'}))
        self.assertEqual('not_met', report['g_complete'])
        for row in report['models']:
            self.assertEqual('P2-01', row['owner'])
            self.assertFalse(row['no_public_spec'])
            for candidate in row['candidate_evidence']:
                self.assertEqual('rejected', candidate['decision'])
                self.assertTrue(candidate['content_hash'])
                self.assertTrue(candidate['alias_reason'])
        self.assertEqual(catalog.encode(report), catalog.encode(evidence.generate_handoff(catalog.ROOT/'testdata/scope.json', catalog.ROOT/'testdata/registry.json')))

    def test_current_freeze_preserves_account_scope_and_excludes_credentials(self):
        account = {'id': 1, 'platform': 'openai', 'type':'apikey',
                   'credentials': {'api_key':'do-not-retain', 'model_mapping':{'alias':'mapped'}},
                   'extra': {'upstream_model_catalog':{'discovered_models':['native'],
                             'provider_profile':'openai:apikey', 'eligible_models':['native']}}}
        other = copy.deepcopy(account)
        other['id'] = 2
        frozen, rows = evidence.freeze_accounts([account, other], '2026-09-17T00:00:00Z')
        self.assertEqual(4, frozen['identity_count'])
        self.assertEqual(2, frozen['account_count'])
        self.assertEqual({'native','mapped'}, {r['model_id'] for r in rows})
        self.assertNotIn(b'do-not-retain', catalog.encode(frozen)+catalog.encode(rows))
        self.assertNotIn(b'api_key', catalog.encode(frozen)+catalog.encode(rows))
        self.assertEqual([2, 2], [a['U'] for a in frozen['accounts']])
        self.assertTrue(all(a['mapping_sha256'] for a in frozen['accounts']))


if __name__ == '__main__':
    unittest.main()
