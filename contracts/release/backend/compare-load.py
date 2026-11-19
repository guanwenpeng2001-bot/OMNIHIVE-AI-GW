#!/usr/bin/env python3
"""Decide the paired two-hour gate from two full runner reports."""
import argparse
import json
from pathlib import Path


def resources(snapshot):
    return {
        **{key: snapshot[key] for key in ("pg_connections", "pending_receipts", "reserved_budgets", "redis_bytes")},
        **{key: sum(p[key] for p in snapshot["instances"]) for key in ("fds", "rss_kib", "threads")},
    }


def compare(baseline_report, candidate_report, limits):
    env = [r["environment"] for r in (baseline_report, candidate_report)]
    loads = [r["checks"]["LOAD"] for r in (baseline_report, candidate_report)]
    before, after = loads
    checks = {
        "distinct_revisions": env[0]["revision"] != env[1]["revision"],
        "clean_product_trees": all(e.get("product_clean") is True for e in env),
        "same_machine_and_driver": all(env[0].get(k) and env[0][k] == env[1].get(k)
                                       for k in ("machine_hash", "cpu_count", "go", "postgres", "redis", "load_driver_sha256")),
        "paired_7200s": before["schedule"] == after["schedule"] and
                        all(r["duration_s"] == 7200 and r["elapsed_s"] >= 7200 for r in loads),
        "complete_samples": all(r["all"]["n"] == 7200 and r["all"]["errors"] == 0 and
                                r["overhead_samples"] == r["expected_overhead_samples"] == 4800 for r in loads),
        "matched_action_counts": {k: v["n"] for k,v in before["actions"].items()} ==
                                 {k: v["n"] for k,v in after["actions"].items()},
    }
    ms = [r.get("gateway_overhead_p95_ms") for r in loads]
    ratio = ms[1] / ms[0] if all(isinstance(v, (float,int)) and v > 0 for v in ms) else None
    checks["p95_overhead"] = ratio is not None and ratio <= 1.10
    drifts = []
    for run in loads:
        cold, cooled = (resources(run[key]) for key in ("resources_before", "resources_after_cooldown"))
        drifts.append({k: cooled[k] - v for k,v in cold.items()})
    if limits is not None:
        if set(limits) != set(drifts[0]) or any(type(v) is not int or v < 0 for v in limits.values()):
            raise ValueError("limits must specify a nonnegative integer for each resource")
        checks["resource_return"] = all(r["cooldown_s"] == 60 for r in loads) and all(
            drift[key] <= limit for drift in drifts for key,limit in limits.items())
    status = "fail" if not all(checks.values()) else "pending" if limits is None else "pass"
    return dict(status=status, checks=checks, revisions=[e["revision"] for e in env],
                gateway_overhead_p95_ms=ms, ratio=round(ratio,4) if ratio else None, ratio_limit=1.10,
                resource_limits=limits, cooled_minus_preload=drifts,
                resource_gate="operator-calibrated limits required" if limits is None else "evaluated")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("baseline", type=Path)
    parser.add_argument("candidate", type=Path)
    parser.add_argument("--resource-limits", type=Path, help="maximum allowed cooldown deltas per resource")
    args = parser.parse_args()
    try:
        result = compare(json.loads(args.baseline.read_text()), json.loads(args.candidate.read_text()),
                         json.loads(args.resource_limits.read_text()) if args.resource_limits else None)
    except (KeyError, TypeError, ValueError, OSError) as exc:
        result = dict(status="fail", reason=f"invalid or incomplete paired reports ({type(exc).__name__})")
    print(json.dumps(result, indent=2))
    return 0 if result["status"] == "pass" else 1


if __name__ == "__main__":
    raise SystemExit(main())
