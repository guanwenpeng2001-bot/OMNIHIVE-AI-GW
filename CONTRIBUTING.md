# Contributing to OmniHive

Use Go 1.27.4, Node.js 24 and the pnpm version pinned in `frontend/package.json`. Prepare PostgreSQL and Redis for local development. Copy the [configuration example](deploy/config.example.yaml) into your runtime directory as `config.yaml` and set local connection details. See the [developer guide](DEV_GUIDE.md) for frontend proxy settings, embedded builds and migration conventions.

From the repository root, install dependencies and start the frontend. Run the backend command in a second terminal configured for your local services.

```sh
pnpm --dir frontend install --frozen-lockfile
pnpm --dir frontend dev
(cd backend && go run ./cmd/server)
```

## Tests

Run the checks for the components you change. Use disposable databases for integration tests because the fixtures apply migrations and write data.

```sh
(cd backend && go test -tags=unit ./...)
(cd backend && go test -tags=integration ./...)
pnpm --dir frontend test:run
pnpm --dir frontend typecheck
pnpm --dir frontend lint:check
```

## Submitting a change

1. Fork the repository and branch from the target branch. Use an issue to discuss changes to public APIs or deployment behavior.
2. Keep each commit focused. Use an imperative subject with a scope when helpful, such as `fix(billing): retain the request receipt`. Include the reason for a behavior change in the commit body.
3. Update relevant tests and user documentation. Add a new migration when changing a deployed schema.
4. Open a pull request describing the behavior before and after the change, checks run and any upgrade or rollback considerations. Include screenshots for visible console changes.
5. Address review feedback and check CI results before requesting merge. Report security issues through the [security policy](SECURITY.md).
