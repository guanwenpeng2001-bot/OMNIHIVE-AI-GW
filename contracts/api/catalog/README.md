# Catalog API fixtures

These fixtures show the v2 success envelope, opaque model IDs, a project perspective and a compare scenario. See the catalog OpenAPI fragment for the request and response schemas.

Call the anonymous route for the public catalog and the selected project's route for the session perspective. Use `catalog_version` for a detail view pinned to a list response. Repeated facet values are combined with OR, while separate facets are combined with AND. A combined action and protocol must match the same capability, and a combined region and retention must match the same endpoint. Price input and TTFT sorts are ascending, and context is descending. Model ID breaks ties.

`availability_state` is `live` when the public projection can reach at least one eligible live binding and `catalog_only` when the model is listed from catalog metadata but no live binding is currently eligible. The field never grants routing or pricing access: `catalog_only` entries keep `reference_price` metadata and still require an authorized binding before they can serve traffic. A missing `availability_state` is treated as unknown, not as either value.

The fixtures include a known false model fact, an unknown model fact, a measured zero success rate and an unknown TTFT with one sample. A comparison request uses the same visible model IDs in its current scope. Empty benchmarks indicate that no verified benchmark is available.
