#!/usr/bin/env python3
"""Build release compatibility projections from the one versioned authority."""
import argparse
import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SOURCE = ROOT / 'backend/internal/config/release_compatibility.json'
INSTALLER = ROOT / 'deploy/install.sh'
FLOOR_ASSIGNMENT = re.compile(r'^MINIMUM_ROLLBACK_VERSION="[^"]+"$', re.MULTILINE)


def stable_version(value):
    if not isinstance(value, str) or not re.fullmatch(r'(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)', value):
        raise ValueError('Rollback floors must be stable semantic versions')
    return tuple(map(int, value.split('.')))


def compatibility():
    values = json.loads(SOURCE.read_text())
    schema = values['schema_rollback_floor']
    semantic = values['semantic_rollback_floor']
    floor = max((schema, semantic), key=stable_version)
    return {**values, 'minimum_rollback_version': floor}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--manifest', nargs=3, metavar=('VERSION', 'COMMIT', 'OUTPUT'))
    parser.add_argument('--sync-installer', action='store_true')
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    values = compatibility()
    assignment = f'MINIMUM_ROLLBACK_VERSION="{values["minimum_rollback_version"]}"'
    installer = INSTALLER.read_text()
    if len(FLOOR_ASSIGNMENT.findall(installer)) != 1:
        raise SystemExit('Installer must contain exactly one generated compatibility projection')
    if args.sync_installer:
        INSTALLER.write_text(FLOOR_ASSIGNMENT.sub(assignment, installer))
    if args.check and FLOOR_ASSIGNMENT.search(INSTALLER.read_text()).group() != assignment:
        raise SystemExit('Installer floor drifted: run scripts/release_compatibility.py --sync-installer')
    if args.manifest:
        version, commit, output = args.manifest
        # GoReleaser accepts prereleases. A prerelease of the rollback floor is
        # deliberately below it, so it cannot become a stable rollback target.
        normalized = version.removeprefix('v')
        if not re.fullmatch(r'\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?', normalized):
            raise SystemExit('Invalid release version')
        target = Path(output)
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(json.dumps({'product': 'omnihive', 'version': normalized, **values, 'commit': commit}, separators=(',', ':')) + '\n')


if __name__ == '__main__':
    main()
