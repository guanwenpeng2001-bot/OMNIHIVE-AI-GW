# OmniHive Docker Image

For existing installations, see the [deployment identity migration guide](../docs/operations/identity-migration.md).

OmniHive distributes and manages AI API quotas. Production images include the web UI,
runtime pricing resources and PostgreSQL 18 `pg_dump`/`psql` for backup and restore.

## Quick start with external PostgreSQL and Redis

Create a protected `omnihive.env` file using the application's actual variable names:

```dotenv
AUTO_SETUP=true
SERVER_HOST=0.0.0.0
SERVER_PORT=8080
SERVER_MODE=release
DATABASE_HOST=your-postgres-host
DATABASE_PORT=5432
DATABASE_USER=omnihive
DATABASE_PASSWORD=replace-with-your-database-password
DATABASE_DBNAME=omnihive
DATABASE_SSLMODE=prefer
REDIS_HOST=your-redis-host
REDIS_PORT=6379
REDIS_PASSWORD=
JWT_SECRET=replace-with-a-generated-secret
TOTP_ENCRYPTION_KEY=replace-with-a-generated-64-hex-key
```

Generate each secret separately with `openssl rand -hex 32`, save it in the file and
keep the same values across container replacements and database restores. The TOTP key
protects existing encrypted data; an automatically generated startup key is not durable.
`DATABASE_URL`, `REDIS_URL`, `PORT` and `GIN_MODE` are not the application configuration
contract. `localhost` inside the container refers to that container, not the host.

```bash
chmod 600 omnihive.env
docker run -d \
  --name omnihive \
  --restart unless-stopped \
  --security-opt no-new-privileges:true \
  -p 8080:8080 \
  --env-file omnihive.env \
  -v omnihive_data:/app/data \
  'ghcr.io/guanwenpeng2001-bot/omnihive-ai-gw:latest'
```

For `docker run`, keep `SERVER_PORT` consistent with the container side of `-p`.
For example, publish host port 8081 with `-p 8081:8080` and `SERVER_PORT=8080`.

## Docker Compose

Use Docker Compose 2.24 or newer and the maintained configurations in the repository:

```bash
git clone 'https://github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW.git'
cd OMNIHIVE-AI-GW/deploy
cp .env.example .env
chmod 600 .env
# Edit .env: set POSTGRES_PASSWORD, JWT_SECRET and TOTP_ENCRYPTION_KEY.
docker compose config --quiet
docker compose up -d
docker compose logs -f omnihive
```

The default stack uses PostgreSQL 18 and Redis 8 with named volumes. For local-directory
storage use `-f docker-compose.local.yml`; for external dependencies use
`-f docker-compose.standalone.yml` and configure `DATABASE_HOST`, `DATABASE_PASSWORD`,
`REDIS_HOST` and the remaining database/Redis connection settings in `.env`.
The development variant builds from source and leaves Batch/Vertex features off with
no project or bucket identifiers until the operator configures them.

The one-click `docker-deploy.sh` script downloads the local-directory configuration
**as `docker-compose.yml`**. In that generated directory use `docker compose up -d`,
`docker compose logs -f omnihive` and, for upgrades, `docker compose pull` followed by
`docker compose up -d`. A manual clone retains the original filenames.

The application service reads `.env` via `env_file`; explicit `environment` entries
control internal topology and interpolate the listed tunables. `SERVER_PORT` in the
Compose `.env` is the published host port, while the container always listens on 8080.
The bundled services always use internal PostgreSQL/Redis ports 5432/6379. Arbitrary
application settings such as `LOG_LEVEL` now reach the container from `.env` as well.
A CLI `--env-file` controls interpolation; if using an alternate application env file,
also override the service's `env_file` path. Inspect `docker compose config` locally
(it contains secrets) before deployment.

## Product backups and disaster recovery

1. Preserve the release artifacts, deployment configuration, JWT secret, persistent
   `TOTP_ENCRYPTION_KEY` and backup-store credentials outside the application DB.
   In Admin → Backups, configure an S3-compatible store. For example, use bucket
   `omnihive-dr`, prefix `production` and the bucket's region. For a private S3
   service also enter its application-reachable HTTPS endpoint and enable path-style
   access if required. Test connectivity and create a manual backup. The service
   needs bucket access and object upload, read and delete permissions.
2. Enable a cron schedule explicitly (for example `0 2 * * *` daily in the process
   timezone). Omitting `retain_days` selects 14 days. An explicit zero disables
   age-based deletion. `retain_count` independently limits the number kept.
   Cleanup runs after a scheduled backup and covers completed manual backups too.
   Keep a separate off-host copy and set an independent policy for pre-restore
   snapshot manifests and parts.
3. For bundled Compose, set distinct `RESTORE_JOURNAL_PASSWORD` and
   `RESTORE_ISOLATED_PASSWORD` values in the protected `.env` (generate each with
   `openssl rand -hex 32`). Start the optional dependencies and application together:

   ```bash
   docker compose -f docker-compose.yml -f compose.restore.yml config --quiet
   docker compose -f docker-compose.yml -f compose.restore.yml up -d
   ```

   The overlay adds two private PostgreSQL services with persistent volumes.
   Back up the journal independently. For externally managed PostgreSQL, set
   `OMNIHIVE_RESTORE_JOURNAL_DSN` and `OMNIHIVE_RESTORE_ISOLATED_DSN` on **all**
   gateway, control and worker replicas. Use explicit PostgreSQL URLs with user,
   host, port, database and sslmode. Use TLS verification across hosts. The
   journal may share the production cluster in its own database. Rehearsal needs
   a distinct cluster system identifier from both. Roles need access to
   `pg_control_system()`, journal schema creation or target DDL as applicable.
4. Keep a worker running. Allow private temporary space for uncompressed SQL and
   verification files, plus database space for transactional replacement and checks.
   Enroll the admin in TOTP, rehearse the selected backup, then make a fresh
   production preview and complete session step-up. During cutover the serving
   replicas return HTTP 503. Record the verified operation ID and check balances,
   keys and settings before declaring recovery complete. Follow the
   [operator recovery procedure](../contracts/migrations/restore/INTEGRATION.md#execution-and-review)
   for an interrupted commit.

Product archives, multipart manifests and pre-restore snapshots use S3 object keys.
For disk-backed storage, run a separately managed S3-compatible service with a
persistent volume and replicate its bucket off-host. A local directory on an
application replica cannot provide the shared archive namespace used by workers.

## Application defaults

| Variable | Default | Purpose |
| --- | --- | --- |
| `SERVER_H2C_ENABLED` | `false` | Opt in only when cleartext HTTP/2 is needed. |
| `SERVER_MAX_REQUEST_BODY_SIZE` | `268435456` | Global request cap, 256 MiB. |
| `GATEWAY_MAX_BODY_SIZE` | `268435456` | Gateway request cap, 256 MiB. |
| `GATEWAY_UPSTREAM_RESPONSE_READ_MAX_BYTES` | `134217728` | Non-stream upstream response cap, 128 MiB. |
| `DATABASE_MAX_OPEN_CONNS` / `DATABASE_MAX_IDLE_CONNS` | `32` / `8` | Application PostgreSQL pool ceilings. |
| `REDIS_POOL_SIZE` / `REDIS_MIN_IDLE_CONNS` | `1024` / `128` | Application Redis pool. |
| `REDIS_MAXCLIENTS` | `10000` | Bundled Redis server connection ceiling. |

The bundled PostgreSQL server allows 100 connections. Budget the sum of all application pools,
including gateway, control and worker replicas, plus auxiliary clients, restore sessions, monitoring
and PostgreSQL reserved slots. For example, three processes at 24 open connections each leave
28 slots for other clients and maintenance. Size `POSTGRES_MAX_CONNECTIONS` with the host's
memory budget before increasing any pool. Idle connections count toward the open limit.

## Startup, probes and security

OmniHive applies database migrations on startup. Transient PostgreSQL startup/connection
errors receive bounded retries; bad credentials, checksum mismatches and incompatible
data fail immediately. Compose dependency healthchecks order initial startup; they do
not replace the application's recovery logic after a host restart.

Container `/health` probes check liveness. Route traffic using `/ready`, which checks
dependencies and draining state. Caddy uses readiness; a dependency outage should remove
the instance from traffic without triggering a liveness restart loop.

Security defaults remain `trust_forwarded_ip_for_api_key_acl=true`,
`url_allowlist.enabled=false`, `allow_private_hosts=true`, `allow_insecure_http=true`.
See `deploy/EDGE_SECURITY.md` for their effects, optional restrictions, prompt-audit
private endpoints, and the explicitly configured CORS wildcard behavior.

LinuxDo confidential clients default to `LINUXDO_CONNECT_USE_PKCE=false`; public clients
using `token_auth_method=none` must explicitly enable PKCE. This example does not change
the authentication default.

## Version check, upgrade and rollback

Pin a release tag or digest instead of `:latest` so upgrades and rollbacks are
reproducible:

```bash
# Check the running binary version
docker compose exec omnihive /app/omnihive --version

# Upgrade to a newer pinned tag
docker compose pull
docker compose up -d

# Rollback to the previous pinned tag (edit the image tag in Compose/.env first)
docker compose up -d
```

Image and binary rollback do not revert applied database migrations. Back up
PostgreSQL and `JWT_SECRET`/`TOTP_ENCRYPTION_KEY` before an upgrade, then verify
`/health` (liveness) and `/ready` (dependency readiness/draining) after restart.

## Builds and links

Build from the repository root with `docker build -f Dockerfile .` or
`docker build -f deploy/Dockerfile .`; both carry the same production runtime section.
`Dockerfile.goreleaser` packages a prebuilt release binary. `backend/Dockerfile` is for
backend development only. Published production architectures are `linux/amd64` and
`linux/arm64`; select an available release tag or digest for reproducible deployments.

- [GitHub repository](https://github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW)
- [Deployment guide](https://github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/blob/main/deploy/README.md)
- [Environment template](https://github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/blob/main/deploy/.env.example)
- [Edge security guide](https://github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/blob/main/deploy/EDGE_SECURITY.md)
