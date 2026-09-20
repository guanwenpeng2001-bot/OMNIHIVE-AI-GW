# Offline capability maintenance

Python 3.10+ standard library only for catalog.py: its commands read local files,
never fetch models, contact an account, grant eligibility, publish a release, or
write a trust pin. The separate collect.py command is described below.
Script-relative inputs work from any current directory.

`inputs.json` is the reviewed price-free projection, not a raw registry response.
`reviewed.json` contains explicitly reviewed model specifications, including their
historical evidence references. Those references are provenance strings, not files
opened by this tool. Together they reproduce the published catalog byte for byte.
An unrelated new observation does not refresh the observation date of older records.
Source-union completeness is not account coverage or authorization.

## Candidate preset source projections

`preset-sources.json` stores price-free, reviewed projections of public model
inventories. Each source has a URL, capture hash and observation date; each profile
has a scope, gaps and exact model IDs. Raw pages stay outside the source tree.
Historical namespaces missing from a new registry retain their original observation
dates. Removed candidate IDs do not revoke account discovery or prove retirement.
Lifecycle changes need separate official evidence. Public model names do not invent
private endpoint IDs or establish aliases across hosting providers.

```sh
python -B backend/resources/model-capabilities/scripts/presets.py validate
python -B backend/resources/model-capabilities/scripts/presets.py candidate --output /path/to/candidate.json
```

After reviewing a source projection change, `presets.py candidate` without `--output`
reproduces `backend/internal/service/platform_model_presets.json`. No command fetches
sources or marks a partial inventory complete. The generated revision includes its
scope and model facts. The JSON keeps `task_type` separate from modalities, uses
`embedding` for vector output and `score` for ranking output, and records dimensions,
task limits, retirement dates and typed reasoning controls when evidenced. `speech`
is TTS, `transcription` is STT, and `realtime` describes a real-time task. A documented
lower bound on input modalities is marked `input_modalities_status=partial`.
The source projection retains a full-coverage gap until official scope, region,
plan and lifecycle reconciliation is available; a successful replay is not proof
of full vendor coverage. Additional preset fields require corresponding runtime DTO
support before being exposed by the management API.

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

The independent release manifest binds the reviewed input hash, exact profile/model
applicability, catalog bytes, aliases, review, and actual parser/rule dependencies.
Price bytes are not an applicability condition. Changing prices cannot remove
reviewed specifications; registry values only fill fields absent in that release.
Publishing remains an explicit source review: review the input diff and deployment
identities, advance `reviewed.json`'s generation, reproduce the catalog, review
`release.manifest_for(...)`, and commit the manifest plus independently checked pin.
No candidate, collection, report or validation command performs these steps.
Rollback likewise requires a new reviewed generation; older pins are rejected.

## Three-source collection

`collect.py` invokes `go run ./cmd/capability-collect` in the backend checkout,
reusing `internal/pkg/capabilitysources` for offline fixtures and future read-only
runtime enrichment. This command requires the repository's Go toolchain; the
existing offline catalog commands still use Python's standard library only.

```sh
python backend/resources/model-capabilities/scripts/collect.py --source models.dev --input backend/internal/pkg/capabilitysources/testdata/models.dev.json --verified-at 2026-09-19 --output .pi/candidates/models-dev.json
python backend/resources/model-capabilities/scripts/catalog.py candidate --inputs .pi/candidates/models-dev.json --output-dir .pi/candidates/models-dev
```

The source allowlist is `models.dev`, `openrouter`, `litellm`. Only an explicit
`--network` in place of `--input` fetches the fixed public endpoint. `--dry-run`
writes the proposed action without fetching or registering a schedule. Collection
outputs must be inside this checkout's `.pi`; no scheduler is installed. HTTP is
bounded by 10 seconds and 32 MiB, redirects cannot leave the exact endpoint,
partial pagination and malformed typed fields fail the whole capture, and failure
preserves prior output. ETags are used only when a prior successful capture
returned one; 304 preserves the original observation timestamp.

Source profiles are registry namespaces, never authorization or automatic native
alias evidence. OpenRouter provider-union parameters do not prove native tools or
reasoning controls. Total context limits remain `context_window` plus
`context_limit_kind=total` in candidate inputs, and are not converted into input
limits by the publication generator. False/empty/missing remain distinct, prices
are stripped, and raw observations never enter the published catalog automatically.
The synthetic fixtures test parser contracts; they do not prove current vendor
availability or real-account coverage.

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
