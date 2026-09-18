"""Offline release binding. Candidate generation never edits trust pins."""
from datetime import datetime, timezone
import catalog

BACKEND = catalog.ROOT.parents[2]
MANIFEST = catalog.ROOT.parent / "release-manifest.json"
PIN = catalog.ROOT.parent / "release-pin.json"
VERSION_FILES = (
    "internal/service/model_capability_catalog.go",
    "internal/service/model_capability_profile.go",
    "internal/service/model_capability_matcher.go",
    "internal/service/model_capability_heuristics.go",
    "internal/pkg/capabilitysources/sources.go",
    "internal/pkg/capabilitysources/collect.go",
    "resources/model-capabilities/release.go",
)


def code_versions():
    # Reviewed source versions bind logical text, independent of checkout EOL.
    versions = {}
    for name in VERSION_FILES:
        path = BACKEND / name
        catalog.require(path.is_file(), f"missing release dependency: {name}")
        versions[name] = catalog.digest(path.read_bytes().replace(b"\r\n", b"\n"))
    return versions


def manifest_for(value, review):
    return {"schema_version": 1,
            "approved_generation": int(datetime.strptime(value["generated_at"], "%Y-%m-%dT%H:%M:%SZ").replace(tzinfo=timezone.utc).timestamp()),
            "catalog_sha256": catalog.digest(catalog.encode(value)),
            "applicability": "reviewed-profile-model",
            "review_sha256": catalog.digest(catalog.encode(review)),
            "inputs_sha256": review["approved_inputs_sha256"],
            "alias_version": review["alias_version"],
            "aliases_sha256": catalog.digest(catalog.encode([
                {"profile": r["profile"], "model_id": r["model_id"], "aliases": r["aliases"]}
                for r in value["records"] if r.get("aliases")])),
            "adapter_version": review["adapter_version"], "rules_version": review["rules_version"],
            "code_versions": code_versions(), "record_count": len(value["records"])}


def verify(value, manifest, pin, previous=None):
    catalog.validate(value)
    catalog.require(manifest.get("schema_version") == 1, "unsupported release schema")
    catalog.require(type(manifest.get("approved_generation")) is int and manifest["approved_generation"] > 0, "invalid approved generation")
    generation = int(datetime.strptime(value["generated_at"], "%Y-%m-%dT%H:%M:%SZ").replace(tzinfo=timezone.utc).timestamp())
    catalog.require(generation == manifest["approved_generation"], "catalog generation mismatch")
    catalog.require(manifest["catalog_sha256"] == catalog.digest(catalog.encode(value)), "catalog hash mismatch")
    catalog.require(manifest.get("record_count") == len(value["records"]), "record count mismatch")
    catalog.require(pin["approved_generation"] == generation and pin["manifest_sha256"] == catalog.digest(catalog.encode(manifest)), "untrusted release pin")
    if previous:
        catalog.require(generation >= previous["approved_generation"], "release rollback")
        if generation == previous["approved_generation"]:
            catalog.require(manifest == previous, "same generation different hash")


def validate_release(value, inputs_path=catalog.INPUTS):
    review = catalog.read(catalog.REVIEWED)
    catalog.require(catalog.digest(inputs_path.read_bytes()) == review['approved_inputs_sha256'], "unapproved release inputs")
    actual = catalog.read(MANIFEST)
    verify(value, actual, catalog.read(PIN))
    for path in (MANIFEST, PIN):
        catalog.require(path.read_bytes() == catalog.encode(catalog.read(path)), "release must use UTF-8/LF")
    # Report every stale or missing dependency before refusing the release.
    # The existing independently reviewed pin is never regenerated here.
    differences = []
    expected_paths = set(VERSION_FILES) | set(actual.get("code_versions", {}))
    for name in sorted(expected_paths):
        path = BACKEND / name
        if not path.is_file():
            differences.append(f"missing release dependency: {name}")
        else:
            observed = catalog.digest(path.read_bytes().replace(b"\r\n", b"\n"))
            expected = actual.get("code_versions", {}).get(name)
            if observed != expected:
                differences.append(f"code_versions.{name}: pinned={expected}, current={observed}")
    catalog.require(not differences, "release dependency differences; review required:\n" + "\n".join(differences))
    expected = manifest_for(value, review)
    differences = [f"{name}: pinned={actual.get(name)!r}, current={expected.get(name)!r}"
                   for name in sorted(set(actual) | set(expected)) if actual.get(name) != expected.get(name)]
    catalog.require(not differences, "release manifest differences; review required:\n" + "\n".join(differences))


if __name__ == "__main__":
    validate_release(catalog.read(catalog.CATALOG))
    print("reviewed release pin verified (offline)")
