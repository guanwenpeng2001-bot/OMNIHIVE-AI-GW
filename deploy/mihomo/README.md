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
Only proxy entries and their exact host dependencies are copied; subscription
rules, scripts, DNS, controllers and unrelated configuration are discarded. Administrative labels such as
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

### Required subscription host mappings

The renderer retains exact `hosts` mappings reachable from selected real nodes'
`server` names, following at most 32 mappings per chain and retaining at most
4096 entries. Metadata-only nodes do not contribute dependencies. Unrelated
mappings and wildcard keys are omitted. Matching and emitted host keys/CNAMEs
are lowercased with one optional trailing dot removed; the node's original
server, name, SNI and identity inputs remain unchanged. Ambiguous normalized
keys, required cycles, empty IP lists and invalid required values fail with
redacted errors. No DNS lookup occurs while rendering.

Values may be an IPv4/IPv6 address, a nonempty list of IP addresses, or a single
hostname CNAME; hostname lists are rejected. This follows the
[official hosts schema](https://wiki.metacubex.one/en/config/dns/hosts/).
Public, private and loopback IPs are preserved as supplied, consistent with
the helper's existing endpoint policy: privately review authorized subscriptions
before deployment. No address classification silently removes a required mapping.

Retained mappings are emitted as top-level `hosts` in the private config.
With explicit core DNS, `use-hosts` becomes true. Without `--dns-server`, retained
hosts enable a minimal core DNS block with `nameserver` set to `system`,
`use-hosts: true`, and system hosts lookup disabled. IP-ending chains also use
`system` for `proxy-server-nameserver` and need no DNS listen address; terminal
CNAMEs use the internal service described below. This preserves the deployment's DNS
choice without copying any desktop configuration. Use explicit resolvers where
inherited Docker DNS is unsuitable. With no retained hosts and no DNS option,
the original no-DNS-block behavior remains unchanged.

Host value rotation and host-entry reordering do not change port assignments or
API exports. Keep the existing manifest when rerendering; do not recreate proxy
records or rebind accounts. These are core-local mappings, not mutations of the
machine hosts file, Windows DNS, Docker daemon DNS or Clash.

For a chain ending in a hostname without a retained IP mapping, the renderer
sets the core's internal DNS listener to `127.0.0.1:1053` and routes
`proxy-server-nameserver` to `udp://127.0.0.1:1053`. This is necessary because
the pinned core's raw outbound resolver bypasses CNAME hosts middleware. The
same core's DNS service applies the retained mappings and uses the ordinary
`nameserver` list to resolve the terminal hostname; that list is never pointed
back at the internal listener. This avoids a forwarding loop and preserves
explicit `--dns-server` choices. Chains ending in retained IPs need no internal
listener. Port 1053 is reserved inside this sidecar for the internal resolver.
No DNS port is published by either Compose overlay, and no separate broker is
needed. Runtime connectivity still depends on reachable upstream DNS and nodes.

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

## Opt-in core DNS for portable deployment

If the container's inherited DNS returns desktop VPN FakeIP addresses for node
hostnames, choose resolvers explicitly when rendering. For example, append:

```sh
--dns-server https://1.1.1.1/dns-query \
--dns-server https://8.8.8.8/dns-query
```

These IP-addressed public DoH endpoints avoid inherited DNS even for bootstrap.
Choose endpoints reachable directly from the deployment network; this example
does not assert that either provider is reachable from a particular server.
Production can use its own stable resolver IP or HTTPS DoH endpoint. A bare IP
uses ordinary DNS; HTTPS URLs use DoH. Repeat the option for multiple resolvers.
URLs must have a path and cannot contain userinfo, queries, fragments, percent
escapes or control characters. Proxy/interface suffixes are deliberately rejected
to prevent DNS from depending on the AnyTLS node it must first resolve. Supply
only non-secret resolver addresses on the command line.

The generated core DNS uses `enable: true`, `enhanced-mode: redir-host`, and the
supplied list for both `nameserver` and `proxy-server-nameserver`. DNS connections
are direct (`respect-rules: false`), with no system hosts lookup. `listen: ""`
opens no DNS server port unless a retained terminal CNAME needs the internal
loopback resolver described above. In that case only proxy-server resolution is
sent through the internal service; the supplied list remains its upstream.
The existing core IPv4 choice remains unchanged
(`ipv6: false`, also in DNS); this does not alter host OS IPv6 or DNS settings.
Subscription DNS/rules are still discarded and no TUN or added privilege is used.
These fields follow the [official DNS configuration](https://wiki.metacubex.one/en/config/dns/).

For hostname-based DoH, bootstrap uses supplied IP-addressed resolvers when
available, otherwise the two IP-addressed DoH endpoints above. To choose your own
bootstrap, repeat `--dns-bootstrap-server` with bare IPs or IP-addressed HTTPS
URLs, for example `--dns-bootstrap-server 192.0.2.53` (documentation address;
replace with your reachable resolver). Bootstrap hostnames are rejected, avoiding
a resolution loop. This option requires `--dns-server`.

Without `--dns-server` and without retained hosts, no DNS block is generated and prior behavior is unchanged.
Rerender into the same private output directory, retaining the manifest: DNS
changes preserve ports, node identities and all address exports. Recreate only
the sidecar using the existing deployment procedure after reviewing the generated
config. The helper does not modify Docker daemon DNS, Windows/global DNS, Clash,
or any running service. Container loopback names a container itself; neither
`127.0.0.1` nor Docker's inherited resolver is a portable reference to a desktop
VPN resolver. This core DNS option also does not change the helper's subscription
download resolution.

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
They never access a real subscription. The optional core test is skipped unless
`MIHOMO_TEST_IMAGE=metacubex/mihomo:v1.19.32` is set and that image is already
installed. It uses `--pull never --network none`, no published ports, dropped
capabilities and a read-only fixture mount. Four core syntax checks and a short
synthetic CNAME resolution test run inside an ephemeral container; its two core
processes serve only synthetic data over container loopback and are terminated
on completion. No existing sidecar or external DNS is contacted.
