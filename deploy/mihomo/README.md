# Portable per-node AnyTLS proxy

This optional deployment adds the official Mihomo core as `node-proxy` on the
existing backend's `sub2api-network`. Each real AnyTLS node gets its own HTTP
listener (including HTTP CONNECT), wired directly to that outbound. The runtime
does not require Python, Windows, Clash Verge, TUN, a desktop session, or a system
proxy. The normal `deploy/docker-compose.yml` remains unchanged.

## Render privately

Requires Python 3.9+ and PyYAML. Install into a dedicated virtual environment,
not the shared application environment. For offline machines, download the
requirements wheel ahead of time and install with `--no-index --find-links`.

```sh
python3 -m venv /srv/node-proxy-tools
/srv/node-proxy-tools/bin/pip install -r deploy/mihomo/requirements.txt
/srv/node-proxy-tools/bin/python deploy/mihomo/render.py \
  --input /srv/private/subscription.yaml \
  --output-dir /srv/private/node-proxy
```

Alternatively, supply the official provider's HTTPS subscription URL through a
secret-injected environment variable, and pass only its **name** on the command
line:

```sh
/srv/node-proxy-tools/bin/python deploy/mihomo/render.py \
  --subscription-url-env YTOO_SUBSCRIPTION_URL \
  --output-dir /srv/private/node-proxy
```

The helper fetches directly, ignores inherited HTTP proxies, refuses redirects,
and never uses a converter. Give it the provider's final Clash YAML URL; HTML,
base64 URI lists, proxy providers and non-AnyTLS protocols are not supported.
Only proxy entries are copied; subscription rules, scripts, DNS, controllers and
other top-level configuration are discarded. Administrative labels such as
到期时间、距离下次重置、获取最新订阅、剩余流量 and `remaining` are excluded by anchored
patterns. Unfamiliar real node names are retained. Unsupported outbound fields
fail with a redacted error so they can be reviewed privately without silently
changing routing or TLS semantics. TLS certificate verification options are
preserved from input; review any `skip-cert-verify: true` in the private source.

Outputs must be outside this checkout. The output directory is mode 0700 and
files mode 0600 where supported; on Windows set an equivalent private ACL.
`config.yaml` contains upstream credentials. Keep it, the input, and backups
private. A generated `.gitignore` excludes every output. Diagnostics do not
print URLs, YAML excerpts, credentials, or node names.

| Output | Purpose |
| --- | --- |
| `config.yaml` | Secret runtime config with one HTTP listener per node |
| `manifest.json` | Names, hashed identities, ports and active/retired state; no endpoint credentials |
| `server.create.json` | Array of create request bodies using `node-proxy` |
| `development.create.json` | Same ports using `127.0.0.1` |
| `selected.create.json` | Same entries using `--export-host` (default `node-proxy`) |
| `*.import.json` | Matching `{data: {proxies: [...], accounts: []}}` admin import bodies |
| `migration.json` | Name/identity and development-to-server address mapping; no writes |

Each create entry has `name`, `protocol: http`, `host`, and `port`. No inbound
authentication is configured, so exports do not invent username/password values.
The server listeners are reachable only through the Compose network; only trusted
containers should join it. No controller, dashboard or host ports are enabled.

## Stable allocation

Keep and back up `manifest.json` alongside the configuration. Identity hashes
the exact node name, protocol, lowercased server and upstream port, excluding the
password. Password rotation and input order changes preserve ports. Renaming or
changing an endpoint creates a new identity. Removed nodes remain recorded and
their ports are never allocated to another identity; the same identity can return
to its own port. New ports increase monotonically. Default range is 24000–24999;
set `--port-start` and `--port-end` on the first render and retain the range on
later renders. Exhaustion and invalid manifests fail rather than recycling ports.

Run one renderer at a time. Rendering is deterministic; it reserves the manifest
before atomically replacing individual output files. An interrupted render must
be rerun before deployment. Do not discard the manifest to recover from an error.
Updating a bind-mounted config by replacement requires recreating the sidecar;
there is deliberately no controller-driven hot reload.

## Server Compose integration

From the repository root, use the **same project name, project directory, base
Compose files and environment file as the existing backend deployment**. Below,
`sub2api` is an example project name: replace it if the current deployment uses
another name. Relative paths in all overlays resolve against `deploy`, not the
`mihomo` subdirectory. `NODE_PROXY_CONFIG` must be an absolute host path.

```sh
export NODE_PROXY_CONFIG=/srv/private/node-proxy/config.yaml
docker compose --project-directory deploy --env-file /srv/private/sub2api.env \
  -p sub2api -f deploy/docker-compose.yml -f deploy/mihomo/compose.yml config --quiet
```

The above only validates. After configuration review, deployment is an explicit
operator action:

```sh
docker compose --project-directory deploy --env-file /srv/private/sub2api.env \
  -p sub2api -f deploy/docker-compose.yml -f deploy/mihomo/compose.yml \
  up -d --no-deps --force-recreate node-proxy
```

Both services join `<project>_sub2api-network`; do not deploy this overlay under
a separate project. It adds no dependency to backend startup and launches no
database services when targeted as above. Do not use the local overlay in server
deployment. No `privileged`, host networking, TUN devices, added capabilities or
public listener publication are needed. Runtime writable state is ephemeral in
`/tmp`; the config is mounted read-only.

For a **host-native development backend only**, append
`-f deploy/mihomo/compose.local.yml` to the same Compose command and use
`development.create.json` or `--export-host 127.0.0.1`. That overlay publishes
TCP 24000–24999 strictly on `127.0.0.1`. Set `NODE_PROXY_PORT_START` and
`NODE_PROXY_PORT_END` to match a custom generator range. A containerized backend
uses the server export even during development. `--server-host` changes the
server export and migration target if using an explicitly configured network
alias instead of `node-proxy`.

## Existing proxy IDs during server migration

For **new** records, each `*.create.json` item is a body for
`POST /api/v1/admin/proxies`; `*.import.json` is a body for
`POST /api/v1/admin/proxies/data`. These calls can initiate backend latency probes.
This helper does not submit any request or change accounts.

When moving existing proxies from development to the server, preserve the same
database and proxy IDs. Review `migration.json`, list existing proxy records,
and match each by its exact name **and** old host/port. Require exactly one match;
record the matched ID before making changes. After the server sidecar is healthy,
use the existing `PUT /api/v1/admin/proxies/:id` endpoint to update only `host`
and `port` to the mapping's `to` values. Preserve all other proxy fields and account
bindings. Recheck those same IDs with the backend's proxy test operation.
The mapping is a dry plan, not an executable bulk updater.

Do not re-import server addresses over existing development records: the current
import implementation deduplicates by address/auth, so a different host can create
new IDs. No account rebind is necessary when updating the existing IDs. Retired
nodes need separate operator review; rendering never deletes database records.

## Version and verification

The default image is `metacubex/mihomo:v1.19.32`, the official stable release
checked for this integration. The registry manifest includes Linux amd64, arm64,
arm/v7 and 386. Override `MIHOMO_IMAGE` to an explicitly reviewed release or
`metacubex/mihomo@sha256:<verified digest>`; resolve a digest from the official
registry for your deployment and record it, rather than using a floating latest
or Alpha tag. No image download or core execution is needed for Compose config
validation. A real-core syntax check and connectivity test with the private
subscription remain deployment-time checks.

Primary references:

- [Official AnyTLS outbound configuration](https://wiki.metacubex.one/en/config/proxies/anytls/)
- [Listener `proxy` selects an outbound directly](https://wiki.metacubex.one/en/config/inbound/listeners/)
- [HTTP listeners and optional authentication](https://wiki.metacubex.one/en/config/inbound/listeners/http/)
- [Official release v1.19.32](https://github.com/MetaCubeX/mihomo/releases/tag/v1.19.32)
- [Official core image](https://hub.docker.com/r/metacubex/mihomo)

Run synthetic tests (Compose tests skip if the Docker CLI is absent):

```sh
python3 -m unittest discover -s deploy/mihomo -p test_render.py -v
```

Tests render fake credentials, exercise allocation and errors, and validate both
Compose overlays with an isolated synthetic environment using `compose config`.
They never launch services or access a real subscription.
