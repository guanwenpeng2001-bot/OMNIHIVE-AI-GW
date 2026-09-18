# Offline capability maintenance

Python 3.10+ standard library only. Commands read local files; no command fetches
models, contacts an account, grants eligibility, publishes a release, or writes a
trust pin. Script-relative inputs work from any current directory.

`inputs.json` is the reviewed price-free projection, not a raw registry response.
`reviewed.json` retains the existing reviewed decisions unchanged, including their
historical evidence references. Those references are provenance strings, not files
opened by this tool. Together they reproduce the 252-record published catalog
byte for byte. Source-union completeness is not account coverage or authorization.

From the repository root:

```sh
python backend/resources/model-capabilities/scripts/catalog.py candidate
python backend/resources/model-capabilities/scripts/catalog.py report
python backend/resources/model-capabilities/scripts/catalog.py validate --check-artifacts
python -m unittest discover -s backend/resources/model-capabilities/scripts -p 'test_*.py'
```

Outputs default to this checkout's ignored
`.pi/validation/next-development/offline-generation-toolchain/{candidate,reports}`.
Use `--inputs /path/to/inputs.json` and `--output-dir /path/to/results` on any of
candidate/report/validate to keep a run self-contained. Inputs must be canonical
UTF-8/LF JSON; `catalog.encode` defines that encoding. Do not put generated reports,
raw observations, or captures in tracked source directories. The local attributes
keep the reviewed inputs byte-stable in Windows checkouts.

- `candidate` permits unapproved observations, writes candidate catalog/coverage
  and an informational manifest, and retains the reviewed generation.
- `report` requires approved input bytes and an exact match to the existing catalog.
- `validate` checks the same reproducibility without writing files.
- `validate --check-artifacts` additionally requires the generated report files,
  validates the independent release pin, and checks current code/data dependencies.
  Run `report` into the same output directory first. Missing files, stale reports,
  changed hashes and untrusted pins exit nonzero; validation never repairs them.
- `--require-complete` fails while the source union is incomplete or current
  account scope has not been verified.

The imported baseline catalog and pin agree, but its release manifest references
an absent `internal/service/openrouter_capability_provider.go` and older hashes
for `model_capability_catalog.go` and `model_capability_profile.go`. Consequently,
`validate --check-artifacts` currently exits 1 and names these differences. This is
an explicit failed release validation, not a successful release approval. The
subsequent source/release task must review actual dependencies and a newer release
generation; this tool must not silently replace that trust root. Tests exercise
both a fully consistent synthetic release and tampered/missing dependencies.

## Optional historical evidence

Scope/registry evidence is optional and explicit; no historical account inventory
or raw observations are bundled. `report` and `validate --check-artifacts` accept
`--scope /path/to/scope.json --registry /path/to/registry.json` together to generate
and check `handoff.json`. These are the historical ledger formats understood by
`evidence.py`, not the newer contract document's `model-scope.json` interchange
format. The tiny synthetic files in `testdata/` test separate historical/current
denominators and rejected cross-deployment aliases; they are not real coverage.
The one-off longtail batch and its raw observations/reports are not required by
this maintenance chain and are not imported.

Local source capture is available separately:

```sh
python backend/resources/model-capabilities/scripts/catalog.py capture --source fixture /path/to/local.json --verified-at 2026-09-17 --output /path/to/candidate-inputs.json
python backend/resources/model-capabilities/scripts/catalog.py capture --format openrouter --source https://openrouter.ai/api/v1/models?output_modalities=all /path/to/local-page.json --verified-at 2026-09-17 --output /path/to/candidate-evidence.json
```

The URL above is an evidence label, never a network destination. Capture strips
prices; OpenRouter evidence preserves rejected limits and provider-union parameters
without inferring native deployment facts. Capture does not approve its output.
