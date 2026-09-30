#!/usr/bin/env python3
"""Render a standalone per-node Mihomo sidecar. Never contacts the admin API."""
import argparse
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import sys
import tempfile
import urllib.request
import uuid
from urllib.parse import urlsplit

import yaml

MAX_INPUT = 8 * 1024 * 1024
# Anchored administrative labels, not geography or substring guesses.
METADATA = re.compile(
    r"^(?:剩余流量|流量[：:]|套餐到期|到期时间|到期[：:]|距离.*重置|"
    r"重置[：:]|获取订阅|获取最新|获取[：:]|剩余[：:]|"
    r"有效期\s*[:：]?\s*\d{4}-\d{2}-\d{2}(?:\b|,)|最新官网\s*[:：]|"
    r"remaining\b|traffic\s*[:：]|expire[sd]?\b|reset\b)", re.I)
STRING_FIELDS = {"sni", "client-fingerprint", "fingerprint", "name-cert-verify",
                 "client-metadata"}
BOOL_FIELDS = {"udp", "skip-cert-verify", "tfo", "mptcp"}
INT_FIELDS = {"idle-session-check-interval", "idle-session-timeout", "min-idle-session"}
ALLOWED = {"name", "type", "server", "port", "password", "alpn"} | STRING_FIELDS | BOOL_FIELDS | INT_FIELDS
BASE_FIELDS = {"name", "type", "server", "port", "udp", "tfo", "mptcp"}
TLS_FIELDS = {"skip-cert-verify", "fingerprint", "client-fingerprint", "name-cert-verify", "alpn"}
TRANSPORT_FIELDS = {"network", "ws-opts", "grpc-opts"}
PROTOCOL_FIELDS = {
    "anytls": ALLOWED,
    "ss": BASE_FIELDS | {"password", "cipher"},
    "trojan": BASE_FIELDS | TLS_FIELDS | TRANSPORT_FIELDS | {"password", "sni"},
    "vmess": BASE_FIELDS | TLS_FIELDS | TRANSPORT_FIELDS | {"uuid", "alterId", "cipher", "tls", "servername"},
    "vless": BASE_FIELDS | TLS_FIELDS | TRANSPORT_FIELDS | {"uuid", "tls", "servername", "flow", "reality-opts"},
    "hysteria2": BASE_FIELDS | {"password", "sni", "skip-cert-verify", "fingerprint", "alpn",
                                "obfs", "obfs-password", "ports", "mport"},
}


class SafeError(Exception):
    """Only fixed, non-secret diagnostic messages belong here."""


class ConfigDumper(yaml.SafeDumper):
    """Quote strings explicitly across Python and Go YAML scalar resolvers."""


def quoted_string(dumper, value):
    return dumper.represent_scalar("tag:yaml.org,2002:str", value, style='"')


ConfigDumper.add_representer(str, quoted_string)


def serialize_config(config):
    return yaml.dump(config, Dumper=ConfigDumper, allow_unicode=True, sort_keys=False)


def require(condition, message):
    if not condition:
        raise SafeError(message)


def text_value(value):
    return isinstance(value, str) and bool(value.strip()) and not any(ord(char) < 32 for char in value)


def port_ranges(value):
    require(isinstance(value, str) and re.fullmatch(r"\d+(?:-\d+)?(?:,\d+(?:-\d+)?)*", value),
            "Invalid Hysteria2 port ranges.")
    for segment in value.split(","):
        bounds = [int(part) for part in segment.split("-")]
        require(1 <= bounds[0] <= bounds[-1] <= 65535, "Invalid Hysteria2 port ranges.")
    return value


def validate_transport(entry):
    require(isinstance(entry.get("network", "tcp"), str) and entry.get("network", "tcp") in {"tcp", "ws", "grpc"},
            "Unsupported transport network.")
    for network, fields in {
        "ws": {"path": str, "headers": dict, "max-early-data": int, "early-data-header-name": str,
               "v2ray-http-upgrade": bool, "v2ray-http-upgrade-fast-open": bool},
        "grpc": {"grpc-service-name": str, "grpc-user-agent": str},
    }.items():
        key = network + "-opts"
        if key not in entry:
            continue
        options = entry[key]
        require(entry.get("network") == network and isinstance(options, dict) and not (set(options) - fields.keys()),
                "Unsupported or mismatched nested transport options.")
        for field, value in options.items():
            require(type(value) is fields[field], "Invalid nested transport field type.")
            if isinstance(value, str):
                require(value == "" or text_value(value), "Invalid nested transport text.")
            elif type(value) is int:
                require(value >= 0, "Invalid transport size.")
            elif isinstance(value, dict):
                require(all(text_value(header) and text_value(content) for header, content in value.items()),
                        "Invalid transport headers.")
    if "reality-opts" in entry:
        reality = entry["reality-opts"]
        require(entry.get("tls") is True and isinstance(reality, dict) and
                not (set(reality) - {"public-key", "short-id"}), "Invalid Reality settings.")
        require(isinstance(reality.get("public-key"), str) and
                re.fullmatch(r"[A-Za-z0-9_-]{43}", reality["public-key"]), "Invalid Reality public key.")
        short_id = reality.get("short-id", "")
        require(isinstance(short_id, str) and re.fullmatch(r"(?:[0-9a-fA-F]{2}){0,8}", short_id), "Invalid Reality short ID.")


def validate_node(source):
    entry = dict(source)
    protocol = entry.get("type")
    require(isinstance(protocol, str) and protocol in PROTOCOL_FIELDS, "Unsupported proxy protocol.")
    require(not (set(entry) - PROTOCOL_FIELDS[protocol]), "Unsupported proxy fields; review input privately.")
    require(text_value(entry.get("server")), "Invalid proxy endpoint.")
    require(type(entry.get("port")) is int and 1 <= entry["port"] <= 65535, "Invalid outbound port.")
    if protocol in {"anytls", "ss", "trojan", "hysteria2"}:
        require(text_value(entry.get("password")), "Missing or invalid proxy credential.")
    if protocol in {"vmess", "vless"}:
        require(text_value(entry.get("uuid")), "Missing or invalid proxy UUID.")
        try:
            uuid.UUID(entry["uuid"])
        except ValueError:
            raise SafeError("Invalid proxy UUID.") from None
    for field in (STRING_FIELDS | {"servername", "cipher", "sni"}) & entry.keys():
        require(text_value(entry[field]), "Invalid proxy text field.")
    for field in (BOOL_FIELDS | {"tls"}) & entry.keys():
        require(type(entry[field]) is bool, "Invalid proxy boolean field.")
    for field in (INT_FIELDS | {"alterId"}) & entry.keys():
        require(type(entry[field]) is int and entry[field] >= 0, "Invalid proxy integer field.")
    if "alpn" in entry:
        require(isinstance(entry["alpn"], list) and all(text_value(item) for item in entry["alpn"]), "Invalid ALPN list.")
    if protocol == "ss":
        require(text_value(entry.get("cipher")), "Shadowsocks cipher is required.")
    if protocol == "vmess":
        require(type(entry.get("alterId")) is int and 0 <= entry["alterId"] <= 65535 and
                isinstance(entry.get("cipher"), str) and entry["cipher"] in {"auto", "none", "zero", "aes-128-gcm", "chacha20-poly1305"},
                "Invalid or missing VMess alterId/cipher.")
    if protocol == "vless":
        require(isinstance(entry.get("flow", ""), str) and entry.get("flow", "") in {"", "xtls-rprx-vision", "xtls-rprx-vision-udp443"}, "Unsupported VLESS flow.")
    if protocol in {"trojan", "vmess", "vless"}:
        validate_transport(entry)
    if protocol == "hysteria2":
        # mport is a subscription alias, not a field understood by this core.
        alias = entry.pop("mport", "")
        require(isinstance(alias, str), "Invalid Hysteria2 mport alias.")
        if alias:
            port_ranges(alias)
            require(not entry.get("ports") or entry["ports"] == alias, "Conflicting Hysteria2 ports and mport.")
            entry["ports"] = alias
        if "ports" in entry:
            require(isinstance(entry["ports"], str), "Invalid Hysteria2 ports.")
            if entry["ports"]:
                port_ranges(entry["ports"])
        require(isinstance(entry.get("obfs", ""), str) and entry.get("obfs", "") in {"", "salamander", "gecko"}, "Unsupported Hysteria2 obfuscation.")
        if entry.get("obfs"):
            require(text_value(entry.get("obfs-password")), "Hysteria2 obfuscation password is required.")
        elif "obfs-password" in entry:
            require(entry["obfs-password"] == "", "Hysteria2 obfuscation mode is required.")
    return entry


def parse_subscription(raw):
    require(len(raw) <= MAX_INPUT, "Subscription exceeds size limit.")
    try:
        # Reject aliases: no recursive structures or expansion surprises.
        require(not any(isinstance(token, yaml.tokens.AliasToken) for token in yaml.scan(raw)),
                "YAML aliases are unsupported.")
        document = yaml.safe_load(raw)
    except yaml.YAMLError:
        raise SafeError("Invalid subscription YAML.") from None
    require(isinstance(document, dict) and isinstance(document.get("proxies"), list),
            "Subscription requires a proxies list.")
    nodes, names = [], set()
    for entry in document["proxies"]:
        require(isinstance(entry, dict) and text_value(entry.get("name")), "Invalid proxy entry.")
        name = entry["name"]
        label = re.sub(r"^[\W_]+", "", name, flags=re.UNICODE)
        if METADATA.match(label):
            continue
        require(name not in names, "Duplicate node name; explicit disambiguation required.")
        names.add(name)
        nodes.append(validate_node(entry))
    require(bool(nodes), "No usable proxy nodes.")
    return nodes, required_hosts(document.get("hosts", {}), nodes)


def parse_nodes(raw):
    return parse_subscription(raw)[0]


def valid_source_label(label):
    return isinstance(label, str) and re.fullmatch(r"[^\W_][\w-]{0,31}", label) is not None


def combine_sources(primary, additions, previous=None):
    require(len(additions) <= 8, "At most eight additional inputs are supported.")
    nodes, hosts = parse_subscription(primary)
    groups = [("primary", nodes, dict(hosts))]
    labels = set()
    for label, raw in sorted(additions, key=lambda item: item[0].lower()):
        require(valid_source_label(label),
                "Invalid additional input label.")
        require(label.lower() != "primary" and label.lower() not in labels, "Duplicate or reserved additional input label.")
        labels.add(label.lower())
        source_nodes, source_hosts = parse_subscription(raw)
        groups.append((label, source_nodes, source_hosts))
        for key, value in source_hosts.items():
            require(key not in hosts or hosts[key] == value, "Conflicting required hosts across sources.")
            hosts[key] = value
    # Never let another source's mapping change an existing source's resolution.
    for _, source_nodes, source_hosts in groups:
        require(required_hosts(hosts, source_nodes) == source_hosts, "Cross-source hosts dependency conflict.")
    allocated = {record["identity"] for record in previous.get("nodes", [])} if previous else set()
    names, candidates = set(), []
    for label, source_nodes, _ in groups:
        for node in source_nodes:
            display = node["name"] if label == "primary" else f"[{label}] {node['name']}"
            require(display not in names, "Combined node name collision; choose a different stable label.")
            names.add(display)
            candidate = {**node, "name": display}
            signature = json.dumps({key: value for key, value in node.items() if key != "name"}, sort_keys=True)
            candidates.append((label, node["name"], candidate, signature))
    # Original source identities are never collapsed. Existing allocations win
    # over newly added aliases; otherwise stable labels/names break ties.
    candidates.sort(key=lambda item: (item[0] != "primary", identity(item[2]) not in allocated, item[0].lower(), item[1]))
    canonical, merged, aliases = {}, [], []
    counts = {label: {"source": label, "real_nodes": len(source_nodes), "canonical_nodes": 0, "duplicates": 0}
              for label, source_nodes, _ in groups}
    for label, original_name, candidate, signature in candidates:
        if label == "primary" or signature not in canonical:
            canonical.setdefault(signature, candidate)
            merged.append(candidate)
            counts[label]["canonical_nodes"] += 1
            selected = candidate
        else:
            counts[label]["duplicates"] += 1
            selected = canonical[signature]
        aliases.append({"source": label, "name": original_name, "canonical_name": selected["name"],
                        "canonical_identity": identity(selected)})
    aliases.sort(key=lambda item: (item["source"].lower(), item["name"]))
    summary = {"sources": list(counts.values()), "canonical_nodes": len(merged),
               "duplicates": sum(count["duplicates"] for count in counts.values()), "aliases": aliases}
    return merged, dict(sorted(hosts.items())), summary


def hostname(value):
    require(isinstance(value, str) and value.isascii(), "Invalid required hosts hostname.")
    normalized = value[:-1] if value.endswith(".") else value
    require(0 < len(normalized) <= 253 and all(
        re.fullmatch(r"[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?", label)
        for label in normalized.split(".")), "Invalid required hosts hostname.")
    return normalized.lower()


def host_value(value):
    """Return a canonical IP, IP list, or one CNAME; never resolve it here."""
    if isinstance(value, list):
        require(bool(value), "Required hosts IP list is empty.")
        result = []
        for address in value:
            require(isinstance(address, str) and "%" not in address, "Invalid required hosts IP list.")
            try:
                result.append(str(ipaddress.ip_address(address)))
            except ValueError:
                raise SafeError("Invalid required hosts IP list.") from None
        return result
    require(isinstance(value, str) and "%" not in value, "Invalid required hosts value.")
    try:
        return str(ipaddress.ip_address(value))
    except ValueError:
        alias = hostname(value)
        require("." in alias and not all(label.isdigit() for label in alias.split(".")),
                "Invalid required hosts alias.")
        return alias


def required_hosts(source, nodes):
    require(isinstance(source, dict), "Subscription hosts must be a mapping.")
    if not source:
        return {}
    indexed = {}
    for key, value in source.items():
        try:
            normalized = hostname(key)
        except SafeError:
            # Wildcards and unrelated invalid keys are not exact dependencies.
            continue
        indexed.setdefault(normalized, []).append(value)
    retained = {}
    for node in nodes:
        try:
            ipaddress.ip_address(node["server"])
            continue
        except ValueError:
            current = hostname(node["server"])
        visited = set()
        while current in indexed:
            require(current not in visited, "Required hosts alias cycle.")
            require(len(visited) < 32, "Required hosts alias chain exceeds 32 mappings.")
            visited.add(current)
            require(len(indexed[current]) == 1, "Ambiguous required hosts hostname.")
            value = host_value(indexed[current][0])
            retained[current] = value
            require(len(retained) <= 4096, "Too many required hosts mappings.")
            if isinstance(value, list):
                break
            try:
                ipaddress.ip_address(value)
                break
            except ValueError:
                current = value
    return dict(sorted(retained.items()))


def needs_hosts_resolver(hosts):
    for value in hosts.values():
        if isinstance(value, str) and value not in hosts:
            try:
                ipaddress.ip_address(value)
            except ValueError:
                return True
    return False


def identity(node):
    # Credentials intentionally excluded: password rotation keeps the listener.
    value = [node["name"], node["type"], node["server"].lower(), node["port"]]
    return hashlib.sha256(json.dumps(value, ensure_ascii=False).encode()).hexdigest()


def allocate(nodes, previous, start, end):
    require(1024 <= start <= end <= 65535, "Invalid listener port range.")
    if previous is None:
        previous = {"version": 1, "port_start": start, "port_end": end, "nodes": []}
    require(isinstance(previous, dict) and previous.get("version") == 1 and
            previous.get("port_start") == start and type(previous.get("port_end")) is int and
            start <= previous["port_end"] <= end and isinstance(previous.get("nodes"), list),
            "Invalid manifest; only upper port-range expansion is supported.")
    records, used = {}, set()
    for record in previous["nodes"]:
        require(isinstance(record, dict) and isinstance(record.get("identity"), str) and
                re.fullmatch(r"[0-9a-f]{64}", record["identity"]) and
                text_value(record.get("name")) and type(record.get("port")) is int and
                start <= record["port"] <= previous["port_end"] and type(record.get("active")) is bool,
                "Invalid manifest record.")
        require(record["identity"] not in records and record["port"] not in used, "Duplicate manifest allocation.")
        records[record["identity"]] = {**record, "active": False}
        used.add(record["port"])
    next_port = max(used, default=start - 1) + 1
    for node in sorted(nodes, key=identity):
        key = identity(node)
        if key not in records:
            require(next_port <= end, "Listener port range exhausted; retired ports are reserved.")
            records[key] = {"identity": key, "name": node["name"], "port": next_port}
            next_port += 1
        records[key]["active"] = True
    return {"version": 1, "port_start": start, "port_end": end,
            "nodes": sorted(records.values(), key=lambda record: record["port"])}


def dns_endpoint(value):
    """Accept plain IP DNS or HTTPS DoH, without Mihomo routing directives."""
    message = "Invalid DNS resolver; use an IP address or HTTPS DoH URL without credentials, query or fragment."
    require(isinstance(value, str) and value.isascii() and bool(value) and
            not any(char.isspace() or ord(char) < 33 or ord(char) == 127 for char in value)
            and not any(char in value for char in "\\?#@%"), message)
    try:
        ipaddress.ip_address(value)
        return True
    except ValueError:
        pass
    try:
        parsed = urlsplit(value)
        require(parsed.scheme == "https" and bool(parsed.hostname) and
                parsed.path.startswith("/") and bool(parsed.path[1:]) and
                (parsed.port is None or 1 <= parsed.port <= 65535) and
                not parsed.netloc.endswith(":"), message)
        try:
            ipaddress.ip_address(parsed.hostname)
            return True
        except ValueError:
            require(len(parsed.hostname) <= 253 and all(
                re.fullmatch(r"[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?", label)
                for label in parsed.hostname.split(".")), message)
            return False
    except ValueError:
        raise SafeError(message) from None


def resolver_config(servers, bootstrap_servers=None):
    require(bool(servers) or not bootstrap_servers, "DNS bootstrap requires --dns-server.")
    if not servers:
        return None
    literal_servers = [server for server in servers if dns_endpoint(server)]
    bootstrap = bootstrap_servers or literal_servers or [
        "https://1.1.1.1/dns-query", "https://8.8.8.8/dns-query"]
    require(all(dns_endpoint(server) for server in bootstrap),
            "DNS bootstrap resolvers must use literal IP addresses.")
    return {"enable": True, "enhanced-mode": "redir-host", "listen": "",
            "ipv6": False, "respect-rules": False, "use-hosts": False,
            "use-system-hosts": False, "default-nameserver": list(bootstrap),
            "nameserver": list(servers), "proxy-server-nameserver": list(servers)}


def construct(nodes, manifest, host, dns=None, hosts=None):
    require(text_value(host) and not any(char in host for char in "/@?# "), "Invalid export host.")
    by_identity = {identity(node): node for node in nodes}
    listeners, proxies, entries = [], [], []
    for record in manifest["nodes"]:
        if not record["active"]:
            continue
        outbound = "out-" + record["identity"]
        proxies.append({**by_identity[record["identity"]], "name": outbound})
        listeners.append({"name": "in-" + record["identity"], "type": "http",
                          "listen": "0.0.0.0", "port": record["port"], "proxy": outbound})
        entries.append({"name": record["name"], "protocol": "http", "host": host, "port": record["port"]})
    config = {"mode": "rule", "log-level": "silent", "ipv6": False,
              "allow-lan": True, "listeners": listeners, "proxies": proxies,
              "rules": ["MATCH,REJECT"]}
    if dns is not None:
        config["dns"] = dict(dns)
    if hosts:
        config["hosts"] = hosts
        if dns is None:
            # Keep the deployment's resolver choice, but enable hosts handling.
            config["dns"] = {"enable": True, "enhanced-mode": "redir-host", "listen": "",
                             "ipv6": False, "respect-rules": False, "use-system-hosts": False,
                             "nameserver": ["system"], "proxy-server-nameserver": ["system"]}
        config["dns"]["use-hosts"] = True
        if needs_hosts_resolver(hosts):
            require(all(listener["port"] != 1053 for listener in listeners),
                    "Listener port 1053 conflicts with required internal DNS; review the manifest privately.")
            # The raw outbound resolver bypasses CNAME hosts handling. Route it
            # through this core's own DNS service; that service uses nameserver,
            # not proxy-server-nameserver, so this does not forward to itself.
            config["dns"]["listen"] = "127.0.0.1:1053"
            config["dns"]["proxy-server-nameserver"] = ["udp://127.0.0.1:1053"]
    return config, entries


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, response, code, message, headers, new_url):
        raise SafeError("Subscription redirect refused; supply the official final URL privately.")


def read_input_file(filename):
    source_path = Path(filename).resolve()
    require(not source_path.is_relative_to(Path(__file__).resolve().parents[2]),
            "Subscription input must be outside the source checkout.")
    with source_path.open("rb") as source:
        return source.read(MAX_INPUT + 1)


def read_source(args):
    if args.input:
        return read_input_file(args.input)
    require(re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", args.subscription_url_env) is not None,
            "Invalid subscription environment variable name.")
    url = os.environ.get(args.subscription_url_env, "")
    require(urlsplit(url).scheme == "https" and bool(urlsplit(url).hostname),
            "Subscription environment variable must contain an HTTPS URL.")
    # No inherited desktop/system HTTP proxy and no third-party conversion.
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    with opener.open(urllib.request.Request(url, headers={"User-Agent": "mihomo"}), timeout=30) as response:
        return response.read(MAX_INPUT + 1)


def private_write(path, content):
    descriptor, temporary = tempfile.mkstemp(dir=path.parent, prefix=".render-")
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8", newline="\n") as stream:
            stream.write(content)
        os.chmod(temporary, 0o600)
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def render(args):
    dns = resolver_config(args.dns_server, args.dns_bootstrap_server)
    output = Path(args.output_dir).resolve()
    require(not output.is_relative_to(Path(__file__).resolve().parents[2]),
            "Output directory must be outside the source checkout.")
    require(len(args.additional_input) <= 8, "At most eight additional inputs are supported.")
    additions = []
    for specification in args.additional_input:
        label, separator, filename = specification.partition("=")
        require(bool(separator and filename) and valid_source_label(label),
                "Additional input must be LABEL=FILE with a stable label.")
        additions.append((label, read_input_file(filename)))
    manifest_path = output / "manifest.json"
    previous = json.loads(manifest_path.read_text(encoding="utf-8")) if manifest_path.exists() else None
    nodes, hosts, summary = combine_sources(read_source(args), additions, previous)
    manifest = allocate(nodes, previous, args.port_start, args.port_end)
    config, selected = construct(nodes, manifest, args.export_host, dns, hosts)
    _, server = construct(nodes, manifest, args.server_host)
    _, development = construct(nodes, manifest, "127.0.0.1")
    artifacts = {"config.yaml": serialize_config(config)}
    for prefix, entries in [("selected", selected), ("server", server), ("development", development)]:
        artifacts[prefix + ".create.json"] = json.dumps(entries, ensure_ascii=False, indent=2) + "\n"
        artifacts[prefix + ".import.json"] = json.dumps({"data": {"proxies": entries, "accounts": []}}, ensure_ascii=False, indent=2) + "\n"
    migration = [{"name": record["name"], "identity": record["identity"],
                  "from": {"host": "127.0.0.1", "port": record["port"]},
                  "to": {"host": args.server_host, "port": record["port"]}}
                 for record in manifest["nodes"] if record["active"]]
    artifacts["migration.json"] = json.dumps(migration, ensure_ascii=False, indent=2) + "\n"
    artifacts["sources.json"] = json.dumps(summary, ensure_ascii=False, indent=2) + "\n"
    output.mkdir(parents=True, exist_ok=True, mode=0o700)
    os.chmod(output, 0o700)
    private_write(output / ".gitignore", "*\n")
    # Reserve ports before writing config: interrupted renders cannot reassign them.
    private_write(manifest_path, json.dumps(manifest, ensure_ascii=False, indent=2) + "\n")
    for filename, content in artifacts.items():
        private_write(output / filename, content)
    print(f"Rendered {len(nodes)} nodes; reused {summary['duplicates']} exact duplicates; keep manifest.json for all future renders.")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    source = parser.add_mutually_exclusive_group(required=True)
    source.add_argument("--input", help="Downloaded Clash YAML file outside source")
    source.add_argument("--subscription-url-env", help="ENV NAME only; never pass a URL")
    parser.add_argument("--additional-input", action="append", default=[],
                        help="Additional private Clash YAML as LABEL=FILE; repeatable, maximum eight")
    parser.add_argument("--output-dir", required=True)
    parser.add_argument("--port-start", type=int, default=24000)
    parser.add_argument("--port-end", type=int, default=24999)
    parser.add_argument("--export-host", default="node-proxy")
    parser.add_argument("--server-host", default="node-proxy")
    parser.add_argument("--dns-server", action="append", help="Opt-in resolver: IP or HTTPS DoH URL; repeatable")
    parser.add_argument("--dns-bootstrap-server", action="append",
                        help="Optional IP-based bootstrap resolver for DoH hostnames; repeatable")
    args = parser.parse_args()
    try:
        render(args)
        return 0
    except SafeError as error:
        print(str(error), file=sys.stderr)
    except Exception:
        # Library exceptions may contain credentials, source snippets or URLs.
        print("Render failed; check input, manifest, network and filesystem privately.", file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
