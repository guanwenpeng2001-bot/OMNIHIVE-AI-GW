# OmniHive Deployment Files

This directory contains files for deploying OmniHive on Linux servers and Apple-silicon Macs.

## Deployment Methods

| Method | Best For | Setup Wizard |
|--------|----------|--------------|
| **Docker Compose** | Quick setup, all-in-one | Not needed (auto-setup) |
| **Apple container** | Native local stack on macOS 26 | Not needed (auto-setup) |
| **Binary Install** | Production servers, systemd | Web-based wizard |

## Files

| File | Description |
|------|-------------|
| `docker-compose.yml` | Docker Compose configuration (named volumes) |
| `docker-compose.local.yml` | Docker Compose configuration (local directories, easy migration) |
| `docker-deploy.sh` | **One-click Docker deployment script (recommended)** |
| `apple-container.sh` | Native Apple `container` lifecycle script |
| `APPLE_CONTAINER.md` | Apple `container` deployment and operations guide |
| `.env.example` | Container environment variables template |
| `DOCKER.md` | Docker Hub documentation |
| `install.sh` | One-click binary installation script |
| `omnihive.service` | Systemd service unit file |
| `config.example.yaml` | Example configuration file |
| `EDGE_SECURITY.md` | Reverse proxy, CDN/WAF, trusted proxy, and ingress hardening guide |

---

## Apple container Deployment

Apple-silicon Macs running macOS 26 can run the complete OmniHive, PostgreSQL, and Redis stack with Apple `container` 1.1.0 or newer:

```bash
./apple-container.sh init
./apple-container.sh up
./apple-container.sh status
./apple-container.sh logs app -f
```

The script uses Apple named volumes, starts dependencies in order, and performs live readiness checks. The application container supervises the OmniHive process so the Web UI's update-and-restart flow can relaunch an updated binary. It does not provide host-level automatic startup; run `./apple-container.sh up` after a host reboot. Docker Compose remains the recommended production deployment path.

See [APPLE_CONTAINER.md](./APPLE_CONTAINER.md) for configuration, upgrades, persistence, networking behavior, and limitations.

---

## Docker Deployment (Recommended)

### Method 1: One-Click Deployment (Recommended)

Use the automated preparation script for the easiest setup:

```bash
# Download and run the preparation script
curl -fsSL --globoff 'https://raw.githubusercontent.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/v0.2.4/deploy/docker-deploy.sh' | bash

# Or download first, then run
curl -fsSL --globoff 'https://raw.githubusercontent.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/v0.2.4/deploy/docker-deploy.sh' -o docker-deploy.sh
chmod +x docker-deploy.sh
./docker-deploy.sh
```

**What the script does:**
- Downloads `docker-compose.local.yml` as **`docker-compose.yml`**, plus `.env.example`
- Automatically generates secure secrets (JWT_SECRET, TOTP_ENCRYPTION_KEY, POSTGRES_PASSWORD)
- Creates `.env` file with generated secrets
- Creates necessary data directories (data/, postgres_data/, redis_data/)
- **Displays generated credentials** (POSTGRES_PASSWORD, JWT_SECRET, etc.)

The script defaults to release tag `v0.2.4`; set `OMNIHIVE_RELEASE_TAG` to explicitly
select another release. Downloads fail on HTTP errors and are validated in a temporary
directory before being moved into place. If either `docker-compose.yml` or `.env` already
exists, the script exits without overwriting or changing the deployment.

**After running the script:**
```bash
# Start services
docker compose up -d

# View logs
docker compose logs -f omnihive

# If admin password was auto-generated, find it in logs:
docker compose logs omnihive | grep "admin password"

# Access Web UI
# http://localhost:8080
```

Production, local and development Compose publish the application only on
`127.0.0.1` by default. Access it locally at `http://localhost:8080`, or install
the bundled Caddy configuration on the same host and use `https://your-domain`.
Do not set `BIND_HOST=0.0.0.0` to bypass the proxy's TLS. The standalone template
has no bundled reverse proxy and retains its direct binding; if that port is
reachable by other hosts, you must provide your own TLS reverse proxy or load
balancer. Existing `.env` files override Compose defaults, so review `BIND_HOST`
during upgrades rather than assuming an updated template changes a live deployment.

### Method 2: Manual Deployment

If you prefer manual control:

```bash
# Clone repository
git clone 'https://github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW.git'
cd OMNIHIVE-AI-GW/deploy

# Configure environment
cp .env.example .env
chmod 600 .env
nano .env  # Set POSTGRES_PASSWORD and other required variables

# Generate secure secrets (recommended)
JWT_SECRET=$(openssl rand -hex 32)
TOTP_ENCRYPTION_KEY=$(openssl rand -hex 32)
echo "JWT_SECRET=${JWT_SECRET}" >> .env
echo "TOTP_ENCRYPTION_KEY=${TOTP_ENCRYPTION_KEY}" >> .env

# Create data directories
mkdir -p data postgres_data redis_data

# Start all services using local directory version
docker compose -f docker-compose.local.yml up -d

# View logs (find the private admin password file path)
docker compose -f docker-compose.local.yml logs -f omnihive

# Access Web UI
# http://localhost:8080
```

### Deployment Version Comparison

| Version | Data Storage | Migration | Best For |
|---------|-------------|-----------|----------|
| **docker-compose.local.yml** | Local directories (./data, ./postgres_data, ./redis_data) | ✅ Easy (tar entire directory) | Production, need frequent backups/migration |
| **docker-compose.yml** | Named volumes (/var/lib/docker/volumes/) | ⚠️ Requires docker commands | Simple setup, don't need migration |

**Recommendation:** Use the local-directory configuration for easy migration. A manual clone calls it `docker-compose.local.yml`; the preparation script saves the same configuration as `docker-compose.yml`. Use `docker compose` for script installs and `docker compose -f docker-compose.local.yml` for manual local-directory installs, including upgrades, logs, backups and troubleshooting below. The filename alone does not identify the storage type.

### How Auto-Setup Works

When using Docker Compose with `AUTO_SETUP=true`:

1. On first run, the system automatically:
   - Connects to PostgreSQL and Redis
   - Applies database migrations (SQL files in `backend/migrations/*.sql`) and records them in `schema_migrations`
   - Generates JWT secret (if not provided)
   - Creates admin account (password auto-generated if not provided)
   - Writes config.yaml

2. No manual Setup Wizard needed - just configure `.env` and start

3. If `ADMIN_PASSWORD` is not set, check logs for the generated password:
   ```bash
   docker compose logs omnihive | grep "admin password"
   ```

### Startup and Database Recovery

OmniHive applies database migrations during application startup. PostgreSQL can
remain in its recovery/startup phase briefly after a host or Docker daemon
restart. The application retries transient PostgreSQL startup and connection
errors with bounded exponential backoff, then starts automatically when the
database becomes ready. Authentication errors, migration checksum mismatches,
SQL errors, and other permanent configuration or data errors fail immediately.

The Compose example also uses a PostgreSQL health check that verifies both
server readiness and a simple SQL query. `depends_on: condition: service_healthy`
controls dependency ordering for a fresh Compose start, but it is not a
replacement for application-level retries when Docker restores existing
containers after a host restart.

For systemd deployments, keep `Restart=always` and `RestartSec` configured in
`omnihive.service`; the application retry covers transient database startup,
while systemd remains the supervisor for permanent process exits. For
Kubernetes, use a PostgreSQL readiness probe and retain the OmniHive startup
retry behavior; configure the application liveness probe separately so a
database recovery period is not treated as a permanent process failure.

### Database Migration Notes (PostgreSQL)

- Migrations are applied in lexicographic order (e.g. `001_...sql`, `002_...sql`).
- `schema_migrations` tracks applied migrations (filename + checksum).
- Migrations are forward-only; rollback requires a DB backup restore or a manual compensating SQL script.

**Verify `users.allowed_groups` → `user_allowed_groups` backfill**

During the incremental GORM→Ent migration, `users.allowed_groups` (legacy `BIGINT[]`) is being replaced by a normalized join table `user_allowed_groups(user_id, group_id)`.

Run this query to compare the legacy data vs the join table:

```sql
WITH old_pairs AS (
  SELECT DISTINCT u.id AS user_id, x.group_id
  FROM users u
  CROSS JOIN LATERAL unnest(u.allowed_groups) AS x(group_id)
  WHERE u.allowed_groups IS NOT NULL
)
SELECT
  (SELECT COUNT(*) FROM old_pairs)           AS old_pair_count,
  (SELECT COUNT(*) FROM user_allowed_groups) AS new_pair_count;
```

### Commands

For **local directory version** (docker-compose.local.yml):

```bash
# Start services
docker compose -f docker-compose.local.yml up -d

# Stop services
docker compose -f docker-compose.local.yml down

# View logs
docker compose -f docker-compose.local.yml logs -f omnihive

# Restart OmniHive only
docker compose -f docker-compose.local.yml restart omnihive

# Update to latest version
docker compose -f docker-compose.local.yml pull
docker compose -f docker-compose.local.yml up -d

# Remove all data (caution!)
docker compose -f docker-compose.local.yml down
rm -rf data/ postgres_data/ redis_data/
```

For **script installs**, or the **named-volumes file from a manual clone** (`docker-compose.yml`):

```bash
# Start services
docker compose up -d

# Stop services
docker compose down

# View logs
docker compose logs -f omnihive

# Restart OmniHive only
docker compose restart omnihive

# Update to latest version
docker compose pull
docker compose up -d

# Remove all data (caution!)
docker compose down -v
```

### Version Check, Upgrade and Rollback (Docker)

Pin the image tag (for example `:v0.2.23` or `:0.2`) instead of `:latest` so an
upgrade or rollback is reproducible:

```bash
# Check the running binary version
docker compose exec omnihive /app/omnihive --version

# Health check (liveness) and dependency readiness
curl -fsS http://localhost:8080/health
curl -fsS http://localhost:8080/ready

# Upgrade: move the tag forward
docker compose pull
docker compose up -d

# Rollback: point the Compose image back to the previous tag
# (edit the image tag in docker-compose.yml / .env, then:)
docker compose up -d
```

Container images do not roll back database migrations, so back up PostgreSQL and
the persisted keys before an upgrade, then verify `/health` and `/ready` after
the restart.

Before an upgrade, restart or shutdown, remove the instance from the load balancer
and wait for active connection counts to reach zero. The application makes `/ready`
fail as soon as draining starts, so Caddy can stop assigning new requests. Only then
run `docker compose stop omnihive` (or `down`); do not stop PostgreSQL or Redis first.
The application's shutdown budget is 20 seconds (`backend/cmd/server/main.go`), and
each Compose template sets `stop_grace_period: 30s` to include 10 seconds of cleanup
headroom. If `/ready` fails because dependencies are unavailable, restore dependencies
before draining so in-flight usage receipts can be persisted.

### Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `POSTGRES_PASSWORD` | **Yes** | - | PostgreSQL password |
| `JWT_SECRET` | **Recommended** | *(auto-generated)* | JWT secret (fixed for persistent sessions) |
| `TOTP_ENCRYPTION_KEY` | **Recommended** | *(auto-generated)* | TOTP encryption key (fixed for persistent 2FA) |
| `BIND_HOST` | No | `127.0.0.1` | Host address for the Compose port; keep loopback behind Caddy. Standalone defaults to `0.0.0.0` and needs operator-provided TLS if exposed. |
| `SERVER_PORT` | No | `8080` | Published host port in Compose; application always listens on container port 8080 |
| `ADMIN_EMAIL` | No | `admin@omnihive.local` | Admin email |
| `ADMIN_PASSWORD` | No | *(auto-generated)* | Admin password |
| `TZ` | No | `Asia/Shanghai` | Timezone |
| `UPDATE_GITHUB_TOKEN` | No | *(empty)* | Token for `api.github.com` release checks only; asset downloads remain anonymous. |
| `GEMINI_OAUTH_CLIENT_ID` | No | *(builtin)* | Google OAuth client ID (Gemini OAuth). Leave empty to use the built-in Gemini CLI client. |
| `GEMINI_OAUTH_CLIENT_SECRET` | No | *(builtin)* | Google OAuth client secret (Gemini OAuth). Leave empty to use the built-in Gemini CLI client. |
| `GEMINI_OAUTH_SCOPES` | No | *(default)* | OAuth scopes (Gemini OAuth) |
| `GEMINI_QUOTA_POLICY` | No | *(empty)* | JSON overrides for Gemini local quota simulation (Code Assist only). |

See `.env.example` for all available options. Docker Compose 2.24 or newer is required.

All four Compose variants load the application's `.env` through `env_file` (optional
when settings are supplied explicitly). Compose's `.env` interpolation and container
environment injection are separate: `--env-file /path/custom.env` supplies interpolation
values but does not replace the service's `.env` file. Keep the application settings in
`deploy/.env`, or override the service `env_file` when using a different file. Explicit
`environment` entries take precedence: internal SERVER_HOST/PORT and bundled DB/Redis
addresses stay fixed; the listed tunables use shell/Compose interpolation. LOG_*, CORS_*,
LinuxDo/OIDC and other backend options in `.env` now reach the application as well.
`REDIS_MAXCLIENTS` configures the Redis server command, not the application connection pool.

Default application pools match the backend: PostgreSQL open/idle 32/8 and Redis
pool/min-idle 1024/128. The bundled PostgreSQL stock `max_connections=100` is a separate
server ceiling: budget all replicas and maintenance clients before increasing load,
and explicitly raise that ceiling or reduce application pools to suit your resources.
H2C defaults to false; enable it explicitly only for a cleartext HTTP/2 client/proxy.
`GATEWAY_UPSTREAM_RESPONSE_READ_MAX_BYTES=134217728` is the 128 MiB non-stream response
limit, separate from the 256 MiB global/gateway request limits and 8 MiB model-list limit.

Set `TOTP_ENCRYPTION_KEY` once to 64 hex characters (`openssl rand -hex 32`) and preserve
it together with the database and `.env` on rebuilds, upgrades and restores. All four
Compose variants, including standalone, inject the same value. A data volume alone does
not persist an automatically generated TOTP key; losing or changing it prevents decryption
of existing 2FA and other data encrypted with that key. Keep `JWT_SECRET` fixed for sessions.

Application container healthchecks use `/health` for liveness. Caddy uses `/ready` to stop
sending new traffic when dependencies are unavailable or the process is draining; do not
use dependency readiness as a restart trigger. See [EDGE_SECURITY.md](./EDGE_SECURITY.md)
for the unchanged security defaults, forwarded-header trust and outbound endpoint risks.
The TLS site in `Caddyfile` sets one-year HSTS with `includeSubDomains`, without `preload`.
Enable it only after every subdomain supports HTTPS; the application retains its other
security headers, so verify the final HTTPS response after replacing `api.example.com`.

For a source production image use `docker build -f Dockerfile .` from the repository root;
`deploy/Dockerfile` carries the same runtime section, including `pg_dump` and `psql` from
PostgreSQL 18, pricing resources and liveness. `Dockerfile.goreleaser` packages the release
binary with the same PG tools. `backend/Dockerfile` is a development-only backend build.

> **Note:** The `docker-deploy.sh` script automatically generates `JWT_SECRET`, `TOTP_ENCRYPTION_KEY`, and `POSTGRES_PASSWORD` for you.

### Easy Migration (Local Directory Version)

When using `docker-compose.local.yml`, all data is stored in local directories, making migration simple:

```bash
# On source server: Stop services and create archive
cd /path/to/deployment
docker compose -f docker-compose.local.yml down
cd ..
tar czf omnihive-complete.tar.gz deployment/

# Transfer to new server
scp omnihive-complete.tar.gz user@new-server:/path/to/destination/

# On new server: Extract and start
tar xzf omnihive-complete.tar.gz
cd deployment/
docker compose -f docker-compose.local.yml up -d
```

Your entire deployment (configuration + data) is migrated!

---

## Gemini OAuth Configuration

OmniHive supports three methods to connect to Gemini:

### Method 1: Code Assist OAuth (Recommended for GCP Users)

**No configuration needed** - always uses the built-in Gemini CLI OAuth client (public).

1. Leave `GEMINI_OAUTH_CLIENT_ID` and `GEMINI_OAUTH_CLIENT_SECRET` empty
2. In the Admin UI, create a Gemini OAuth account and select **"Code Assist"** type
3. Complete the OAuth flow in your browser

> Note: Even if you configure `GEMINI_OAUTH_CLIENT_ID` / `GEMINI_OAUTH_CLIENT_SECRET` for AI Studio OAuth,
> Code Assist OAuth will still use the built-in Gemini CLI client.

**Requirements:**
- Google account with access to Google Cloud Platform
- A GCP project (auto-detected or manually specified)

**How to get Project ID (if auto-detection fails):**
1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Click the project dropdown at the top of the page
3. Copy the Project ID (not the project name) from the list
4. Common formats: `my-project-123456` or `cloud-ai-companion-xxxxx`

### Method 2: AI Studio OAuth (For Regular Google Accounts)

Requires your own OAuth client credentials.

**Step 1: Create OAuth Client in Google Cloud Console**

1. Go to [Google Cloud Console - Credentials](https://console.cloud.google.com/apis/credentials)
2. Create a new project or select an existing one
3. **Enable the Generative Language API:**
   - Go to "APIs & Services" → "Library"
   - Search for "Generative Language API"
   - Click "Enable"
4. **Configure OAuth Consent Screen** (if not done):
   - Go to "APIs & Services" → "OAuth consent screen"
   - Choose "External" user type
   - Fill in app name, user support email, developer contact
   - Add scopes: `https://www.googleapis.com/auth/generative-language.retriever` (and optionally `https://www.googleapis.com/auth/cloud-platform`)
   - Add test users (your Google account email)
5. **Create OAuth 2.0 credentials:**
   - Go to "APIs & Services" → "Credentials"
   - Click "Create Credentials" → "OAuth client ID"
   - Application type: **Web application** (or **Desktop app**)
   - Name: e.g., "OmniHive Gemini"
   - Authorized redirect URIs: Add `http://localhost:1455/auth/callback`
6. Copy the **Client ID** and **Client Secret**
7. **⚠️ Publish to Production (IMPORTANT):**
   - Go to "APIs & Services" → "OAuth consent screen"
   - Click "PUBLISH APP" to move from Testing to Production
   - **Testing mode limitations:**
     - Only manually added test users can authenticate (max 100 users)
     - Refresh tokens expire after 7 days
     - Users must be re-added periodically
   - **Production mode:** Any Google user can authenticate, tokens don't expire
   - Note: For sensitive scopes, Google may require verification (demo video, privacy policy)

**Step 2: Configure Environment Variables**

```bash
GEMINI_OAUTH_CLIENT_ID=your-client-id.apps.googleusercontent.com
GEMINI_OAUTH_CLIENT_SECRET=GOCSPX-your-client-secret

# 可选：如需使用 Gemini CLI 内置 OAuth Client（Code Assist / Google One）
# 安全说明：本仓库不会内置该 client_secret，请在运行环境通过环境变量注入。
# GEMINI_CLI_OAUTH_CLIENT_SECRET=GOCSPX-your-built-in-secret
```

**Step 3: Create Account in Admin UI**

1. Create a Gemini OAuth account and select **"AI Studio"** type
2. Complete the OAuth flow
   - After consent, your browser will be redirected to `http://localhost:1455/auth/callback?code=...&state=...`
   - Copy the full callback URL (recommended) or just the `code` and paste it back into the Admin UI

### Method 3: API Key (Simplest)

1. Go to [Google AI Studio](https://aistudio.google.com/app/apikey)
2. Click "Create API key"
3. In Admin UI, create a Gemini **API Key** account
4. Paste your API key (starts with `AIza...`)

### Comparison Table

| Feature | Code Assist OAuth | AI Studio OAuth | API Key |
|---------|-------------------|-----------------|---------|
| Setup Complexity | Easy (no config) | Medium (OAuth client) | Easy |
| GCP Project Required | Yes | No | No |
| Custom OAuth Client | No (built-in) | Yes (required) | N/A |
| Rate Limits | GCP quota | Standard | Standard |
| Best For | GCP developers | Regular users needing OAuth | Quick testing |

---

## Binary Installation

For production servers using systemd.

### One-Line Installation

```bash
curl -sSL --globoff 'https://raw.githubusercontent.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/main/deploy/install.sh' | sudo bash
```

### Manual Installation

1. Download the latest release from [GitHub Releases](https://github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/releases)
2. Extract and copy the binary to `/opt/omnihive/`
3. Copy `omnihive.service` to `/etc/systemd/system/`
4. Run:
   ```bash
   sudo systemctl daemon-reload
   sudo systemctl enable omnihive
   sudo systemctl start omnihive
   ```
5. Open the Setup Wizard in your browser to complete configuration

### Commands

```bash
# Install
sudo ./install.sh

# Upgrade
sudo ./install.sh upgrade

# Version check
sudo /opt/omnihive/omnihive --version

# List available release versions
sudo ./install.sh list-versions

# Rollback to a specific release version
sudo ./install.sh rollback v0.2.23

# Uninstall
sudo ./install.sh uninstall
```

### Service Management

```bash
# Start the service
sudo systemctl start omnihive

# Stop the service
sudo systemctl stop omnihive

# Restart the service
sudo systemctl restart omnihive

# Check status
sudo systemctl status omnihive

# View logs
sudo journalctl -u omnihive -f

# Enable auto-start on boot
sudo systemctl enable omnihive
```

### Configuration

#### Server Address and Port

During installation, you will be prompted to configure the server listen address and port. These settings are stored in the systemd service file as environment variables.

To change after installation:

1. Edit the systemd service:
   ```bash
   sudo systemctl edit omnihive
   ```

2. Add or modify:
   ```ini
   [Service]
   Environment=SERVER_HOST=0.0.0.0
   Environment=SERVER_PORT=3000
   ```

3. Reload and restart:
   ```bash
   sudo systemctl daemon-reload
   sudo systemctl restart omnihive
   ```

#### Gemini OAuth Configuration

If you need to use AI Studio OAuth for Gemini accounts, add the OAuth client credentials to the systemd service file:

1. Edit the service file:
   ```bash
   sudo nano /etc/systemd/system/omnihive.service
   ```

2. Add your OAuth credentials in the `[Service]` section (after the existing `Environment=` lines):
   ```ini
   Environment=GEMINI_OAUTH_CLIENT_ID=your-client-id.apps.googleusercontent.com
   Environment=GEMINI_OAUTH_CLIENT_SECRET=GOCSPX-your-client-secret
   ```

   如需使用“内置 Gemini CLI OAuth Client”（Code Assist / Google One），还需要注入：
   ```ini
   Environment=GEMINI_CLI_OAUTH_CLIENT_SECRET=GOCSPX-your-built-in-secret
   ```

3. Reload and restart:
   ```bash
   sudo systemctl daemon-reload
   sudo systemctl restart omnihive
   ```

> **Note:** Code Assist OAuth does not require any configuration - it uses the built-in Gemini CLI client.
> See the [Gemini OAuth Configuration](#gemini-oauth-configuration) section above for detailed setup instructions.

#### Application Configuration

The setup wizard writes `/etc/omnihive/config.yaml`. The native service keeps application data in `/opt/omnihive/data`. See [the identity migration guide](../docs/operations/identity-migration.md) before switching an existing installation.

### Prerequisites

- Linux server (Ubuntu 20.04+, Debian 11+, CentOS 8+, etc.)
- PostgreSQL 14+
- Redis 6+
- systemd

### Directory Structure

```
/opt/omnihive/
├── omnihive              # Main binary
├── omnihive.backup       # Backup (after upgrade)
└── data/                # Runtime data

/etc/omnihive/
└── config.yaml          # Configuration file
```

---

## Troubleshooting

### Docker

For **local directory version**:

```bash
# Check container status
docker compose -f docker-compose.local.yml ps

# View detailed logs
docker compose -f docker-compose.local.yml logs --tail=100 omnihive

# Check database connection
docker compose -f docker-compose.local.yml exec postgres pg_isready

# Check Redis connection
docker compose -f docker-compose.local.yml exec redis redis-cli ping

# Restart all services
docker compose -f docker-compose.local.yml restart

# Check data directories
ls -la data/ postgres_data/ redis_data/
```

For **named volumes version**:

```bash
# Check container status
docker compose ps

# View detailed logs
docker compose logs --tail=100 omnihive

# Check database connection
docker compose exec postgres pg_isready

# Check Redis connection
docker compose exec redis redis-cli ping

# Restart all services
docker compose restart
```

### Binary Install

```bash
# Check service status
sudo systemctl status omnihive

# View recent logs
sudo journalctl -u omnihive -n 50

# Check config file
sudo cat /etc/omnihive/config.yaml

# Check PostgreSQL
sudo systemctl status postgresql

# Check Redis
sudo systemctl status redis
```

### Common Issues

1. **Port already in use**: Change `SERVER_PORT` in `.env` or systemd config
2. **Database connection failed**: Check PostgreSQL is running and credentials are correct
3. **Redis connection failed**: Check Redis is running and password is correct
4. **Permission denied**: Ensure proper file ownership for binary install

---

## TLS Fingerprint Configuration

OmniHive supports TLS fingerprint simulation to make requests appear as if they come from the official Claude CLI (Node.js client).

> Compare TLS fingerprints using diagnostics under your control before changing a production transport profile.

### Default Behavior

- Built-in `claude_cli_v2` profile simulates Node.js 20.x + OpenSSL 3.x
- JA3 Hash: `1a28e69016765d92e3b381168d68922c`
- JA4: `t13d5911h1_a33745022dd6_1f22a2ca17c4`
- Profile selection: `accountID % profileCount`

### Configuration

```yaml
gateway:
  tls_fingerprint:
    enabled: true  # Global switch
    profiles:
      # Simple profile (uses default cipher suites)
      profile_1:
        name: "Profile 1"

      # Profile with custom cipher suites (use compact array format)
      profile_2:
        name: "Profile 2"
        cipher_suites: [4866, 4867, 4865, 49199, 49195, 49200, 49196]
        curves: [29, 23, 24]
        point_formats: 0

      # Another custom profile
      profile_3:
        name: "Profile 3"
        cipher_suites: [4865, 4866, 4867, 49199, 49200]
        curves: [29, 23, 24, 25]
```

### Profile Fields

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Display name (required) |
| `cipher_suites` | []uint16 | Cipher suites in decimal. Empty = default |
| `curves` | []uint16 | Elliptic curves in decimal. Empty = default |
| `point_formats` | []uint8 | EC point formats. Empty = default |

### Common Values Reference

**Cipher Suites (TLS 1.3):** `4865` (AES_128_GCM), `4866` (AES_256_GCM), `4867` (CHACHA20)

**Cipher Suites (TLS 1.2):** `49195`, `49196`, `49199`, `49200` (ECDHE variants)

**Curves:** `29` (X25519), `23` (P-256), `24` (P-384), `25` (P-521)

### Product backups and disaster recovery

Configure a persistent TOTP encryption key and an S3-compatible bucket in the backup administration page. Enable a schedule after testing a manual backup. Production restore also requires a dedicated journal database, a rehearsal database on a separate PostgreSQL cluster and an administrator session with fresh TOTP step-up. The optional Compose overlay, retention settings and cutover procedure are in the [Docker deployment guide](DOCKER.md#product-backups-and-disaster-recovery).
