# Catalog API fixtures

These fixtures show the v2 success envelope, opaque model IDs, a project perspective and a compare scenario. See the catalog OpenAPI fragment for the request and response schemas.

Call the anonymous route for the public catalog and the selected project's route for the session perspective. Use `catalog_version` for a detail view pinned to a list response. Repeated facet values are combined with OR, while separate facets are combined with AND. A combined action and protocol must match the same capability, and a combined region and retention must match the same endpoint. Price input and TTFT sorts are ascending, and context is descending. Model ID breaks ties.

Use `availability.state` for live status and `availability.publication` for publication status. Public offers use `requires_scope` even when the legacy `availability_state` is `live`. The legacy value classifies the advertised offer. A scoped `live` model has an eligible binding for the requested action and protocol, when specified. Read `billing.state` independently. Reference rates carry descriptive prices and may appear alongside sale rates.

List, detail, and compare share a perspective's `catalog_version`. Scoped responses also carry decimal-string `scope_version`. Project list and detail accept an optional `api_key_id` query parameter. Project compare accepts the same selector in its body.

The fixtures include a known false model fact, an unknown model fact, a measured zero success rate and an unknown TTFT with one sample. A comparison request uses the same visible model IDs in its current scope. Empty benchmarks indicate that no verified benchmark is available.
