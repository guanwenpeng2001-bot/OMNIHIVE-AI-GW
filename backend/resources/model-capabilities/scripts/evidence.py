"""Price-free source capture and scope ledger; no network or approval authority."""
from collections import Counter
from datetime import date
from pathlib import Path

import catalog



def task_group(output):
    for modality, task in (("embeddings", "embedding"), ("rerank", "rerank"),
                           ("transcription", "audio_transcription"), ("speech", "audio_generation"),
                           ("image", "image"), ("audio", "audio"), ("video", "video")):
        if modality in output:
            return task
    return "text_generation" if output == ["text"] else "unknown"


def freeze_accounts(accounts, observed_at):
    """Project an explicit admin read into credential-free D union M identities.

    Never retain the raw response. Account identity stays in the denominator;
    identically named models on two accounts are not silently deduplicated.
    """
    frozen, models = [], []
    seen = set()
    for account in sorted(accounts, key=lambda a: a['id']):
        account_id = account['id']
        catalog.require(account_id not in seen, "duplicate account in freeze")
        seen.add(account_id)
        extra, credentials = account.get('extra') or {}, account.get('credentials') or {}
        saved = extra.get('upstream_model_catalog') or {}
        metadata = (extra.get('upstream_model_metadata') or {}).get('models') or {}
        mapping = credentials.get('model_mapping') or {}
        catalog.require(isinstance(mapping, dict), "invalid model mapping")
        discovered = saved.get('discovered_models') or []
        catalog.require(all(isinstance(v, str) for v in discovered), "invalid discovery IDs")
        catalog.require(all(isinstance(v, str) for v in mapping.values()), "invalid mapping targets")
        targets = set(mapping.values())
        profile = saved.get('provider_profile') or account['platform'] + ':' + account['type'] + ':unresolved'
        scope = {'platform': account['platform'], 'auth_type': account['type'],
                 **{k: credentials.get(k) for k in ('base_url', 'api_base_urls', 'api_protocol', 'account_mode')}}
        frozen.append({'account_id': account_id, 'platform': account['platform'], 'auth_type': account['type'],
            'deployment_profile': profile, 'deployment_sha256': catalog.digest(catalog.encode(scope)),
            'mapping_sha256': catalog.digest(catalog.encode(mapping)),
            'identity_revision': saved.get('identity_revision'), 'rules_revision': saved.get('rules_revision'),
            'catalog_revision': saved.get('revision'), 'catalog_observed_at': saved.get('observed_at'),
            'schema_version': saved.get('schema_version'), 'source_generation': saved.get('source_generation'),
            'source_versions': saved.get('source_versions'), 'D': len(set(discovered)), 'M': len(targets),
            'U': len(set(discovered) | targets), 'E': len(saved.get('eligible_models') or []),
            'scope_status': 'saved_discovery_and_mapping' if saved else 'discovery_missing'})
        for model_id in sorted(set(discovered) | targets):
            record = metadata.get(model_id, {})
            models.append({'set':'U0', 'account_id':account_id, 'deployment_profile':profile,
                'profile':record.get('provider_profile') or profile, 'model_id':model_id,
                'task_type':record.get('task_type'), 'discovered':model_id in discovered,
                'metadata_status':record.get('metadata_status', 'unobserved'),
                'missing_fields':record.get('missing_fields') or ([] if record.get('metadata_status') == 'complete' else ['native_metadata']),
                'estimated_fields':record.get('estimated_fields', []), 'conflicts':record.get('conflicts', []),
                'mapping_references':sorted(k for k,v in mapping.items() if v == model_id)})
    return {'status':'frozen', 'environment':'current local development database; not production',
            'observed_at':observed_at, 'definition':'account-scoped union of saved discovery D and mapping targets M; no upstream resync performed',
            'account_count':len(frozen), 'identity_count':len(models), 'accounts':frozen,
            'limitations':['Saved discovery is not a fresh native specification or authorization check.',
                           'This capture is newer than P0-01 and does not reconstruct its missing U0 list.',
                           'Platforms with no current account are not measured.']}, models


def capture_registry(args):
    catalog.require(len(args.source) == 1, "one complete registry page required")
    url, filename = args.source[0]
    catalog.require(url == "https://openrouter.ai/api/v1/models?output_modalities=all", "unsupported registry scope")
    page = catalog.read(filename)
    catalog.require(isinstance(page.get("data"), list) and page["data"], "empty registry")
    catalog.require(not page.get("error") and not page.get("next") and not page.get("links", {}).get("next") and not page.get("has_more"), "incomplete registry pagination; capture all pages with P1-08 first")
    date.fromisoformat(args.verified_at)
    raw_hash = catalog.digest(Path(filename).read_bytes())
    records, seen = [], set()
    for raw in sorted(page["data"], key=lambda r: r["id"]):
        model_id = raw["id"]
        catalog.require(isinstance(model_id, str) and "/" in model_id and model_id not in seen, "duplicate/invalid registry ID")
        seen.add(model_id)
        values, rejected = {}, {}
        for target, value in (("context_window", raw.get("context_length")),
                              ("max_output_tokens", raw.get("top_provider", {}).get("max_completion_tokens"))):
            if value is not None:
                if type(value) is int and 0 < value <= 2**63-1:
                    values[target] = value
                else:
                    rejected[target] = {"value": value, "reason": "not a positive int64 limit",
                                        "source_url": url, "observed_at": args.verified_at, "content_hash": raw_hash}
        for target, value in (("input_modalities", raw.get("architecture", {}).get("input_modalities")),
                              ("output_modalities", raw.get("architecture", {}).get("output_modalities")),
                              ("supported_parameters", raw.get("supported_parameters"))):
            if value is not None:
                catalog.require(isinstance(value, list) and all(isinstance(v, str) and v.strip() for v in value), "invalid registry list")
                values[target] = value
        output = values.get("output_modalities", [])
        task = task_group(output)
        records.append({"model_id": model_id, "profile": "openrouter", "deployment": "openrouter.ai", "region": "global",
                        "task_group": task, "task_group_evidence": "derived grouping only, not endpoint authorization",
                        "alias_reason": "exact ID only; no reviewed cross-deployment alias", "rejected_fields": rejected,
                        "field_evidence": {field: {"value": value, "source_url": url,
                            "observed_at": args.verified_at, "content_hash": raw_hash,
                            "deployment": "openrouter.ai/global", "confidence": "candidate",
                            "constraint": "provider union; not a native DashScope/author declaration"}
                            for field, value in values.items()}})
    if page.get("total") is not None:
        catalog.require(page["total"] == len(records), "registry total mismatch")
    catalog.write(args.output, {"schema_version": 1, "format": "openrouter-candidate-evidence-v1",
        "source_url": url, "observed_at": args.verified_at, "content_hash": raw_hash,
        "approved": False, "records": records})


def handoff(scope, registry, published):
    catalog.require(scope.get("schema_version") == 1, "invalid scope")
    index = {(r["profile"], r["model_id"]): r for r in published["records"]}
    candidates = registry["records"]
    rows = []
    identities = set()
    for entry in scope["models"]:
        key = (entry["set"], entry.get('account_id'), entry["profile"], entry["model_id"])
        catalog.require(key not in identities, "duplicate frozen scope identity")
        identities.add(key)
        # A lexical relationship is retained only as rejected evidence, never
        # as an alias or a fact about a different hosting deployment.
        related = [r for r in candidates if r["model_id"] == entry["model_id"] or r["model_id"].split("/", 1)[-1] == entry["model_id"]]
        record = index.get((entry["profile"], entry["model_id"]))
        rows.append({**entry, "owner": "P2-01", "review_after": "2026-10-01",
                     "status": "requires_native_evidence_or_account_revalidation",
                     "published_record": None if record is None else {"profile": record["profile"], "model_id": record["model_id"], "source": record["source"], "content_hash": record["source_content_hash"]},
                     "candidate_evidence": [{"model_id": r["model_id"], "observation_sha256": catalog.digest(catalog.encode(registry)),
                         "fields": sorted(r["field_evidence"]), "source_url": registry["source_url"],
                         "observed_at": registry["observed_at"], "content_hash": registry["content_hash"],
                         "deployment": "openrouter.ai/global", "decision": "rejected",
                         "alias_reason": "no reviewed alias for target deployment",
                         "reason": "cross-deployment or unreviewed identity; provider-union parameters do not prove endpoints"} for r in related],
                     "next_step": "Obtain native deployment/model specification or authorized private declaration for each missing field; preserve account identity/mapping and revalidate against approved release.",
                     "no_public_spec": False})
    counts = Counter((r["set"], r["profile"], r.get("task_type") or "unknown") for r in rows)
    historical = {(r['profile'],r['model_id']) for r in rows if r['set']=='H485'}
    current = {(r['profile'],r['model_id']) for r in rows if r['set']=='U0'}
    return {"schema_version": 1, "scope_sha256": catalog.digest(catalog.encode(scope)),
            "observation_sha256": catalog.digest(catalog.encode(registry)),
            "historical_count": sum(r["set"] == "H485" for r in rows),
            "current_u0": scope["current_u0"], "g_adjudicated": "partial", "g_complete": "not_met",
            "approved_new_registry_records": 0,
            "candidate_provider_task_groups": [
                {"profile":"openrouter", "deployment":"openrouter.ai/global", "task":task, "count":count}
                for task,count in sorted(Counter(r['task_group'] for r in candidates).items())],
            "rejected_registry_fields": sum(len(r.get('rejected_fields', {})) for r in candidates),
            "delta": {"current_unique_profile_ids":len(current),
                "historical_ids_present_in_current":len(historical & current),
                "historical_ids_absent_from_current":[{'profile':p,'model_id':m,'owner':'P2-01','reason':'not in current account union; retained in H485'} for p,m in sorted(historical-current)],
                "current_ids_not_in_historical_incomplete":[{'profile':p,'model_id':m} for p,m in sorted(current-historical)]},
            "provider_task_groups": [{"set": s, "profile": p, "task": t, "count": n} for (s,p,t), n in sorted(counts.items())],
            "scope_blockers": [] if scope["current_u0"].get("status") == "frozen" else [
                {"owner": "P2-01", "item": "current-U0-identities", "reason": scope["current_u0"]["reason"],
                 "next_step": "Freeze current per-account discovery union mapping targets with deployment/identity/rules digests, then rerun this report; historical H485 is not U0.", "review_after": "2026-10-01"}],
            "models": rows}


def generate_handoff(scope_path, registry_path, catalog_path=catalog.CATALOG):
    return handoff(catalog.read(scope_path), catalog.read(registry_path), catalog.read(catalog_path))
