# Routing decision contract

The shared planner accepts frozen request features, an effective policy, and a read-only snapshot. Candidate IDs must be unique and nonempty within a snapshot. Rank starts at one and is null for excluded candidates. The selected candidate is the predicted winner in simulation. Execution updates selection after the scheduler completes live acquisition or returns a wait plan. Unknown quota can be tolerated by policy and is disclosed separately from observed remaining quota. A known exhausted quota excludes the candidate.

Strict region and zero-retention constraints require verified candidate evidence. Exact price ceilings require a frozen amount. Model and endpoint fallback permissions are independent, and every fallback requires authorized funding and passes the same hard constraints. An upstream price estimate or reservation should only be supplied by the authorized snapshot adapter.

Replay requires the same candidate evidence, effective policy, affinity and seed. Candidate traces provide the first failing hard stage and ranking explanation in deterministic order. Candidate labels in scoped stored projections are local to each trace.

See the [routing API fragment](../../../backend/api/openapi/fragments/routing.yaml) and [migration integration](../../migrations/routing/INTEGRATION.md).
