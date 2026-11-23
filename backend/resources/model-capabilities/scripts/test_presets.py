import copy
import unittest
from unittest import mock

import catalog
import presets


class PresetEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.inputs = catalog.read(presets.INPUTS)
        self.generated = presets.generate(self.inputs)

    def test_all_product_profiles_are_reproducible_and_have_scoped_candidates(self):
        self.assertEqual(catalog.read(presets.PRESETS), self.generated)
        self.assertEqual({"anthropic", "openai", "gemini", "grok", "kimi", "zhipu", "deepseek",
                          "minimax", "opencode_go", "qianwen", "cursor", "antigravity", "commandcode",
                          "volcengine", "qianfan", "hunyuan", "mistral", "bedrock", "openrouter"},
                         {profile["platform"] for profile in self.generated})
        for profile in self.generated:
            if profile["platform"] == "openrouter":
                self.assertEqual("unconfigured", profile["coverage_status"])
                self.assertEqual([], profile["models"])
                continue
            self.assertTrue(profile["models"], profile["platform"])
            self.assertTrue(profile["source_refs"])
            self.assertEqual("partial", profile["coverage_status"])
            self.assertTrue(profile["gaps"])
            self.assertTrue(profile["coverage_scope"]["as_of"])

    def test_embedding_is_a_vector_and_never_chat_admission(self):
        models = [model for profile in self.generated for model in profile["models"]
                  if model["task_type"] == "embedding"]
        self.assertTrue(models)
        for model in models:
            self.assertEqual(["embedding"], model["output_modalities"])
            self.assertNotIn("chat_completions", model["methods"])
        qianfan = next(profile for profile in self.generated if profile["platform"] == "qianfan")
        embedding = next(model for model in qianfan["models"] if model["id"] == "embedding-v1")
        self.assertEqual(384, embedding["embedding_dimensions"])
        self.assertEqual(384, embedding["max_input_tokens"])
        self.assertEqual(16, embedding["task_limits"]["max_batch_size"])
        openai = {model["id"]: model for model in next(
            profile for profile in self.generated if profile["platform"] == "openai")["models"]}
        self.assertEqual(3072, openai["text-embedding-3-large"]["embedding_dimensions"])
        self.assertEqual(1536, openai["text-embedding-3-small"]["embedding_dimensions"])
        self.assertEqual(1536, openai["text-embedding-ada-002"]["embedding_dimensions"])
        self.assertEqual(["embeddings"], openai["text-embedding-3-large"]["methods"])
        self.assertNotIn("max_output_tokens", openai["text-embedding-3-large"])
        volcengine = {model["id"]: model for model in next(
            profile for profile in self.generated if profile["platform"] == "volcengine")["models"]}
        vision = volcengine["doubao-embedding-vision-251215"]
        self.assertEqual(2048, vision["embedding_dimensions"])
        self.assertEqual(["embeddings"], vision["methods"])
        self.assertEqual([2048, 1024], vision["task_limits"]["embedding_dimensions"])
        gemini = next(model for profile in self.generated if profile["platform"] == "gemini"
                      for model in profile["models"] if model["id"] == "gemini-embedding-001")
        self.assertNotIn("embedding_dimensions", gemini)

    def test_retirement_and_hosting_boundaries_are_not_aliases(self):
        hunyuan = next(profile for profile in self.generated if profile["platform"] == "hunyuan")
        models = {model["id"]: model for model in hunyuan["models"]}
        self.assertNotIn("hy3-preview", models)
        self.assertFalse(any("customized" in identifier for identifier in models))
        self.assertNotIn("hunyuan-turbos-latest", models)
        self.assertEqual("preview", models["hy4-preview"]["lifecycle"])
        self.assertEqual("active", models["hunyuan-role-latest"]["lifecycle"])
        self.assertNotIn("retirement_date", models["hunyuan-role-latest"])
        self.assertEqual(["chat_completions"], models["hy-mt2-pro"]["methods"])
        self.assertEqual(["chat_completions", "responses", "messages"], models["hy3"]["methods"])
        self.assertEqual("2026-06-22", hunyuan["coverage_scope"]["legacy_model_shutdown_date"])
        self.assertEqual("2026-09-30", hunyuan["coverage_scope"]["legacy_platform_shutdown_date"])
        self.assertFalse(any(identifier.startswith(("deepseek", "glm", "kimi", "minimax", "qwen", "mimo"))
                             for identifier in models))

    def test_reviewed_legacy_defaults_keep_exact_platform_identity(self):
        profiles = {profile["platform"]: profile for profile in self.generated
                    if "legacy_default_review" in profile["coverage_scope"]}
        included_count = excluded_count = 0
        for platform in ("openai", "gemini", "cursor", "antigravity", "grok"):
            profile = profiles[platform]
            audit = profile["coverage_scope"]["legacy_default_review"]
            models = {model["id"]: model for model in profile["models"]}
            self.assertFalse(set(audit["included_ids"]) & set(audit["excluded_ids"]))
            self.assertTrue(set(audit["included_ids"]) <= models.keys())
            self.assertFalse(set(audit["excluded_ids"]) & models.keys())
            self.assertEqual("partial", profile["coverage_status"])
            included_count += len(audit["included_ids"])
            excluded_count += len(audit["excluded_ids"])
            for identifier in audit["included_ids"]:
                model = models[identifier]
                self.assertNotIn("models.dev", model["field_sources"]["id"])
                if platform == "cursor":
                    self.assertEqual("code_generation", model["task_type"])
                    self.assertEqual("partial", model["input_modalities_status"])
                    self.assertEqual({"kind": "unknown"}, model["reasoning_control"])
                    self.assertEqual([], model["methods"])
                    self.assertNotIn("max_input_tokens", model)
        self.assertEqual((31, 49), (included_count, excluded_count))
        self.assertIn("claude-opus-4-6", profiles["cursor"]["coverage_scope"]["legacy_default_review"]["excluded_ids"])
        self.assertIn("gemini-3.8-flash-high", profiles["antigravity"]["coverage_scope"]["legacy_default_review"]["excluded_ids"])

    def test_reviewed_image_models_keep_image_output_and_future_retirement(self):
        for profile in self.generated:
            for model in profile["models"]:
                if model["id"] in {"gpt-image-2.5-flare", "gpt-image-2.5-sunburst",
                                    "grok-imagine-image-2.0", "grok-imagine-image-quality"}:
                    self.assertEqual("image_generation", model["task_type"])
                    self.assertEqual(["text", "image"], model["input_modalities"])
                    self.assertEqual(["image"], model["output_modalities"])
                    self.assertNotIn("chat_completions", model["methods"])
                    if model["id"] == "grok-imagine-image-quality":
                        self.assertEqual("deprecated", model["lifecycle"])
                        self.assertEqual("2026-11-02", model["retirement_date"])

    def test_nontext_tasks_and_reasoning_have_specific_evidence(self):
        mistral = next(profile for profile in self.generated if profile["platform"] == "mistral")
        models = {model["id"]: model for model in mistral["models"]}
        self.assertEqual("speech", models["voxtral-mini-tts-2603"]["task_type"])
        self.assertEqual("transcription", models["voxtral-mini-2602"]["task_type"])
        self.assertEqual("realtime", models["voxtral-mini-transcribe-realtime-2602"]["task_type"])
        openai = {model["id"]: model for model in next(
            profile for profile in self.generated if profile["platform"] == "openai")["models"]}
        self.assertEqual("image_generation", openai["gpt-image-1"]["task_type"])
        self.assertEqual("realtime", openai["gpt-realtime-2.1"]["task_type"])
        gemini = {model["id"]: model for model in next(
            profile for profile in self.generated if profile["platform"] == "gemini")["models"]}
        self.assertEqual("speech", gemini["gemini-2.5-flash-preview-tts"]["task_type"])
        self.assertEqual("image_generation", gemini["gemini-2.5-flash-image"]["task_type"])
        self.assertEqual("video_generation", gemini["gemini-omni-flash-preview"]["task_type"])
        self.assertEqual("audio_generation", gemini["lyria-3-pro-preview"]["task_type"])
        self.assertEqual("realtime", gemini["gemini-3.1-flash-live-preview"]["task_type"])
        self.assertEqual("unknown", gemini["deep-research-preview-04-2026"]["task_type"])
        grok = {model["id"]: model for model in next(
            profile for profile in self.generated if profile["platform"] == "grok")["models"]}
        self.assertEqual("image_generation", grok["grok-imagine-image"]["task_type"])
        self.assertEqual("video_generation", grok["grok-imagine-video-1.5"]["task_type"])
        records = {(record["profile"], record["model_id"]): record
                   for record in catalog.read(catalog.CATALOG)["records"]}
        flash = records["deepseek", "deepseek-flash"]
        self.assertEqual(["low", "high", "max"], flash["reasoning_control"]["effort_values"])
        self.assertIn("api-docs.deepseek.com", flash["field_sources"]["reasoning_control"]["source"])
        self.assertEqual("unknown", records["deepseek", "deepseek-reasoner"]["reasoning_control"])
        self.assertEqual("2026-09-17", records["deepseek", "deepseek-reasoner"]["verified_at"])
        self.assertEqual("2026-09-20", flash["verified_at"])

    def test_missing_or_fabricated_evidence_fails_validation(self):
        for mutate in (
            lambda value: value["profiles"][0].update(coverage_status="complete"),
            lambda value: value["profiles"][0]["models"][0].update(source="missing"),
            lambda value: value["profiles"][0]["models"][0].update(task_type="embedding", output_modalities=["text"]),
            lambda value: value["profiles"][0]["models"][0].update(embedding_dimensions=0),
            lambda value: value["profiles"][0]["models"][0].update(reasoning_control={"kind": "effort"}),
        ):
            value = copy.deepcopy(self.inputs)
            mutate(value)
            with self.assertRaises(ValueError):
                presets.generate(value)

    def test_released_projection_recovery_preserves_only_recorded_evidence(self):
        source_id = "released-v0.3.3-missing-projections"
        source = self.inputs["sources"][source_id]
        self.assertEqual("released_projection_recovery", source["kind"])
        self.assertEqual("94d8a70b39c46895cac3817f3b230fb5eabe2692", source["released_from"]["commit"])
        self.assertEqual("a3f13e774f291715c9c3ebf291f6ae8caf7876c0aa5ba3d0c543b3c81c28a253",
                         source["released_from"]["artifact_sha256"])
        self.assertNotIn("content_sha256", source)
        self.assertNotIn("observed_at", source)
        self.assertEqual(5, len(source["recovered_records"]))
        generated = {(profile["platform"], profile["provider_profile"], model["id"]): model
                     for profile in self.generated for model in profile["models"]}
        recovered = []
        for profile in self.inputs["profiles"]:
            for row in profile["models"]:
                if row["source"] != source_id:
                    continue
                expected = {key: value for key, value in row.items() if key != "source"}
                model = generated[profile["platform"], profile["provider_profile"], row["id"]]
                self.assertEqual(expected, model)
                self.assertEqual("unknown", model["task_type"])
                self.assertEqual([], model["methods"])
                for field in ("display_name", "lifecycle", "task_type", "methods", "reasoning_control"):
                    self.assertNotIn(field, model["field_sources"])
                if model["id"] == "ark-code-latest":
                    self.assertNotIn("source_content_hash", model)
                    self.assertNotIn("verified_at", model)
                    self.assertEqual({"id"}, set(model["field_sources"]))
                else:
                    self.assertEqual("2026-10-11T00:18:20.140336Z", model["verified_at"])
                    self.assertEqual(64, len(model["source_content_hash"]))
                recovered.append(model)
        self.assertEqual(5, len(recovered))

    def test_released_projection_recovery_rejects_changed_or_incomplete_records(self):
        source_id = "released-v0.3.3-missing-projections"
        mutations = {
            "new upstream observation": lambda source, row: source.update(observed_at="2026-11-22T00:00:00Z"),
            "artifact hash as upstream evidence": lambda source, row: source.update(content_sha256="1" * 64),
            "unfixed revision": lambda source, row: source["released_from"].update(commit="main"),
            "missing artifact binding": lambda source, row: source["released_from"].pop("artifact_sha256"),
            "missing recovery basis": lambda source, row: source.pop("recovery_basis"),
            "unlisted identity": lambda source, row: source["recovered_records"].pop(),
            "duplicate identity": lambda source, row: source["recovered_records"].append(
                copy.deepcopy(source["recovered_records"][0])),
            "invented ark verification": lambda source, row: row.update(
                source_content_hash="2" * 64, verified_at="2026-11-22T00:00:00Z"),
            "invented ark method": lambda source, row: row.update(methods=["chat_completions"]),
            "invented field evidence": lambda source, row: row["field_sources"].update(
                lifecycle="https://example.invalid/unsupported"),
            "changed identity": lambda source, row: row.update(id="another-ark-alias"),
        }
        for label, mutate in mutations.items():
            with self.subTest(label=label):
                value = copy.deepcopy(self.inputs)
                row = next(model for profile in value["profiles"] for model in profile["models"]
                           if model["id"] == "ark-code-latest")
                mutate(value["sources"][source_id], row)
                with self.assertRaises(ValueError):
                    presets.generate(value)
        value = copy.deepcopy(self.inputs)
        for profile in value["profiles"]:
            profile["models"] = [row for row in profile["models"] if row["id"] != "ark-code-latest"]
        with self.assertRaisesRegex(ValueError, "every declared identity"):
            presets.generate(value)

    def test_recovery_never_reads_output_and_keeps_normal_capture_validation(self):
        with mock.patch.object(catalog, "read", side_effect=AssertionError("generation read another authority")):
            self.assertEqual(self.generated, presets.generate(self.inputs))
        for field, invalid in (("content_sha256", "missing"), ("observed_at", "not-an-observation-date")):
            with self.subTest(field=field):
                value = copy.deepcopy(self.inputs)
                source = next(source for source in value["sources"].values() if "kind" not in source)
                source[field] = invalid
                with self.assertRaises(ValueError):
                    presets.generate(value)

    def test_generation_does_not_mutate_inputs_and_revision_tracks_facts(self):
        self.assertEqual(self.inputs, catalog.read(presets.INPUTS))
        altered = copy.deepcopy(self.inputs)
        altered["profiles"][0]["models"][0]["lifecycle"] = "retired"
        changed = presets.generate(altered)
        self.assertNotEqual(changed[0]["revision"], self.generated[0]["revision"])
        self.assertEqual(changed[1:], self.generated[1:])


if __name__ == "__main__":
    unittest.main()
