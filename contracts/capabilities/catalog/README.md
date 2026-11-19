# Capability evidence

| Source | Model facts | Endpoint facts | Publication |
| --- | --- | --- | --- |
| Approved registry | Model identity, documented limits and optional capability values | Methods checked against the gateway adapter registry | Public after the existing plaza independently lists the model |
| Legacy discovery | Identity-bound native facts and administrator declarations | Gateway model-to-endpoint binding | Project scoped |
| Connection verification | Model IDs returned by successful discovery | Time-bounded authorization and discovery at the qualified connection | Project scoped |
| Account configuration | No model or vendor facts | Configured Bedrock region and confirmed training preference where available | Project scoped |

An unknown numeric or Boolean fact is `null`, while explicit false and zero retain their values. Model and endpoint facts have separate provenance. Redis, connection reachability, model access and price are independent sources. A route candidate can expire, become unavailable, or require new authorization before dispatch. The public rating and price fields depend on approved evidence. Private observations remain scoped even when the model name itself is public.
