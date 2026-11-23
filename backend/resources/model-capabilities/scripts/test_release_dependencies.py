import shutil
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import catalog
import release


ORIGINAL_VERSION_FILES = (
    "internal/service/model_registry.go",
    "internal/service/model_registry_reload.go",
    "internal/service/model_registry_metadata.go",
    "internal/service/visible_model_resolver.go",
    "internal/service/upstream_models.go",
    "internal/service/platform_model_catalog.go",
    "internal/service/model_capability_catalog.go",
    "internal/service/model_capability_profile.go",
    "internal/service/model_capability_readonly.go",
    "internal/service/model_capability_matcher.go",
    "internal/service/model_capability_heuristics.go",
    "internal/pkg/capabilitysources/sources.go",
    "internal/pkg/capabilitysources/collect.go",
    "resources/model-capabilities/release.go",
)
PROFILE_VERSION_FILES = (
    "internal/service/native_provider_profiles.go",
    "internal/service/compatible_provider_runtime.go",
    "internal/domain/compatible_provider.go",
    "internal/domain/compatible_plan.go",
    "internal/service/compatible_billing_plan.go",
)
PROJECTION_VERSION_FILES = (
    "internal/modules/catalog/builtin.go",
    "internal/modules/catalog/provenance.go",
)
PROVIDERS = "internal/domain/data/compatible_providers"


class ReleaseDependencyTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.backend = Path(temporary.name)
        for name in (*ORIGINAL_VERSION_FILES, *PROFILE_VERSION_FILES, *PROJECTION_VERSION_FILES,
                     "internal/service/platform_model_presets.json"):
            target = self.backend / name
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(release.BACKEND / name, target)
        self.providers = self.backend / PROVIDERS
        self.providers.mkdir(parents=True)
        (self.providers / "index.json").write_bytes(b"[]\n")
        (self.providers / "example.json").write_bytes(b"[]\n")
        for name, value in (("BACKEND", self.backend),
                            ("MANIFEST", self.backend / "manifest.json"),
                            ("PIN", self.backend / "pin.json")):
            patched = patch.object(release, name, value)
            patched.start()
            self.addCleanup(patched.stop)
        self.value = catalog.read(catalog.CATALOG)
        manifest = release.manifest_for(self.value, catalog.read(catalog.REVIEWED))
        catalog.write(release.MANIFEST, manifest)
        catalog.write(release.PIN, {
            "approved_generation": manifest["approved_generation"],
            "manifest_sha256": catalog.digest(catalog.encode(manifest)),
        })
        self.trusted_bytes = (release.MANIFEST.read_bytes(), release.PIN.read_bytes())
        release.validate_release(self.value)

    def assert_rejected_without_repin(self, dependency, missing=False):
        message = f"missing release dependency: {dependency}" if missing else f"code_versions.{dependency}"
        with self.assertRaises(ValueError) as error:
            release.validate_release(self.value)
        self.assertIn(message, str(error.exception))
        self.assertEqual(self.trusted_bytes,
                         (release.MANIFEST.read_bytes(), release.PIN.read_bytes()))

    def test_original_dependencies_retained_with_profile_dependencies(self):
        self.assertEqual(set(ORIGINAL_VERSION_FILES + PROFILE_VERSION_FILES + PROJECTION_VERSION_FILES),
                         set(release.VERSION_FILES))
        expected = set(ORIGINAL_VERSION_FILES + PROFILE_VERSION_FILES + PROJECTION_VERSION_FILES)
        expected.update(f"{PROVIDERS}/{name}" for name in ("index.json", "example.json"))
        self.assertEqual(expected, set(release.code_versions()))

    def test_each_profile_helper_edit_invalidates_unchanged_pin(self):
        for name in PROFILE_VERSION_FILES:
            with self.subTest(dependency=name):
                target = self.backend / name
                original = target.read_bytes()
                target.write_bytes(original + b"// changed profile selection\n")
                try:
                    self.assert_rejected_without_repin(name)
                finally:
                    target.write_bytes(original)

    def test_each_projection_helper_edit_invalidates_unchanged_pin(self):
        for name in PROJECTION_VERSION_FILES:
            with self.subTest(dependency=name):
                target = self.backend / name
                original = target.read_bytes()
                target.write_bytes(original + b"// changed compatibility projection\n")
                try:
                    self.assert_rejected_without_repin(name)
                finally:
                    target.write_bytes(original)

    def test_json_change_invalidates_unchanged_pin(self):
        (self.providers / "example.json").write_bytes(b"[{}]\n")
        self.assert_rejected_without_repin(f"{PROVIDERS}/example.json")

    def test_json_addition_invalidates_unchanged_pin(self):
        (self.providers / "added.json").write_bytes(b"[]\n")
        self.assert_rejected_without_repin(f"{PROVIDERS}/added.json")

    def test_json_removal_invalidates_unchanged_pin_and_reports_missing(self):
        (self.providers / "example.json").unlink()
        self.assert_rejected_without_repin(f"{PROVIDERS}/example.json", missing=True)

    def test_index_is_bound(self):
        (self.providers / "index.json").write_bytes(b"[{}]\n")
        self.assert_rejected_without_repin(f"{PROVIDERS}/index.json")

    def test_only_exact_json_suffix_at_embed_depth_is_bound(self):
        baseline = release.code_versions()
        for name in ("ignored.JSON", "ignored.Json", "ignored.json.bak", "notes.txt"):
            (self.providers / name).write_bytes(b"[]\n")
        nested = self.providers / "nested"
        nested.mkdir()
        (nested / "ignored.json").write_bytes(b"[]\n")
        self.assertEqual(baseline, release.code_versions())
        release.validate_release(self.value)

    def test_crlf_logical_normalization_for_go_and_json(self):
        baseline = release.code_versions()
        for name in (*ORIGINAL_VERSION_FILES, *PROFILE_VERSION_FILES, *PROJECTION_VERSION_FILES,
                     f"{PROVIDERS}/example.json", f"{PROVIDERS}/index.json"):
            target = self.backend / name
            target.write_bytes(target.read_bytes().replace(b"\r\n", b"\n").replace(b"\n", b"\r\n"))
        self.assertEqual(baseline, release.code_versions())
        release.validate_release(self.value)

    def test_missing_original_and_new_helpers_are_reported_together(self):
        names = (ORIGINAL_VERSION_FILES[0], PROFILE_VERSION_FILES[0], PROJECTION_VERSION_FILES[0])
        for name in names:
            (self.backend / name).unlink()
        for name in names:
            self.assert_rejected_without_repin(name, missing=True)
        with self.assertRaisesRegex(ValueError, "missing release dependency"):
            release.code_versions()


if __name__ == "__main__":
    unittest.main()
