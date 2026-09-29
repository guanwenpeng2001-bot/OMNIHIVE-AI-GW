# Approved cache base restoration

This field-only default override restores six exact model/field pairs approved on 2026-09-28. Prices are historical project values, not a claim about current provider prices.

Source: `model_prices_and_context_window.json`, repository commit `7a199a58dd5d405828264451dc6a254f3ac7665c`; LF SHA256 `7a13436de2e24ae2f929d51310eea34fa428834ad5462509b58f98ecc16d5ae7`. No provider prefix is stripped to obtain prices.

## Exact source excerpts (USD/token)

These excerpts retain the original exact key and target field/value; unrelated fields are omitted.

```json
{
  "gemini-2.5-pro": {
    "cache_creation_input_token_cost": 1.25e-06
  }
}
```

```json
{
  "gemini-3-pro-preview": {
    "cache_creation_input_token_cost": 2e-06
  }
}
```

```json
{
  "gpt-5.4-pro": {
    "cache_read_input_token_cost": 3e-06
  }
}
```

```json
{
  "gpt-5.4-pro-2026-03-05": {
    "cache_read_input_token_cost": 3e-06
  }
}
```

```json
{
  "gpt-5.5-pro": {
    "cache_read_input_token_cost": 3e-06
  }
}
```

```json
{
  "gpt-5.5-pro-2026-04-23": {
    "cache_read_input_token_cost": 3e-06
  }
}
```

## Deliberately excluded

- `gemini-3.1-pro-preview` and `gemini-3.1-pro-preview-customtools`: the fixed source has native base prices, but the latest decision excludes them from this batch.
- `vertex_ai/gemini-3-pro-preview`, `vertex_ai/gemini-3.1-pro-preview`, `vertex_ai/gemini-3.1-pro-preview-customtools`, and `openrouter/google/gemini-3-pro-preview`: no proven exact historical base creation price. Do not copy native prices.
- Their missing-base warnings remain expected. Unknown prices are not established free prices.
- Above-tier, priority, output, context, tool and lifecycle fields are unchanged. In particular, the runtime Gemini 2.5 Pro above-200k cache creation value is not replaced.

## Loading and operator overrides

The default `pricing.override_file` is `./resources/model-pricing/cache-base-overrides.json`, relative to the server working directory (Docker: `/app`). Both production Dockerfiles copy the resources directory. Remote download bodies and sync hashes remain untouched; the existing field override is reapplied on load/download and on local content-fingerprint changes. The fallback adds only absent models; this override repairs fields on existing ones.

An explicitly configured custom override replaces the default file, including an explicit empty string to disable this layer. It is never silently rewritten or merged. To adopt this patch in a custom file:

1. Back up the existing custom file and configuration.
2. Create a separate preview: add only missing exact model/field pairs; keep equal values and every other field. List differing target values as conflicts, including null (explicit deletion); resolve them against the administrator configuration record before replacement.
3. Review `git diff --no-index -- <backup> <preview>` and the conflict list. Do not report the default patch as effective on a custom deployment without checking its final values.
4. Atomically replace the custom file in the same directory, wait for the configured hash-check interval, and verify final PricingService values and cache billing components.

Rollback: atomically restore the previous override/configuration, allow reload (or restart if the configured path changed), and verify the effective values. Missing cache bases and their warnings can return. Do not delete the downloaded runtime cache, modify its sync hash, or recalculate historical receipts.
