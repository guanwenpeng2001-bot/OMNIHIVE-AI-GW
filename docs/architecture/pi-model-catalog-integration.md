# Built-in model catalog and pi-ai supplement

OmniHive uses exact provider and model IDs for built-in capability facts. The reviewed release comes first. `catalog.json`, `platform_model_presets.json`, `release-manifest.json`, and `release-pin.json` define the approved data and its trust chain. The registry verifies the catalog and presets before publishing them. The public catalog's built-in index also accepts OmniHive's custom entries in `supplementalBuiltinModels` for keys absent from the reviewed capability catalog.

The generated `pi-supplement.json` supplies facts for remaining exact keys. The generator excludes reviewed preset keys, and the Go index gives the reviewed capability records and custom entries precedence over every field in a dependency record. This preserves the reviewed context limits, `suppressed_fields`, and modalities including audio, video, PDF, embeddings, and non-text outputs. Gateway actions such as images, speech and transcriptions continue to use the OmniHive records and their existing adapters.

The supplement can fill in display name, context window, output limit, reasoning, vision, modalities, and method information for a model already in the catalog view. API adapter support, credential discovery, and access checks still determine route availability. Admin candidate IDs and the approved capability snapshot come from the reviewed registry. The package lock pins the dependency version and tarball integrity, and the generated files are bundled at build time. Runtime catalog lookup uses the compiled JSON.

## Provider and protocol mapping

The generator maps `amazon-bedrock` to `bedrock`, `google` to `gemini`, `google-vertex` to `vertex_ai`, `moonshotai` to `moonshot`, and `kimi-coding` to `kimi-for-coding`. It maps `openai-codex` to `openai` and `opencode` to `opencode-go`, giving the native OpenAI and OpenCode Go entries precedence at duplicate exact keys. The other provider and profile names are carried through, including the independent OpenRouter, Vercel AI Gateway, Cloudflare AI Gateway, and GitHub Copilot namespaces. An upstream provider or protocol without an explicit mapping stops generation for review.

| Upstream API | Mode | Methods |
| --- | --- | --- |
| OpenAI Completions, Mistral Conversations | chat | `chat_completions` |
| OpenAI Responses | responses | `responses`, plus `chat_completions` for native OpenAI models that support both |
| Azure OpenAI Responses, Codex Responses | responses | `responses` |
| Anthropic Messages | chat | `messages` |
| Google Generative AI, Google Vertex | chat | `generateContent` |
| Bedrock Converse Stream | chat | `converse` |

The capability dataset uses pi-ai's total context window, output token limit, input modalities, and textual output. Pricing fields live in a separate generated file.

## Token prices

`pi-prices-supplement.json` contains reference prices in USD per token. The generator shifts each decimal by six places and writes JSON numbers for input, output, cache reads, and cache writes. It excludes all-zero and negative sentinel tariffs and records the upstream context window as the reference input upper bound alongside the output limit. Price lookup uses a provider-qualified key for each supported tariff and an unqualified model ID where a single unscoped provider supplies that ID. Aggregator and hosted gateway tariffs use their qualified keys.

The catalog detail projects the provider-qualified pi-ai token rates under `reference_price` when the model is unpriced. Billing, budget admission and quotations use the active pricing dataset.

## Regeneration

Use Node.js 24 and npm to regenerate the locked version from the repository root.

```sh
npm --prefix tools/pi-catalog ci --ignore-scripts --no-audit --no-fund
node scripts/sync-pi-catalog.mjs
(cd backend && go test ./internal/modules/catalog ./resources/model-capabilities ./resources/model-pricing)
```

To update the dependency, replace the npm CI command with the following command, then regenerate and run the tests above.

```sh
npm --prefix tools/pi-catalog install --save-exact @mariozechner/pi-ai@latest --ignore-scripts --no-audit --no-fund
```

The weekly **Sync pi-ai model catalog** workflow also runs on demand. It installs the latest dependency, generates both files, verifies the catalog and pricing contracts, and publishes a reviewable patch artifact when the lockfile or data changes. Apply the reviewed patch to update the pinned dependency and generated files together.

The npm registry deprecates `@mariozechner/pi-ai@0.73.1` and names `@earendil-works/pi-ai` as its successor. The workflow checks updates to the configured `@mariozechner/pi-ai` package.
