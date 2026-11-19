#!/usr/bin/env python3
import argparse
import json
from pathlib import Path


def lookup(value, path):
    for part in path.split("."):
        value = value[int(part)] if isinstance(value, list) else value[part]
    return value


def substitute(value, scope):
    if isinstance(value, str):
        return value.replace("{scope}", scope)
    return value


def check_step(step, reply, scope):
    def expect(actual, expected, field):
        if type(actual) is not type(expected) or actual != expected:
            raise ValueError(f"unexpected {field}")

    expect(reply["status"], step["status"], "HTTP status")
    expect(reply["body"]["code"], 0, "envelope code")
    for path, expected in step["checks"]:
        expect(lookup(reply["body"]["data"], path), substitute(expected, scope), path)


def main():
    parser = argparse.ArgumentParser(description="Check the six BE-24A responses offline")
    parser.add_argument("--observed", type=Path, help="sanitized JSON responses keyed by J01–J06")
    args = parser.parse_args()
    try:
        fixture = json.loads(Path(__file__).with_name("journey.json").read_text(encoding="utf-8"))
        observed = json.loads(args.observed.read_text(encoding="utf-8")) if args.observed else None
        steps = fixture["steps"]
        expected_ids = [f"J{index:02}" for index in range(1, 7)]
        if [step["id"] for step in steps] != expected_ids:
            raise ValueError("expected J01–J06 in order")
        if args.observed and (not isinstance(observed, dict) or set(observed) != set(expected_ids)):
            raise ValueError("expected exactly one response for each J01–J06")
    except (OSError, ValueError, KeyError, TypeError) as exc:
        print(f"fail journey-input — {type(exc).__name__}; check fixture or capture format")
        return 1

    failed = False
    for step in steps:
        step_id = step["id"]
        if args.observed:
            reply = observed[step_id]
        else:
            reply = {
                "status": step["status"],
                "body": {"code": 0, "data": {
                    key: substitute(value, fixture["scope"]) for key, value in step["data"].items()
                }},
            }
        try:
            check_step(step, reply, fixture["scope"])
        except (IndexError, KeyError, TypeError, ValueError) as exc:
            detail = str(exc) if isinstance(exc, ValueError) else "missing or malformed response field"
            print(f"fail {step_id} — {detail}")
            failed = True
        else:
            source = "sanitized capture" if args.observed else "synthetic response"
            print(f"pass {step_id} — {source} matches assertions")
    return int(failed)


if __name__ == "__main__":
    raise SystemExit(main())
