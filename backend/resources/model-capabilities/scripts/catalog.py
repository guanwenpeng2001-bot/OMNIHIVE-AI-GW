#!/usr/bin/env python3
"""Offline, standard-library-only capability candidate and coverage maintenance.

No command downloads data or publishes a candidate. See README.md for review steps.
"""
import argparse
import copy
import hashlib
import json
from pathlib import Path
import re
import sys
from datetime import date, datetime

ROOT = Path(__file__).resolve().parent
CATALOG = ROOT.parent / "catalog.json"
INPUTS = ROOT / "inputs.json"
REVIEWED = ROOT / "reviewed.json"
OUTPUT = ROOT.parents[3] / ".pi/validation/next-development/offline-generation-toolchain"
LIMITS = ("max_input_tokens", "max_output_tokens")
BOOLS = ("supports_reasoning", "supports_vision", "supports_function_calling", "supports_prompt_caching")
MODALITIES = ("input_modalities", "output_modalities")
FIELDS = LIMITS + BOOLS + MODALITIES
EXTENDED_FIELDS = ("supported_methods", "embedding_dimensions", "reasoning_control", "created_at", "model_version")
REVIEW_FIELDS = set(FIELDS + EXTENDED_FIELDS + ("mode", "context_window", "context_limit_kind"))
# Suppression is explicit signed per-record data, never inferred from omission.
SUPPRESSION_FIELDS = set(LIMITS + ('context_window', 'context_limit_kind'))
TEXT_MODES = {"chat", "responses", "completion"}
TIERS = {"local-data", "online-registry", "official-schema"}
# Provider namespaces are audited here, never used to create model aliases.
HOSTS = {
    "api.openai.com": "openai", "chatgpt.com": "openai",
    "api2.cursor.sh": "cursor", "api.cursor.sh": "cursor", "api.cursor.com": "cursor",
    "opencode.ai": "opencode-go", "api.x.ai": "xai",
    "dashscope.aliyuncs.com": "alibaba-cn", "api.anthropic.com": "anthropic",
    "generativelanguage.googleapis.com": "google",
}
PROVIDER_REVIEW = {
    "gemini": {"registry_provider": "google", "scope": "Gemini API only; no Vertex aliases"},
    "xai": {"registry_provider": "xai", "scope": "official API; no Grok OAuth aliases"},
    "anthropic": {"registry_provider": "anthropic", "scope": "official API; no Bedrock/Vertex aliases"},
    "alibaba-cn": {"registry_provider": "alibaba-cn", "scope": "China DashScope only; no international aliases"},
}


def encode(value):
    return (json.dumps(value, ensure_ascii=False, indent=2, sort_keys=True) + "\n").encode()


def digest(body):
    return hashlib.sha256(body).hexdigest()


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f"duplicate JSON key: {key}")
        result[key] = value
    return result


def read(path):
    return json.loads(Path(path).read_bytes(), object_pairs_hook=unique_object)


def write(path, value):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(path.name + ".tmp")
    temporary.write_bytes(encode(value))
    temporary.replace(path)


def require(condition, message):
    if not condition:
        raise ValueError(message)


def missing_fields(record, required):
    # Explicit false and budget zero are facts, while empty modality/method
    # lists and structured unknown controls do not prove a complete profile.
    return [field for field in required if field not in record or record[field] == "unknown"
            or record[field] == [] or (isinstance(record[field], dict) and record[field].get("kind") == "unknown")]


def check_fields(record):
    check_identity_facts(record)
    for field in LIMITS:
        if field in record:
            require(type(record[field]) is int and 0 < record[field] <= 2**63 - 1,
                    f"{field} must be a positive int64")
    for field in BOOLS:
        if field in record:
            require(type(record[field]) is bool, f"{field} must be boolean")
    for field in MODALITIES:
        if field in record:
            require(isinstance(record[field], list) and all(
                isinstance(v, str) and v.strip() for v in record[field]), f"invalid {field}")


def check_identity_facts(record):
    if "created_at" in record:
        value = record["created_at"]
        require(isinstance(value, str) and re.fullmatch(
            r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})", value),
            "created_at must be an explicit RFC3339 timestamp")
        datetime.fromisoformat(value.replace("Z", "+00:00"))
    if "model_version" in record:
        require(isinstance(record["model_version"], str) and record["model_version"].strip(),
                "model_version must be an explicit nonempty string")


def capture(args):
    """Strip all pricing before committing a historical observation fixture."""
    sources = []
    for label, filename in args.source:
        raw = Path(filename).read_bytes()
        entries = read(filename)
        require(isinstance(entries, dict) and entries, "empty source")
        records = []
        for model_id, entry in sorted(entries.items()):
            if model_id == "sample_spec":
                continue
            require(isinstance(entry, dict), f"invalid source entry {model_id}")
            provider = entry.get("litellm_provider")
            require(isinstance(provider, str) and provider.strip(), f"missing provider: {model_id}")
            record = {"model_id": model_id, "provider": provider, "profile": provider}
            if entry.get("mode"):
                record["mode"] = entry["mode"]
            record.update({field: entry[field] for field in FIELDS if field in entry})
            normalized = public_record(entry)
            record.update({field: normalized[field] for field in EXTENDED_FIELDS if field in normalized})
            check_fields(record)
            validate_control(record)
            records.append(record)
        lf = raw.replace(b"\r\n", b"\n")
        # Git may check the same reviewed JSON out with LF or CRLF. Bind both
        # concrete byte representations, not arbitrary future normalized data.
        equivalent_hashes = sorted({digest(raw), digest(lf), digest(lf.replace(b"\n", b"\r\n"))})
        sources.append({"id": label, "tier": args.tier, "content_hash": digest(raw),
                        "equivalent_content_hashes": equivalent_hashes,
                        "verified_at": args.verified_at, "records": records})
    require(len({s['id'] for s in sources}) == len(sources), "duplicate source ID")
    date.fromisoformat(args.verified_at)
    write(args.output, {"schema_version": 1, "sources": sorted(sources, key=lambda s: s["id"])})


def generate(inputs, reviewed=None):
    require(inputs.get("schema_version") == 1 and inputs.get("sources"), "invalid inputs")
    sources = sorted(inputs["sources"], key=lambda s: s["id"])
    require(len({source["id"] for source in sources}) == len(sources), "duplicate source ID")
    by_key = {}
    for source in sources:
        require(source["tier"] in TIERS, "unknown evidence tier")
        require(re.fullmatch(r"[0-9a-f]{64}", source["content_hash"]), "invalid source hash")
        date.fromisoformat(source["verified_at"])
        seen = set()
        for record in source["records"]:
            check_fields(record)
            key = (record["profile"], record["model_id"])
            require(key not in seen, f"duplicate source identity {key}")
            seen.add(key)
            by_key.setdefault(key, []).append((source, record))
    source_hash = digest(encode(inputs))
    applicable = sorted({h for source in sources for h in
                         source.get("equivalent_content_hashes", [source["content_hash"]])})
    verified = max(source["verified_at"] for source in sources)
    records, rows = [], []
    for (profile, model_id), observations in sorted(by_key.items()):
        providers = {record["provider"] for _, record in observations}
        require(len(providers) == 1, f"provider conflict for {profile}/{model_id}")
        record = {"provider": providers.pop(), "profile": profile, "model_id": model_id,
                  "source": "local-data:resources/model-capabilities/scripts/inputs.json",
                  "source_content_hash": source_hash,
                  "verified_at": max(source["verified_at"] for source, _ in observations),
                  "applicable_content_hashes": applicable, "field_sources": {}}
        conflicts, evidence = [], {}
        for field in FIELDS + ("mode",) + EXTENDED_FIELDS:
            observed = [(source, entry[field]) for source, entry in observations if field in entry]
            if not observed:
                continue
            evidence[field] = [{"source": source["id"], "content_hash": source["content_hash"],
                                "tier": source["tier"], "value": value} for source, value in observed]
            if any(value != observed[0][1] for _, value in observed):
                conflicts.append(field)
                continue  # Runtime/embed retain their own values; supplements cannot arbitrate.
            record[field] = observed[0][1]
            source = observed[0][0]
            record["field_sources"][field] = {
                "source": f"{source['tier']}:{source['id']}", "content_hash": source["content_hash"],
                "observed_at": source["verified_at"], "confidence": "observed"}
        if record.get("supports_reasoning") is not False and "reasoning_control" not in record:
            record["reasoning_control"] = "unknown"
        required = list(LIMITS + ("supports_reasoning", "supports_vision", "supports_function_calling", "supported_methods") + MODALITIES)
        if record.get("supports_reasoning") is not False:
            required.append("reasoning_control")
        missing = missing_fields(record, required)
        is_text = record.get("mode") in TEXT_MODES
        if record.get("mode") == "embedding":
            required = ["max_input_tokens", "input_modalities", "output_modalities", "supported_methods", "embedding_dimensions"]
            missing = missing_fields(record, required)
        elif not is_text:
            missing = ["task_specific_required_fields", "gateway_endpoint_evidence"]
        rows.append({"original_id": model_id, "profile": profile, "task": record.get("mode", "unknown"),
                     "required_fields": required if is_text or record.get("mode") == "embedding" else "requires task adapter contract",
                     "field_evidence": evidence, "match_rule": "exact-profile-and-original-id",
                     "missing_fields": missing, "conflicts": conflicts,
                     "status": "incomplete" if missing or conflicts else "complete",
                     "endpoint": "not-observed", "authorization": "not-observed",
                     "exclusion_before": "not-observed", "exclusion_after": "not-evaluated",
                     "mapping_references": None})
        records.append(record)
    catalog = {"schema_version": 1, "generated_at": verified + "T00:00:00Z",
               "description": "Pinned local observations, not current official specifications or account authorization. Conflicting fields are omitted; runtime/embed keep their respective observed limits. Unknown reasoning controls remain unknown under P0 schema. No model aliases or prices.",
               "records": records}
    if reviewed is None:
        reviewed = read(REVIEWED)
    # The old reviewed supplement is reproducible independently of candidate
    # observations. New observations cannot silently inherit its approval.
    catalog["generated_at"] = reviewed["generated_at"]
    catalog["description"] = reviewed["description"]
    records.extend(copy.deepcopy(reviewed["records"]))
    apply_reviewed_overlays(records, rows, reviewed.get("overlays", []))
    validate(catalog)
    counts = {field: sum(field in record for record in records) for field in FIELDS}
    source_counts = {source["id"]: {"models": len(source["records"]), **{
        field: sum(field in record for record in source["records"]) for field in FIELDS}}
        for source in sources}
    report = {"schema_version": 1, "scope": "union of pinned local source IDs; NOT account frozen U",
              "frozen_u_acceptance": "unverified: no current account scope or native authorization established by this source-union report",
              "catalog_sha256": digest(encode(catalog)), "source_counts": source_counts,
              "summary": {"models": len(records), "field_presence": counts,
                          "complete": sum(row["status"] == "complete" for row in rows),
                          "incomplete": sum(row["status"] == "incomplete" for row in rows),
                          "conflicting_fields": sum(len(row["conflicts"]) for row in rows),
                          "models_with_conflicts": sum(bool(row["conflicts"]) for row in rows)},
              "provider_review": PROVIDER_REVIEW, "known_hosts": HOSTS,
              "missing_provider_evidence": ["alibaba-cn (no Qwen observations)",
                  "alibaba international DashScope (no observations or P0 KnownHost entry)",
                  "Grok OAuth, Antigravity, CommandCode and private deployments (no account profiles)"],
              "limitations": ["max_input_tokens is an input limit, not proof of total context; max_tokens is never imported",
                  "supports_vision is a partial observation, never a complete input modality list",
                  "P0 loader reconstructs field_sources using the bundle hash; detailed original field evidence is in this report",
                  "P0 supports only unknown reasoning_control; no effort, budget, toggle or fixed values are invented",
                  "schema documents prove field shapes, not per-model or hosted deployment facts"],
              "models": rows}
    for record in records[len(by_key):]:
        required = ["max_input_tokens", "input_modalities", "output_modalities", "supported_methods", "embedding_dimensions"] if record.get("mode") == "embedding" else ["task_specific_required_fields", "gateway_endpoint_evidence"]
        missing = missing_fields(record, required)
        prior_row = next((row for row in rows if row['profile'] == record['profile'] and row['original_id'] == record['model_id']), None)
        row = {"original_id": record["model_id"], "profile": record["profile"],
                     "task": record.get("mode", "unknown"), "status": "incomplete" if missing else "complete", "missing_fields": missing,
                     "required_fields": required,
                     "conflicts": [], "match_rule": "exact-profile-and-original-id",
                     "field_evidence": {field: [{**record.get('field_sources', {}).get(field, {
                         "source": record["source"], "content_hash": record["source_content_hash"], "observed_at": record["verified_at"]}),
                         "value": record[field], "deployment": {k: record[k] for k in ("profile", "region", "protocol") if k in record}}]
                         for field in sorted(REVIEW_FIELDS) if field in record}}
        if prior_row is not None:
            row['conflicts'] = prior_row['conflicts']
            row['field_evidence'].update(prior_row['field_evidence'])
            row['missing_fields'] = sorted(set(row['missing_fields'] + prior_row['missing_fields']))
            row['status'] = 'incomplete' if row['missing_fields'] or row['conflicts'] else 'complete'
            rows.remove(prior_row)
        rows.append(row)
    report["summary"].update(models=len(records), complete=sum(r["status"] == "complete" for r in rows),
                             incomplete=sum(r["status"] == "incomplete" for r in rows),
                             conflicting_fields=sum(len(r['conflicts']) for r in rows),
                             models_with_conflicts=sum(bool(r['conflicts']) for r in rows))
    report["limitations"] = ["Local observations are historical, not native authority.",
        "Structured reasoning_control maps to internal control; unknown remains unknown.",
        "Embedding output modality embedding denotes a numeric vector; text describes its input only.",
        "Source union is not H485 or frozen U0; see handoff.json for independent denominators."]
    report["missing_provider_evidence"][0] = "alibaba-cn: only reviewed embedding records; other deployments unverified"
    manifest = {"schema_version": 1, "generated_at": catalog["generated_at"],
                "catalog_sha256": digest(encode(catalog)), "inputs_sha256": source_hash,
                "coverage_sha256": digest(encode(report)), "record_count": len(records),
                "sources": [{key: value for key, value in source.items() if key != "records"} for source in sources]}
    return catalog, report, manifest


def validate(catalog):
    require(catalog.get("schema_version") == 1, "unsupported catalog schema")
    datetime.strptime(catalog["generated_at"], "%Y-%m-%dT%H:%M:%SZ")
    require(isinstance(catalog.get("records"), list) and catalog["records"], "empty catalog")
    allowed = set(FIELDS) | {"provider", "profile", "model_id", "region", "protocol", "mode",
        "reasoning_control", "supported_methods", "embedding_dimensions", "aliases", "created_at", "model_version",
        "source", "verified_at", "source_content_hash", "applicable_content_hashes", "field_sources",
        "context_window", "context_limit_kind", "suppressed_fields"}
    seen = set()
    for record in catalog["records"]:
        require(not (set(record) - allowed), f"unsupported/price fields: {set(record) - allowed}")
        for field in ("provider", "profile", "model_id", "source"):
            require(isinstance(record.get(field), str) and record[field].strip() and "\0" not in record[field],
                    f"invalid {field}")
        key = (record["profile"], record["model_id"])
        require(key not in seen, f"duplicate identity {key}")
        seen.add(key)
        check_fields(record)
        suppressed = record.get('suppressed_fields', [])
        require(isinstance(suppressed, list) and len(suppressed) == len(set(suppressed)) and
                set(suppressed) <= SUPPRESSION_FIELDS, 'invalid suppression field')
        require(not set(suppressed) & set(record), 'suppressed field still present')
        if 'context_window' in record:
            require(type(record['context_window']) is int and 0 < record['context_window'] <= 2**63 - 1,
                    'invalid context window')
            require(record.get('context_limit_kind') in ('input', 'total'), 'missing context limit kind')
        for field, provenance in record.get('field_sources', {}).items():
            require(field in REVIEW_FIELDS, 'unknown evidence field')
            require(not set(provenance) - {'source', 'content_hash', 'observed_at', 'confidence', 'proof_scope'},
                    'unknown provenance field')
            require(provenance.get('source') and re.fullmatch(r'[0-9a-f]{64}', provenance.get('content_hash', '')),
                    'invalid field provenance')
            if 'observed_at' in provenance:
                date.fromisoformat(provenance['observed_at'])
            if 'confidence' in provenance:
                require(provenance['confidence'] in ('verified', 'observed'), 'invalid confidence')
        validate_control(record)
        if "embedding_dimensions" in record:
            require(type(record["embedding_dimensions"]) is int and 0 < record["embedding_dimensions"] <= 2**63 - 1,
                    "invalid embedding dimensions")
        for name in ("supported_methods", "aliases"):
            if name in record:
                require(isinstance(record[name], list) and all(isinstance(v, str) and v.strip() for v in record[name]),
                        f"invalid {name}")
        date.fromisoformat(record["verified_at"])
        hashes = [record["source_content_hash"]] + record["applicable_content_hashes"]
        require(bool(record["applicable_content_hashes"]), "missing applicability")
        require(all(isinstance(h, str) and re.fullmatch(r"[0-9a-f]{64}", h) for h in hashes), "invalid SHA256")
        for field in FIELDS:
            if field in record:
                provenance = record.get("field_sources", {}).get(field, {})
                if record["source"].startswith("https://") and not provenance:
                    provenance = {"source": record["source"], "content_hash": record["source_content_hash"]}
                require(provenance.get("source") and re.fullmatch(r"[0-9a-f]{64}", provenance.get("content_hash", "")),
                        f"missing field provenance: {key}/{field}")


CONTROL_KINDS = {"none", "effort", "toggle", "budget", "adaptive", "fixed", "unknown"}


def apply_reviewed_overlays(records, rows, overlays):
    indexed = {(r['profile'], r['model_id']): r for r in records}
    coverage = {(r['profile'], r['original_id']): r for r in rows}
    seen = set()
    for note in overlays:
        require(not set(note) - {'profile', 'model_id', 'source', 'source_content_hash', 'verified_at',
                                'proof_scope', 'fields', 'suppress_fields', 'field_sources', 'prior_evidence'}, 'unknown overlay field')
        key = (note['profile'], note['model_id'])
        require(key in indexed and key not in seen, 'overlay must name one existing identity')
        seen.add(key)
        require(note.get('proof_scope') and note['source'].startswith('https://'), 'missing overlay proof scope')
        require(re.fullmatch(r'[0-9a-f]{64}', note['source_content_hash']), 'invalid overlay hash')
        date.fromisoformat(note['verified_at'])
        fields, suppressed = note.get('fields', {}), note.get('suppress_fields', [])
        require(isinstance(fields, dict) and set(fields) <= REVIEW_FIELDS, 'unknown reviewed field')
        require(isinstance(note.get('field_sources', {}), dict) and set(note.get('field_sources', {})) <= set(fields),
                'overlay provenance must name an asserted field')
        require(isinstance(suppressed, list) and len(suppressed) == len(set(suppressed)) and
                set(suppressed) <= SUPPRESSION_FIELDS, 'invalid suppression field')
        require(not set(fields) & set(suppressed), 'review cannot assert a suppressed field')
        record = indexed[key]
        row = coverage.get(key)
        prior = note.get('prior_evidence', {})
        require(isinstance(prior, dict) and set(prior) <= set(suppressed), 'unknown prior evidence field')
        if row is None and suppressed:
            row = {'original_id': key[1], 'profile': key[0], 'task': record.get('mode', 'unknown'),
                   'field_evidence': {}, 'conflicts': [], 'missing_fields': [], 'status': 'incomplete',
                   'required_fields': [], 'match_rule': 'exact-profile-and-original-id'}
            rows.append(row)
            coverage[key] = row
        provenance = {'source': note['source'], 'content_hash': note['source_content_hash'],
                      'observed_at': note['verified_at'], 'confidence': 'verified', 'proof_scope': note['proof_scope']}
        for field in suppressed:
            if field in prior:
                require(isinstance(prior[field], list) and prior[field], 'missing prior evidence')
                row['field_evidence'].setdefault(field, []).extend(copy.deepcopy(prior[field]))
            require(row is not None and field in row['field_evidence'], 'suppression needs original evidence')
            record.pop(field, None)
            record['field_sources'].pop(field, None)
            if field not in row['conflicts']:
                row['conflicts'].append(field)
            row['field_evidence'][field].append({**provenance, 'value': 'unknown', 'reason': note['proof_scope']})
            row['status'] = 'incomplete'
            if field not in row['missing_fields']:
                row['missing_fields'].append(field)
        if suppressed:
            record['suppressed_fields'] = sorted(suppressed)
        for field, value in fields.items():
            record[field] = copy.deepcopy(value)
            source = copy.deepcopy(note.get('field_sources', {}).get(field, provenance))
            record.setdefault('field_sources', {})[field] = source
            if row is not None:
                row['field_evidence'].setdefault(field, []).append({**source, 'value': value})
                if field in row['conflicts']:
                    row['conflicts'].remove(field)
                    row.setdefault('resolved_conflicts', []).append(field)
                if field in row['missing_fields']:
                    row['missing_fields'].remove(field)
                row['status'] = 'incomplete' if row['missing_fields'] or row['conflicts'] else 'complete'


def validate_control(record):
    control = record.get("reasoning_control", "")
    if isinstance(control, str):
        require(control in ("", "none", "unknown"), "known control requires structured evidence")
        return
    require(isinstance(control, dict), "invalid reasoning control")
    require(not set(control) - {"kind", "supported_kinds", "effort_values", "toggle_values", "budget_min",
                              "budget_max", "adaptive", "default"}, "unknown control field")
    kind = control.get("kind")
    require(kind in CONTROL_KINDS, "invalid control kind")
    require(record.get("supports_reasoning") is not False or kind in ("none", "unknown"), "conflicting reasoning")
    if "effort_values" in control or kind == "effort":
        values = control.get("effort_values")
        require(isinstance(values, list) and bool(values) and all(isinstance(v, str) and v.strip() for v in values), "missing effort values")
    if "supported_kinds" in control:
        require(isinstance(control["supported_kinds"], list) and all(v in CONTROL_KINDS for v in control["supported_kinds"]), "invalid supported kinds")
    if "toggle_values" in control:
        require(isinstance(control["toggle_values"], list) and all(type(v) is bool for v in control["toggle_values"]), "invalid toggle values")
    if "adaptive" in control:
        require(type(control["adaptive"]) is bool, "invalid adaptive")
    if kind == "budget" or "budget_min" in control or "budget_max" in control:
        lo, hi = control.get("budget_min"), control.get("budget_max")
        require(type(lo) is int and type(hi) is int and 0 <= lo <= hi <= 2**63 - 1, "invalid reasoning budget")


def public_record(record):
    """Normalize Go's marshaled internal Control plus legacy kind into public JSON."""
    record = copy.deepcopy(record)
    if "control" in record:
        control = record.pop("control")
        require(isinstance(control, dict), "invalid internal control")
        legacy = record.get("reasoning_control")
        require(legacy is None or legacy == control or legacy == control.get("kind"), "conflicting control forms")
        record["reasoning_control"] = control
    validate_control(record)
    return record


def internal_record(record):
    record = public_record(record)
    if isinstance(record.get("reasoning_control"), dict):
        record["control"] = record["reasoning_control"]
        record["reasoning_control"] = record["control"]["kind"]
    return record


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    cap = commands.add_parser("capture", help="extract price-free candidate observations from local files")
    cap.add_argument("--source", nargs=2, action="append", required=True, metavar=("LABEL", "FILE"))
    cap.add_argument("--verified-at", required=True)
    cap.add_argument("--tier", choices=sorted(TIERS), default="local-data")
    cap.add_argument("--output", type=Path, required=True)
    cap.add_argument("--format", choices=("litellm", "openrouter"), default="litellm")
    for name in ("candidate", "validate", "report"):
        sub = commands.add_parser(name)
        sub.add_argument("--inputs", type=Path, default=INPUTS)
        sub.add_argument("--catalog", type=Path, default=CATALOG)
        sub.add_argument("--output-dir", type=Path, default=OUTPUT / "reports")
        sub.add_argument("--scope", type=Path, help="explicit historical scope ledger (optional)")
        sub.add_argument("--registry", type=Path, help="explicit price-free registry evidence (requires --scope)")
        if name == "candidate":
            sub.set_defaults(output_dir=OUTPUT / "candidate")
        if name == "validate":
            sub.add_argument("--check-artifacts", action="store_true")
            sub.add_argument("--require-complete", action="store_true")
    args = parser.parse_args()
    if args.command == "capture":
        if args.format == "openrouter":
            from evidence import capture_registry
            capture_registry(args)
        else:
            capture(args)
        return
    require(bool(args.scope) == bool(args.registry), "--scope and --registry must be supplied together")
    inputs = read(args.inputs)
    require(args.inputs.read_bytes() == encode(inputs), "inputs must use deterministic UTF-8/LF encoding")
    catalog, report, manifest = generate(inputs)
    if args.command == "candidate":
        write(args.output_dir / "catalog.json", catalog)
    else:
        require(digest(encode(inputs)) == read(REVIEWED)["approved_inputs_sha256"],
                "observed inputs are not approved; review required")
        actual = read(args.catalog)
        validate(actual)
        require(actual == catalog, "catalog differs from deterministic candidate; regenerate and review")
        require(args.catalog.read_bytes() == encode(catalog), "catalog byte encoding is not deterministic UTF-8/LF")
    handoff = None
    if args.scope:
        from evidence import generate_handoff
        handoff = generate_handoff(args.scope, args.registry, args.catalog)
    if args.command in ("candidate", "report"):
        write(args.output_dir / "coverage.json", report)
        write(args.output_dir / "manifest.json", manifest)
        if handoff is not None:
            write(args.output_dir / "handoff.json", handoff)
    elif args.check_artifacts:
        for name, value in (("coverage.json", report), ("manifest.json", manifest)):
            require((args.output_dir / name).read_bytes() == encode(value), f"stale {name}")
        from release import validate_release
        validate_release(actual, inputs_path=args.inputs)
        if handoff is not None:
            require((args.output_dir / "handoff.json").read_bytes() == encode(handoff), "stale handoff.json")
    if args.command == "validate" and args.require_complete:
        require(report["summary"]["incomplete"] == 0, "source union has incomplete records (see coverage report)")
        require(report["frozen_u_acceptance"] == "verified", "frozen U acceptance unverified")
    print(json.dumps(report["summary"], sort_keys=True))


if __name__ == "__main__":
    try:
        main()
    except (ValueError, KeyError, TypeError, OSError) as error:
        print(f"catalog validation failed: {error}", file=sys.stderr)
        sys.exit(1)
