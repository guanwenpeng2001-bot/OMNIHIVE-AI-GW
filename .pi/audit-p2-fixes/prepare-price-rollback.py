"""Offline, fail-closed rollback of append-only price-card changes; never calls an API."""
import argparse
import json
from pathlib import Path


def rollback(before, after, current):
    if not all(isinstance(cards, list) for cards in (before, after, current)):
        raise ValueError('Expected model_pricing arrays')
    if after[:len(before)] != before:
        raise ValueError('Not an append-only operation; manual per-card review required')
    added = after[len(before):]
    result = list(current)
    for card in added:
        if before.count(card) or added.count(card) != 1 or result.count(card) != 1:
            raise ValueError('Target card changed, missing or duplicated; manual review required')
        result.remove(card)
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('before', 'after', 'current', 'output'):
        parser.add_argument('--' + name, type=Path, required=True)
    args = parser.parse_args()
    read = lambda path: json.loads(path.read_text(encoding='utf-8-sig'))
    current = read(args.current)
    proposed = rollback(read(args.before), read(args.after), current)
    # Exclusive creation keeps the reviewed plan from being silently overwritten.
    with args.output.open('x', encoding='utf-8') as stream:
        json.dump({'expected_current': current, 'payload': {'model_pricing': proposed}},
                  stream, ensure_ascii=False, indent=2)


if __name__ == '__main__':
    main()
