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


def generate(inputs):
    catalog.require(inputs.get("schema_version") == 1, "unsupported preset source schema")
    sources = inputs["sources"]
    for source in sources.values():
        catalog.require(source["url"].startswith("https://"), "source must identify a public document")
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
        if profile["coverage_status"] == "complete":
            catalog.require(not profile.get("gaps") and profile["coverage_scope"].get("official_full_inventory"),
                            "complete requires an official scoped inventory and no unresolved gaps")
        elif profile["coverage_status"] == "partial":
            catalog.require(profile["models"] and profile.get("gaps"), "partial requires candidates and gaps")
        seen = set()
        for model in profile["models"]:
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
            fields = model.pop("field_sources", {})
            for field in model:
                fields.setdefault(field, source["url"])
            model["field_sources"] = fields
            model["source_content_hash"] = source["content_sha256"]
            model["verified_at"] = source["observed_at"]
        profile["models"].sort(key=lambda model: model["id"])
        profile["revision"] = catalog.digest(catalog.encode(profile))[:16]
        result.append(profile)
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
