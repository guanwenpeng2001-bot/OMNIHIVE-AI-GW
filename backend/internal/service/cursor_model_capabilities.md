# Cursor capability snapshot

Reviewed 2026-09-13. This table is independent of pricing and never creates routes.

Sources:
- https://cursor.com/docs/models-and-pricing.md (Other Models notes explicitly publish up to 1M tokens for the 14 exact IDs in cursorPublishedContextWindows).
- https://cursor.com/docs/models/claude-sonnet-5.md independently confirms Sonnet 5's 1M maximum and thinking support.
- https://cursor.com/help/ai-features/max-mode.md explains that extended context depends on the plan/mode; the stored window is published maximum capacity, not a guaranteed per-request budget.
- internal/pkg/cursor/variant_snapshot.go records AvailableModels field 36 from PR 6289. ModelReasoningLevels extracts only observed effort suffixes, including extra-high -> xhigh. Missing variants do not mean reasoning=false.
- internal/pkg/cursor/models.go identifies the exact picker IDs. Text is the verified common input supported by this gateway's ChatMessage/Run contract. The fallback advertises only text: it does not assert that upstream models lack vision.

Maintenance: verify the official Cursor model page and live picker before adding/changing an exact ID. Do not infer context from family prefixes, pricing thresholds, provider maxima, or Auto routing. Update source URLs and review date with every snapshot change and extend the regression tests. Do not fabricate max output tokens, image/audio support or reasoning defaults: these remain unset without Cursor-specific evidence. There is no wildcard match, including for unknown relay IDs or parameterized variants.

Direct upstream fields take precedence over this fallback where already present; registry enrichment can still fill remaining empty fields. Complete known entries can be persisted through the existing metadata snapshot contract. Unknown context, missing reasoning evidence, `default` and unknown IDs retain the existing incomplete/partial warning; syncing a fully documented model no longer necessarily warns. The table deliberately covers only published evidence; expanding coverage requires further official evidence, not guessing.
