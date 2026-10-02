"""Real cross-language fixtures ensure collection and runtime share the parser."""
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

import catalog
import collect
import release


class CollectionTests(unittest.TestCase):
    def test_three_sources_feed_candidates_without_approval(self):
        pinned = release.PIN.read_bytes()
        for source in collect.SOURCES:
            with self.subTest(source=source):
                inputs = collect.collect(source, collect.BACKEND / 'internal/pkg/capabilitysources/testdata' / (source + '.json'), '2026-09-19')
                record = inputs['sources'][0]['records'][0]
                self.assertEqual([], record['input_modalities'])
                self.assertNotIn('pricing', record)
                review = catalog.read(catalog.REVIEWED)
                review['records'] = []
                review['overlays'] = []
                value, _, _ = catalog.generate(inputs, review)
                self.assertTrue(value['records'])
                with self.assertRaises(ValueError):
                    release.verify(value, catalog.read(release.MANIFEST), catalog.read(release.PIN))
        self.assertEqual(pinned, release.PIN.read_bytes())

    def test_unknown_source_rejected_before_execution(self):
        with self.assertRaisesRegex(ValueError, 'unsupported'):
            collect.collect('unknown')

    def test_failed_capture_preserves_previous_output(self):
        (collect.BACKEND.parent / '.pi').mkdir(exist_ok=True)
        with tempfile.TemporaryDirectory(dir=collect.BACKEND.parent / '.pi') as temporary:
            output = Path(temporary) / 'inputs.json'
            output.write_bytes(b'previous candidate')
            bad = Path(temporary) / 'bad.json'
            bad.write_text('{"data":[],"has_more":true}')
            result = subprocess.run([sys.executable, str(catalog.ROOT / 'collect.py'),
                                     '--source', 'openrouter', '--input', str(bad),
                                     '--verified-at', '2026-09-19', '--output', str(output)],
                                    capture_output=True, text=True)
            self.assertNotEqual(0, result.returncode)
            self.assertEqual(b'previous candidate', output.read_bytes())


if __name__ == '__main__':
    unittest.main()
