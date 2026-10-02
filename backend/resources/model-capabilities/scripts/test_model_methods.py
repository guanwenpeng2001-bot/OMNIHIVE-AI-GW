import copy
import unittest

import catalog
import presets


class ModelMethodsReviewTests(unittest.TestCase):
    def setUp(self):
        self.inputs = catalog.read(presets.INPUTS)
        self.profiles = [profile for profile in self.inputs["profiles"]
                         if (profile["platform"], profile["provider_profile"]) in presets.METHOD_REVIEW_SCOPES]

    def test_reviewed_october_identity_sets_are_44_and_86(self):
        frozen = {
            "opencode_go": (44, "ffd35167a7782f2cd538571d925acdd9ddb620688abf7c8da37b28822f998005"),
            "commandcode": (86, "01ae0bb504f63de01d6714ac98042103254cb4d0314bdd3f528fbd41ad7e21dc"),
        }
        for profile in self.profiles:
            identifiers = sorted(model["id"] for model in profile["models"])
            self.assertEqual(frozen[profile["platform"]],
                             (len(identifiers), catalog.digest(("\n".join(identifiers) + "\n").encode())))
            self.assertEqual(len(identifiers), len(set(identifiers)))

    def test_every_target_has_an_explicit_review_disposition(self):
        totals = {"confirmed": 0, "conflicting": 0, "unavailable": 0}
        for profile in self.profiles:
            records = profile["coverage_scope"]["methods_review"]["records"]
            self.assertEqual({model["id"] for model in profile["models"]},
                             {record["exact_id"] for record in records})
            for record in records:
                totals[record["decision"]] += 1
                self.assertTrue(record["remaining_gap"])
            self.assertEqual("partial", profile["coverage_status"])
            self.assertFalse(profile["coverage_scope"]["official_full_inventory"])
        self.assertEqual({"confirmed": 119, "conflicting": 0, "unavailable": 11}, totals)
        presets.generate(self.inputs)

    def test_methods_require_exact_hosted_endpoint_evidence(self):
        for field, invalid in (("provider_profile", "native"), ("exact_id", "unreviewed-alias"),
                               ("capture_sha256", "f" * 64), ("observed_at", "2026-01-01"),
                               ("evidence_url", "https://opencode.ai/docs/zen/"),
                               ("endpoints", ["https://opencode.ai/zen/v1/chat/completions"])):
            with self.subTest(field=field):
                changed = copy.deepcopy(self.profiles[0])
                record = next(r for r in changed["coverage_scope"]["methods_review"]["records"]
                              if r["decision"] == "confirmed")
                record[field] = invalid
                with self.assertRaises(ValueError):
                    presets.reviewed_methods(changed, self.inputs["sources"])
        changed = copy.deepcopy(self.profiles[0])
        changed["models"][0]["field_sources"]["methods"] = "https://models.dev/api.json"
        with self.assertRaises(ValueError):
            presets.reviewed_methods(changed, self.inputs["sources"])

    def test_supported_endpoints_map_to_existing_method_taxonomy(self):
        self.assertEqual({"/chat/completions": "chat_completions", "/messages": "messages",
                          "/responses": "responses"}, presets.ENDPOINT_METHODS)
        other_methods = {method for profile in self.inputs["profiles"] if profile not in self.profiles
                         for model in profile["models"] for method in model["methods"]}
        self.assertLessEqual(set(presets.ENDPOINT_METHODS.values()), other_methods)
        changed = copy.deepcopy(self.profiles[1])
        changed["coverage_scope"]["methods_review"]["records"][0]["endpoints"] = ["/future-endpoint"]
        with self.assertRaisesRegex(ValueError, "unsupported endpoint"):
            presets.reviewed_methods(changed, self.inputs["sources"])

    def test_unknown_and_conflicting_methods_stay_unasserted(self):
        for decision in ("unavailable", "conflicting"):
            changed = copy.deepcopy(self.profiles[0])
            record = next(r for r in changed["coverage_scope"]["methods_review"]["records"]
                          if r["decision"] == "confirmed")
            record["decision"] = decision
            with self.assertRaisesRegex(ValueError, "unasserted"):
                presets.reviewed_methods(changed, self.inputs["sources"])
            next(model for model in changed["models"] if model["id"] == record["exact_id"])["methods"] = []
            presets.reviewed_methods(changed, self.inputs["sources"])

    def test_methods_source_does_not_refresh_unrelated_observation_dates(self):
        generated = presets.generate(self.inputs)
        for profile in self.profiles:
            result = next(item for item in generated if item["platform"] == profile["platform"])
            before = {model["id"]: model for model in profile["models"]}
            for model in result["models"]:
                source = self.inputs["sources"][before[model["id"]]["source"]]
                self.assertEqual(source["observed_at"], model["verified_at"])
                self.assertEqual(source["content_sha256"], model["source_content_hash"])
                if not before[model["id"]]["source"].startswith("official-oct03-"):
                    self.assertTrue(model["verified_at"].startswith("2026-09-20"))
                self.assertEqual(source["url"], model["field_sources"]["lifecycle"])

    def test_provider_projection_contains_no_prices_or_credentials(self):
        allowed = {"platform", "provider_profile", "exact_id", "evidence_url", "capture_sha256",
                   "observed_at", "method_or_field", "decision", "remaining_gap", "endpoints", "source"}
        for profile in self.profiles:
            for record in profile["coverage_scope"]["methods_review"]["records"]:
                self.assertEqual(allowed, set(record))

    def test_method_changes_update_revision_without_mutating_inputs(self):
        before = copy.deepcopy(self.inputs)
        generated = presets.generate(self.inputs)
        self.assertEqual(before, self.inputs)
        profile = self.profiles[0]
        record = profile["coverage_scope"]["methods_review"]["records"][0]
        record.update(decision="unavailable", remaining_gap="Withdrawn pending review")
        next(model for model in profile["models"] if model["id"] == record["exact_id"])["methods"] = []
        changed = presets.generate(self.inputs)
        revisions = {(item["platform"], item["provider_profile"]): item["revision"] for item in generated}
        for item in changed:
            identity = (item["platform"], item["provider_profile"])
            if item["platform"] == profile["platform"]:
                self.assertNotEqual(revisions[identity], item["revision"])
            else:
                self.assertEqual(revisions[identity], item["revision"])


if __name__ == "__main__":
    unittest.main()
