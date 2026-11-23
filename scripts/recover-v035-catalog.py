#!/usr/bin/env python3
"""Recover four previously reviewed v0.3.5 JSON artifacts from fixed Git history.

No network request, new source observation, trust-pin update, Git write, or commit.
Default/--recover writes only the four fixed artifacts after every expected digest
matches. --verify-root only reads an isolated artifact directory and checks its
exact file set, bytes, and absence of symlinks; it does not inspect Git or import
repository modules.
"""
import argparse
import copy
import hashlib
import json
from pathlib import Path
import subprocess
import sys

sys.dont_write_bytecode = True
BASE = "94d8a70b39c46895cac3817f3b230fb5eabe2692"
UPLOADED = "e243ef275d0fec25ae1757a461ad2d9975238444"
STAMP = "2026-11-22T23:50:13Z"
GENERATION = 1795391413
MANIFEST_HASH = "bbc7b752cf47cc89304f0ea35e6444e9daa3c95004701fe91064ce7d06f2539b"
GENERATOR_HASH = "3729fe05c76ae6972367519ace03de01e9247f8524a237db4c929fd32a8e33d4"
EXPECTED = {
    "backend/resources/model-capabilities/catalog.json": "d1d0a7863c3ee941bd0ba6e3d2ec7320bbadc518f0af0c7bba979e89bba843cd",
    "backend/resources/model-capabilities/scripts/reviewed.json": "66359b1c389f70ae296ec8915946bb0ff0c384731e1e747a5b1566f1d75e60fd",
    "backend/resources/model-capabilities/scripts/preset-sources.json": "b27dc917f08806d5e58997931a3c6474ca5982910ecf8a76596c2698faf7efe4",
    "backend/internal/service/platform_model_presets.json": "da0303082c648147331fc2587bcc591cb2e009cf278ea7c887d2b308d5b06136"
}
BASE_HASHES = {
    "backend/resources/model-capabilities/catalog.json": "f03d4dfa1570cea353a6fb9d4d72e1976c17a9cf771b7f9849b625ae5c4f1bf8",
    "backend/resources/model-capabilities/scripts/reviewed.json": "a8efb4975cade09ebcaf8b68467be20d19a58f082b1b438a0da1f78e8c6e7ebe",
    "backend/resources/model-capabilities/scripts/preset-sources.json": "505468addca4536d664f5eec69ea93b190fa2b78c649ea45406a5e3a78818705",
    "backend/internal/service/platform_model_presets.json": "a3f13e774f291715c9c3ebf291f6ae8caf7876c0aa5ba3d0c543b3c81c28a253"
}
INITIAL_NOTE = " AI code-review version binding at 2026-11-22T23:40:56Z: the parent delivery agent independently reviewed the v0.3.5 registry runtime changes before authorizing this preparation. Scope is truthful preset/builtin field provenance and publication of the derived compatibility index only after successful registry validation and storage. The direct builtin/provenance projection implementations are explicitly bound as release dependencies. All 912 previously reviewed model records, per-field source evidence and observation dates are unchanged; inputs, platform presets, pi reference resources and price resources are unchanged. This is not a new human review, a provider-availability verification, or a digital signature. The compiled hash pin is checked and written independently from this preparation."
RECOVERY_NOTE = " AI source-projection recovery review at 2026-11-22T23:50:13Z: the parent delivery agent approved restoration of five candidate rows already published in v0.3.3 commit 94d8a70b39c46895cac3817f3b230fb5eabe2692, artifact SHA256=a3f13e774f291715c9c3ebf291f6ae8caf7876c0aa5ba3d0c543b3c81c28a253. Two Qwen3.8 Omni rows in each of alibaba and alibaba-cn and the native Volcengine ark-code-latest row are restored to the sole preset-sources input with explicit released_projection_recovery provenance and exact identity/row digests. The generator does not read its output as a source and preserves every released row and its existing field evidence without filling unknown fields. No new network observation, upstream capture, official verification, human model review, authorization, route eligibility, or signature is asserted; ark-code-latest still has no upstream verification hash or date. All 1092 candidate model rows across 31 profiles, coverage scopes, gaps, source references, and original observation dates equal the fixed released artifact. Only three content revisions are recomputed. All 912 previously reviewed catalog records, approved inputs, aliases, pi references and price resources remain unchanged. Preset projection SHA256=b27dc917f08806d5e58997931a3c6474ca5982910ecf8a76596c2698faf7efe4; generated presets SHA256=da0303082c648147331fc2587bcc591cb2e009cf278ea7c887d2b308d5b06136; offline preset generator SHA256=3729fe05c76ae6972367519ace03de01e9247f8524a237db4c929fd32a8e33d4. The generation advances for the corrected reproducible projection; the compiled hash pin is independently checked and written by the parent delivery agent."
CATALOG = "backend/resources/model-capabilities/catalog.json"
REVIEWED = "backend/resources/model-capabilities/scripts/reviewed.json"
SOURCES = "backend/resources/model-capabilities/scripts/preset-sources.json"
PRESETS = "backend/internal/service/platform_model_presets.json"
MANIFEST = "backend/resources/model-capabilities/release-manifest.json"
PIN = "backend/resources/model-capabilities/release-pin.json"


def require(condition, message):
    if not condition:
        raise ValueError(message)


def digest(body):
    return hashlib.sha256(body).hexdigest()


def encode(value):
    return (json.dumps(value, ensure_ascii=False, indent=2, sort_keys=True) + "\n").encode()


def git_blob(body):
    return hashlib.sha1(b"blob " + str(len(body)).encode() + b"\0" + body).hexdigest()


def verify_bodies(bodies):
    require(set(bodies) == set(EXPECTED), "artifact file set differs from the four reviewed JSON files")
    result = []
    for path, expected in sorted(EXPECTED.items()):
        body = bodies[path]
        require(digest(body) == expected, "reviewed SHA256 mismatch: " + path)
        result.append({"path": path, "sha256": expected, "git_blob_sha": git_blob(body), "bytes": len(body)})
    return result


def verify_root(root):
    require(not root.is_symlink() and root.is_dir(), "artifact root must be a real directory")
    root = root.resolve(strict=True)
    files = {}
    for path in root.rglob("*"):
        require(not path.is_symlink(), "artifact contains a symlink: " + str(path))
        if path.is_file():
            files[path.relative_to(root).as_posix()] = path
        else:
            require(path.is_dir(), "artifact contains a non-regular entry: " + str(path))
    require(set(files) == set(EXPECTED), "artifact file set differs from the four reviewed JSON files")
    return verify_bodies({name: path.read_bytes() for name, path in files.items()})


def git_read(root, ref, path):
    return subprocess.run(
        ["git", "show", ref + ":" + path], cwd=root,
        check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
    ).stdout


def replace_once(body, old, new):
    require(body.count(old) == 1, "restoration replacement must match exactly once")
    return body.replace(old, new, 1)


def recover(root):
    original = {}
    for path, expected in BASE_HASHES.items():
        body = git_read(root, BASE, path)
        require(digest(body) == expected, "fixed baseline SHA256 mismatch: " + path)
        require(git_read(root, UPLOADED, path) == body, "uploaded baseline differs: " + path)
        original[path] = body

    manifest_body = (root / MANIFEST).read_bytes()
    require(digest(manifest_body) == MANIFEST_HASH, "independently reviewed manifest must already be restored")
    manifest = json.loads(manifest_body)
    require(json.loads((root / PIN).read_bytes()) == {
        "approved_generation": GENERATION, "manifest_sha256": MANIFEST_HASH,
    }, "independently reviewed pin must already be restored; recovery never writes a pin")
    require(manifest["approved_generation"] == GENERATION and manifest["record_count"] == 912,
            "reviewed manifest identity differs")
    require(len(manifest["code_versions"]) == 98, "reviewed code dependency count differs")
    for name, expected in manifest["code_versions"].items():
        require(digest((root / "backend" / name).read_bytes().replace(b"\r\n", b"\n")) == expected,
                "reviewed code dependency differs: " + name)

    script_dir = root / "backend/resources/model-capabilities/scripts"
    require(digest((script_dir / "presets.py").read_bytes()) == GENERATOR_HASH,
            "reviewed preset generator differs")
    utility_path = "backend/resources/model-capabilities/scripts/catalog.py"
    require((root / utility_path).read_bytes() == git_read(root, UPLOADED, utility_path),
            "catalog utility differs from fixed uploaded code")
    sys.path.insert(0, str(script_dir))
    import presets

    base_presets = json.loads(original[PRESETS])
    sources = json.loads(original[SOURCES])
    require(encode(sources) == original[SOURCES], "source input encoding differs")
    require(len(base_presets) == 31 and sum(len(profile["models"]) for profile in base_presets) == 1092,
            "released candidate inventory differs")
    wanted = {
        ("qianwen", "alibaba"): {"qwen3.8-omni-flash", "qwen3.8-omni-flash-realtime"},
        ("qianwen", "alibaba-cn"): {"qwen3.8-omni-flash", "qwen3.8-omni-flash-realtime"},
        ("volcengine", "native"): {"ark-code-latest"},
    }
    actual = {(profile["platform"], profile["provider_profile"]): profile for profile in base_presets}
    source_id = "released-v0.3.3-missing-projections"
    require(source_id not in sources["sources"], "fixed baseline already contains recovery source")
    recovery = {
        "kind": "released_projection_recovery",
        "url": "https://github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/blob/" + BASE + "/" + PRESETS,
        "released_from": {
            "repository": "https://github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW",
            "commit": BASE, "path": PRESETS, "artifact_sha256": digest(original[PRESETS]),
        },
        "recovery_basis": "Exact candidate rows and their existing field evidence recovered from the v0.3.3 preset artifact at commit 94d8a70b39c46895cac3817f3b230fb5eabe2692 to repair a missing source projection. No new network observation, upstream capture, official verification, model approval, entitlement, or endpoint guarantee is asserted. The released artifact hash identifies repository history only; original upstream hash/time are preserved where present and remain absent where absent.",
        "recovered_records": [],
    }
    for profile in sources["profiles"]:
        key = (profile["platform"], profile["provider_profile"])
        if key not in wanted:
            continue
        old = actual[key]
        require(not {model["id"] for model in profile["models"]} & wanted[key],
                "fixed source baseline unexpectedly contains restored IDs")
        for model in old["models"]:
            if model["id"] not in wanted[key]:
                continue
            row = copy.deepcopy(model)
            row["source"] = source_id
            profile["models"].append(row)
            recovery["recovered_records"].append({
                "platform": key[0], "provider_profile": key[1], "model_id": model["id"],
                "projection_sha256": digest(encode(model)),
            })
        profile["gaps"] = copy.deepcopy(old["gaps"])
        profile["source_refs"] = copy.deepcopy(old["source_refs"])
    require(len(recovery["recovered_records"]) == 5, "must restore exactly five released candidate rows")
    sources["sources"][source_id] = recovery
    generated = presets.generate(sources)
    require(len(generated) == len(base_presets), "profile count changed")
    changed_revisions = []
    for old, new in zip(base_presets, generated):
        require({key: value for key, value in old.items() if key != "revision"} ==
                {key: value for key, value in new.items() if key != "revision"},
                "candidate facts, evidence, scope, gaps or source references changed")
        if old["revision"] != new["revision"]:
            changed_revisions.append((old["platform"], old["provider_profile"]))
    require(set(changed_revisions) == set(wanted) and len(changed_revisions) == 3,
            "unexpected preset revision change set")

    old_catalog = json.loads(original[CATALOG])
    old_review = json.loads(original[REVIEWED])
    require(len(old_catalog["records"]) == 912 and digest(encode(old_catalog["records"])) ==
            "c94aaae4695acf4ed98365761cbe3e70a2104b96e4c1bfbfd86c1fd455590943",
            "approved catalog records differ")
    require(old_catalog["generated_at"] == old_review["generated_at"] == "2026-10-11T00:21:59Z",
            "fixed baseline generation differs")
    old_stamp = b'"generated_at": "2026-10-11T00:21:59Z"'
    new_stamp = ('"generated_at": "' + STAMP + '"').encode()
    catalog_body = replace_once(original[CATALOG], old_stamp, new_stamp)
    review_body = replace_once(
        original[REVIEWED],
        json.dumps(old_review["approval_basis"], ensure_ascii=False).encode(),
        json.dumps(old_review["approval_basis"] + INITIAL_NOTE + RECOVERY_NOTE, ensure_ascii=False).encode(),
    )
    review_body = replace_once(review_body, old_stamp, new_stamp)
    bodies = {CATALOG: catalog_body, REVIEWED: review_body, SOURCES: encode(sources), PRESETS: encode(generated)}
    result = verify_bodies(bodies)
    require(git_blob(catalog_body) == "774806670cde329b52027187a289b8b480e0ef8d", "catalog Git blob differs")
    require(git_blob(review_body) == "fa044b32bc025b490ac7b456c95c09eeb06ad52b", "review Git blob differs")
    require(manifest["catalog_sha256"] == EXPECTED[CATALOG] and
            manifest["review_sha256"] == EXPECTED[REVIEWED] and
            manifest["presets_sha256"] == EXPECTED[PRESETS], "manifest does not bind the recovered artifacts")
    for path, body in bodies.items():
        target = root / path
        require(not target.is_symlink() and target.resolve().is_relative_to(root),
                "refusing an artifact path outside the checkout")
        require(target.read_bytes() in (original[path], body), "refusing to overwrite unexpected changes: " + path)
    # Every byte and precondition is checked before writing the first artifact.
    for path, body in bodies.items():
        (root / path).write_bytes(body)
    require((root / MANIFEST).read_bytes() == manifest_body, "manifest unexpectedly changed")
    verify_bodies({path: (root / path).read_bytes() for path in EXPECTED})
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    modes = parser.add_mutually_exclusive_group()
    modes.add_argument("--recover", action="store_true", help="restore only the four fixed reviewed JSON files (default)")
    modes.add_argument("--verify-root", type=Path, help="only verify an isolated four-file artifact directory")
    args = parser.parse_args()
    if args.verify_root is not None:
        result = verify_root(args.verify_root)
        mode = "verify-root"
    else:
        result = recover(Path(__file__).resolve().parents[1])
        mode = "recover"
    print(json.dumps({"mode": mode, "generation": GENERATION, "manifest_sha256": MANIFEST_HASH,
                      "files": result, "pin_written": False}, sort_keys=True, indent=2))


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, KeyError, TypeError, subprocess.CalledProcessError) as error:
        print("catalog recovery refused: " + str(error), file=sys.stderr)
        raise SystemExit(1)
