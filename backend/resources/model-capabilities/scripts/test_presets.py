import copy
import unittest

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
                          "volcengine", "qianfan", "hunyuan", "mistral", "bedrock"},
                         {profile["platform"] for profile in self.generated})
        for profile in self.generated:
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
        self.assertEqual("deprecated", models["hunyuan-turbos-latest"]["lifecycle"])
        self.assertEqual("2026-09-30", models["hunyuan-turbos-latest"]["retirement_date"])
        self.assertEqual("unknown", models["hunyuan-turbos-latest"]["task_type"])

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

    def test_generation_does_not_mutate_inputs_and_revision_tracks_facts(self):
        self.assertEqual(self.inputs, catalog.read(presets.INPUTS))
        altered = copy.deepcopy(self.inputs)
        altered["profiles"][0]["models"][0]["lifecycle"] = "retired"
        changed = presets.generate(altered)
        self.assertNotEqual(changed[0]["revision"], self.generated[0]["revision"])
        self.assertEqual(changed[1:], self.generated[1:])


if __name__ == "__main__":
    unittest.main()
