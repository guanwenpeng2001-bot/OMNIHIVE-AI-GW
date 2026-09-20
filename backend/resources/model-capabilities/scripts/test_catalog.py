import copy
import tempfile
import unittest
from pathlib import Path

import catalog


class CatalogTests(unittest.TestCase):
    def setUp(self):
        self.inputs = catalog.read(catalog.INPUTS)
        self.generated, self.report, self.manifest = catalog.generate(self.inputs)

    def test_release_is_reproducible_and_price_free(self):
        self.assertEqual(catalog.CATALOG.read_bytes(), catalog.encode(self.generated))
        for forbidden in (b"cost_per", b"pricing_basis", b"input_cost", b"output_cost"):
            self.assertNotIn(forbidden, catalog.encode(self.generated))
            self.assertNotIn(forbidden, catalog.encode(self.inputs))
        expected = {(record["profile"], record["model_id"])
                    for source in self.inputs["sources"] for record in source["records"]}
        expected.update((record["profile"], record["model_id"])
                        for record in catalog.read(catalog.REVIEWED)["records"])
        self.assertEqual(expected, {(record["profile"], record["model_id"])
                                    for record in self.generated["records"]})
        self.assertEqual(27, self.report["summary"]["conflicting_fields"])

    def test_conflicts_do_not_override_source_limits_or_tasks(self):
        records = {(r["profile"], r["model_id"]): r for r in self.generated["records"]}
        for row in self.report["models"]:
            for field in row["conflicts"]:
                record = records[row["profile"], row["original_id"]]
                self.assertNotIn(field, record)
                values = {str(e["value"]) for e in row["field_evidence"][field]}
                self.assertGreater(len(values), 1)
                self.assertEqual("incomplete", row["status"])

    def test_false_unknown_and_hosting_boundaries(self):
        records = {(r["profile"], r["model_id"]): r for r in self.generated["records"]}
        reasoner = records["deepseek", "deepseek-reasoner"]
        self.assertIs(False, reasoner["supports_function_calling"])
        self.assertIs(True, reasoner["supports_reasoning"])
        self.assertNotIn("supports_vision", reasoner)
        self.assertEqual("unknown", reasoner["reasoning_control"])
        self.assertNotIn(("cursor", "gpt-4o"), records)
        self.assertNotIn(("google", "gemini-2.5-pro"), records)
        self.assertEqual(18, self.report["summary"]["complete"])

    def test_explicit_empty_fields_remain_present_but_incomplete(self):
        inputs = copy.deepcopy(self.inputs)
        inputs['sources'] = [inputs['sources'][0]]
        inputs['sources'][0]['records'] = [inputs['sources'][0]['records'][0]]
        source_record = inputs['sources'][0]['records'][0]
        source_record.update(input_modalities=[], output_modalities=[], supports_vision=False)
        review = catalog.read(catalog.REVIEWED)
        review['records'] = []
        generated, report, _ = catalog.generate(inputs, review)
        record = generated['records'][0]
        self.assertEqual([], record['input_modalities'])
        self.assertEqual([], record['output_modalities'])
        self.assertIs(False, record['supports_vision'])
        self.assertEqual('incomplete', report['models'][0]['status'])

    def test_reject_invalid_catalog_instead_of_publishing(self):
        mutations = [
            lambda c: c["records"].append(copy.deepcopy(c["records"][0])),
            lambda c: c["records"][0].update(max_input_tokens=0),
            lambda c: c["records"][0].update(supports_vision="false"),
            lambda c: c["records"][0].update(max_output_tokens=True),
            lambda c: c["records"][0].update(input_cost_per_token=0),
            lambda c: c["records"][0].update(reasoning_control="effort"),
            lambda c: c["records"][0].update(source_content_hash="z" * 64),
            lambda c: c["records"][0].update(field_sources={}),
        ]
        for mutate in mutations:
            candidate = copy.deepcopy(self.generated)
            mutate(candidate)
            with self.subTest(mutation=mutate), self.assertRaises(ValueError):
                catalog.validate(candidate)

    def test_duplicate_source_identity_and_json_keys_rejected(self):
        self.inputs["sources"][0]["records"].append(self.inputs["sources"][0]["records"][0])
        with self.assertRaises(ValueError):
            catalog.generate(self.inputs)
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "bad.json"
            path.write_text('{"schema_version": 1, "schema_version": 2}')
            with self.assertRaises(ValueError):
                catalog.read(path)


if __name__ == "__main__":
    unittest.main()
