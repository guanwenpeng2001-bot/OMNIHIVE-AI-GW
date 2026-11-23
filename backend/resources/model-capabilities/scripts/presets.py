#!/usr/bin/env python3
"""Reproduce candidate presets from reviewed, price-free source projections.

No network, account discovery, catalog publication, or trust-pin mutation occurs.
"""
import argparse
import copy
from datetime import datetime
from pathlib import Path
import re

import catalog

INPUTS = catalog.ROOT / "preset-sources.json"
PRESETS = catalog.ROOT.parents[2] / "internal/service/platform_model_presets.json"
TASKS = {"unknown", "text_generation", "code_generation", "embedding", "rerank",
         "image_generation", "video_generation", "audio_generation", "speech",
         "transcription", "realtime", "ocr", "three_dimensional_generation"}
MODALITIES = {"text", "image", "audio", "video", "pdf", "embedding", "score", "mesh"}
LIFECYCLES = {"unknown", "active", "preview", "deprecated", "retired"}
ENDPOINT_METHODS = {"/chat/completions": "chat_completions",
                    "/messages": "messages", "/responses": "responses"}
METHOD_REVIEW_SCOPES = {
    ("opencode_go", "opencode-go"): ("https://opencode.ai/docs/go/", "https://opencode.ai/zen/go/v1"),
    ("commandcode", "native"): ("https://api.commandcode.ai/provider/v1/models", ""),
}
RELEASED_PROJECTION_RECOVERY = "released_projection_recovery"


def recovered_projection_digests(source):
    """Validate archived projection identity without claiming a new observation."""
    catalog.require("content_sha256" not in source and "observed_at" not in source,
                    "released projection recovery is not a new upstream capture")
    released = source.get("released_from", {})
    repository, commit, path = (released.get(key, "") for key in ("repository", "commit", "path"))
    catalog.require(isinstance(repository, str) and repository.startswith("https://") and
                    isinstance(commit, str) and re.fullmatch(r"[0-9a-f]{40}", commit) and
                    path == "backend/internal/service/platform_model_presets.json" and
                    source["url"] == f"{repository}/blob/{commit}/{path}",
                    "recovered projections must identify a fixed released preset artifact")
    artifact_hash = released.get("artifact_sha256", "")
    catalog.require(isinstance(artifact_hash, str) and re.fullmatch(r"[0-9a-f]{64}", artifact_hash),
                    "invalid released preset artifact hash")
    catalog.require(isinstance(source.get("recovery_basis"), str) and source["recovery_basis"].strip(),
                    "missing released projection recovery basis")
    records = source.get("recovered_records")
    catalog.require(isinstance(records, list) and records, "missing recovered projection identities")
    result = {}
    for record in records:
        catalog.require(isinstance(record, dict), "invalid recovered projection identity")
        identity = tuple(record.get(key) for key in ("platform", "provider_profile", "model_id"))
        digest = record.get("projection_sha256", "")
        catalog.require(all(isinstance(value, str) and value for value in identity) and
                        identity not in result and isinstance(digest, str) and
                        re.fullmatch(r"[0-9a-f]{64}", digest), "invalid/duplicate recovered projection")
        result[identity] = digest
    return result


def reviewed_methods(profile, sources):
    """Validate the fixed per-ID projection; public evidence never grants access."""
    identity = (profile["platform"], profile["provider_profile"])
    review = profile["coverage_scope"].get("methods_review")
    if identity not in METHOD_REVIEW_SCOPES:
        catalog.require(review is None, "methods review has an unreviewed hosted identity")
        return
    catalog.require(isinstance(review, dict), "missing per-model methods review")
    expected_url, endpoint_base = METHOD_REVIEW_SCOPES[identity]
    models = {model["id"]: model for model in profile["models"]}
    records = review["records"]
    catalog.require(len(records) == len(models) and
                    {record["exact_id"] for record in records} == models.keys(),
                    "methods review must cover each exact identity once")
    catalog.require(review["identity_sha256"] == catalog.digest(
        ("\n".join(sorted(models)) + "\n").encode()), "methods review identity set changed")
    for record in records:
        catalog.require((record["platform"], record["provider_profile"]) == identity,
                        "methods evidence crosses hosted identity")
        catalog.require(record["method_or_field"] == "methods", "unexpected reviewed field")
        source = sources.get(record["source"], {})
        catalog.require(source.get("url") == record["evidence_url"] == expected_url and
                        source.get("content_sha256") == record["capture_sha256"] and
                        source.get("observed_at") == record["observed_at"],
                        "methods evidence is not bound to its captured source")
        catalog.require(record["decision"] in {"confirmed", "conflicting", "unavailable"} and
                        record["remaining_gap"], "missing methods disposition or gap")
        model = models[record["exact_id"]]
        if record["decision"] != "confirmed":
            catalog.require(not model["methods"], "unknown/conflicting methods must stay unasserted")
            continue
        endpoints = record["endpoints"]
        allowed = {endpoint_base + path: method for path, method in ENDPOINT_METHODS.items()}
        catalog.require(isinstance(endpoints, list) and endpoints and
                        all(endpoint in allowed for endpoint in endpoints),
                        "unsupported endpoint or wrong hosted lane")
        catalog.require(model["methods"] == sorted({allowed[endpoint] for endpoint in endpoints}),
                        "methods differ from exact endpoint evidence")
        catalog.require(model.get("field_sources", {}).get("methods") == expected_url,
                        "methods require their field-specific source")


def generate(inputs):
    catalog.require(inputs.get("schema_version") == 1, "unsupported preset source schema")
    sources = inputs["sources"]
    recovered, recovered_used = {}, set()
    for source_id, source in sources.items():
        catalog.require(source["url"].startswith("https://"), "source must identify a public document")
        if source.get("kind") == RELEASED_PROJECTION_RECOVERY:
            recovered[source_id] = recovered_projection_digests(source)
            continue
        catalog.require("kind" not in source, "unsupported preset source kind")
        catalog.require(re.fullmatch(r"[0-9a-f]{64}", source["content_sha256"]), "invalid source hash")
        datetime.fromisoformat(source["observed_at"].replace("Z", "+00:00"))
    result, identities = [], set()
    for projection in inputs["profiles"]:
        profile = copy.deepcopy(projection)
        identity = (profile["platform"], profile["provider_profile"])
        catalog.require(identity not in identities, f"duplicate profile {identity}")
        identities.add(identity)
        catalog.require(profile["coverage_status"] in {"partial", "complete", "unconfigured"}, "invalid coverage")
        catalog.require(profile.get("coverage_scope"), "missing coverage scope")
        reviewed_methods(profile, sources)
        if profile["coverage_status"] == "complete":
            catalog.require(not profile.get("gaps") and profile["coverage_scope"].get("official_full_inventory"),
                            "complete requires an official scoped inventory and no unresolved gaps")
        elif profile["coverage_status"] == "partial":
            catalog.require(profile["models"] and profile.get("gaps"), "partial requires candidates and gaps")
        seen = set()
        for model in profile["models"]:
            catalog.check_identity_facts(model)
            source_id = model.pop("source")
            catalog.require(source_id in sources, "unknown model source")
            source = sources[source_id]
            catalog.require(model["id"] and model["id"] not in seen, f"duplicate/empty model in {identity}")
            seen.add(model["id"])
            catalog.require(model["task_type"] in TASKS, "unknown task taxonomy")
            if "tasks" in model:
                catalog.require(isinstance(model["tasks"], list) and set(model["tasks"]) <= TASKS - {"unknown"},
                                "invalid supported tasks")
            catalog.require(model["lifecycle"] in LIFECYCLES, "unknown lifecycle")
            for field in ("input_modalities", "output_modalities"):
                catalog.require(set(model[field]) <= MODALITIES, f"unknown modality in {model['id']}")
            if model["task_type"] == "embedding":
                catalog.require(model["output_modalities"] == ["embedding"], "embedding output must be a vector")
            if "reasoning_control" in model:
                catalog.validate_control(model)
            if "embedding_dimensions" in model:
                catalog.require(type(model["embedding_dimensions"]) is int and model["embedding_dimensions"] > 0,
                                "invalid vector dimensions")
            for field in ("max_input_tokens", "context_window", "max_output_tokens"):
                if field in model:
                    catalog.require(type(model[field]) is int and model[field] > 0, f"invalid {field}")
            if "task_limits" in model:
                catalog.require(isinstance(model["task_limits"], dict), "invalid task limits")
                for key, value in model["task_limits"].items():
                    catalog.require((type(value) is int and value > 0) or
                                    (isinstance(value, list) and value and all(
                                        (type(item) is int and item > 0) or (isinstance(item, str) and item)
                                        for item in value)), f"invalid task limit {key}")
            if source_id in recovered:
                recovered_identity = (*identity, model["id"])
                catalog.require(recovered[source_id].get(recovered_identity) ==
                                catalog.digest(catalog.encode(model)),
                                "released projection changed or crosses its recorded identity")
                recovered_used.add((source_id, recovered_identity))
                # Preserve the released row exactly. Missing field evidence or
                # upstream hash/time stays missing; the artifact hash is not an
                # upstream observation and cannot fill those gaps.
            else:
                fields = model.pop("field_sources", {})
                for field in model:
                    fields.setdefault(field, source["url"])
                model["field_sources"] = fields
                model["source_content_hash"] = source["content_sha256"]
                model["verified_at"] = source["observed_at"]
        profile["models"].sort(key=lambda model: model["id"])
        profile["revision"] = catalog.digest(catalog.encode(profile))[:16]
        result.append(profile)
    catalog.require(recovered_used == {(source_id, identity) for source_id, records in recovered.items()
                                       for identity in records},
                    "released projection recovery must reproduce every declared identity")
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("candidate", "validate"))
    parser.add_argument("--inputs", type=Path, default=INPUTS)
    parser.add_argument("--output", type=Path, default=PRESETS)
    args = parser.parse_args()
    presets = generate(catalog.read(args.inputs))
    if args.command == "candidate":
        catalog.write(args.output, presets)
    else:
        catalog.require(catalog.read(args.output) == presets, "preset data differs from fixed source projections")
    print(f"{len(presets)} profiles, {sum(len(profile['models']) for profile in presets)} candidates; {args.command} passed")


if __name__ == "__main__":
    main()
