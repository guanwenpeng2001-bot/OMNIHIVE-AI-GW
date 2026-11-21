# Gateway identity compatibility

The regular and Google API-key middleware both call the identity access and funding evaluators with `identitycompat.KeyFacts` and `identitycompat.Subscriptions`. The adapter translates existing key, user, group and subscription projections into the identity decision and preserves subscription maintenance and limit errors. The restricted credential path calls `ProjectKey` to hydrate the existing gateway key from its stable database identity. It removes login secrets from the request projection and restores the authorized group list and current group RPM override.

Ingress continues using the existing credential digest resolver, Redis/L1 auth cache, key touch, model access snapshots, IP rules, protocol errors and abuse limiter. Current project membership and credential revocation are read in PostgreSQL. Console requests use the existing JWT/session entry point, which also serves passkey and TOTP sign-ins.

runtime moves residual service callers. retirement retires the compatibility layer after old authentication and billing projections leave service. Rotation and existing custom credentials retain their original digest namespace throughout rollout.
