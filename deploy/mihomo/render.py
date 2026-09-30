#!/usr/bin/env python3
"""Render a standalone AnyTLS sidecar. Never contacts the admin API."""
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
from urllib.parse import urlsplit

import yaml

MAX_INPUT = 8 * 1024 * 1024
# Anchored administrative labels, not geography or substring guesses.
METADATA = re.compile(
    r"^(?:剩余流量|流量[：:]|套餐到期|到期时间|到期[：:]|距离.*重置|"
    r"重置[：:]|获取订阅|获取最新|获取[：:]|剩余[：:]|"
    r"remaining\b|traffic\s*[:：]|expire[sd]?\b|reset\b)", re.I)
STRING_FIELDS = {"sni", "client-fingerprint", "fingerprint", "name-cert-verify",
                 "client-metadata"}
BOOL_FIELDS = {"udp", "skip-cert-verify", "tfo", "mptcp"}
INT_FIELDS = {"idle-session-check-interval", "idle-session-timeout", "min-idle-session"}
ALLOWED = {"name", "type", "server", "port", "password", "alpn"} | STRING_FIELDS | BOOL_FIELDS | INT_FIELDS


class SafeError(Exception):
    """Only fixed, non-secret diagnostic messages belong here."""


def require(condition, message):
    if not condition:
        raise SafeError(message)


def text_value(value):
    return isinstance(value, str) and bool(value.strip()) and not any(ord(char) < 32 for char in value)


def parse_nodes(raw):
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
        require(entry.get("type") == "anytls", "Unsupported proxy protocol; only AnyTLS is supported.")
        require(not (set(entry) - ALLOWED), "Unsupported AnyTLS fields; review input privately.")
        require(name not in names, "Duplicate node name; explicit disambiguation required.")
        require(text_value(entry.get("server")) and text_value(entry.get("password")), "Invalid AnyTLS endpoint or credential.")
        require(type(entry.get("port")) is int and 1 <= entry["port"] <= 65535, "Invalid outbound port.")
        for field in STRING_FIELDS & entry.keys():
            require(text_value(entry[field]), "Invalid AnyTLS text field.")
        for field in BOOL_FIELDS & entry.keys():
            require(type(entry[field]) is bool, "Invalid AnyTLS boolean field.")
        for field in INT_FIELDS & entry.keys():
            require(type(entry[field]) is int and entry[field] >= 0, "Invalid AnyTLS session field.")
        if "alpn" in entry:
            require(isinstance(entry["alpn"], list) and all(text_value(item) for item in entry["alpn"]), "Invalid ALPN list.")
        names.add(name)
        nodes.append(dict(entry))
    require(bool(nodes), "No usable AnyTLS nodes.")
    return nodes


def identity(node):
    # Credentials intentionally excluded: password rotation keeps the listener.
    value = [node["name"], node["type"], node["server"].lower(), node["port"]]
    return hashlib.sha256(json.dumps(value, ensure_ascii=False).encode()).hexdigest()


def allocate(nodes, previous, start, end):
    require(1024 <= start <= end <= 65535, "Invalid listener port range.")
    if previous is None:
        previous = {"version": 1, "port_start": start, "port_end": end, "nodes": []}
    require(isinstance(previous, dict) and previous.get("version") == 1 and
            previous.get("port_start") == start and previous.get("port_end") == end and
            isinstance(previous.get("nodes"), list), "Invalid manifest or changed port range.")
    records, used = {}, set()
    for record in previous["nodes"]:
        require(isinstance(record, dict) and isinstance(record.get("identity"), str) and
                re.fullmatch(r"[0-9a-f]{64}", record["identity"]) and
                text_value(record.get("name")) and type(record.get("port")) is int and
                start <= record["port"] <= end and type(record.get("active")) is bool,
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


def construct(nodes, manifest, host, dns=None):
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
        config["dns"] = dns
    return config, entries


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, response, code, message, headers, new_url):
        raise SafeError("Subscription redirect refused; supply the official final URL privately.")


def read_source(args):
    if args.input:
        source_path = Path(args.input).resolve()
        require(not source_path.is_relative_to(Path(__file__).resolve().parents[2]),
                "Subscription input must be outside the source checkout.")
        with source_path.open("rb") as source:
            return source.read(MAX_INPUT + 1)
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
    nodes = parse_nodes(read_source(args))
    manifest_path = output / "manifest.json"
    previous = json.loads(manifest_path.read_text(encoding="utf-8")) if manifest_path.exists() else None
    manifest = allocate(nodes, previous, args.port_start, args.port_end)
    config, selected = construct(nodes, manifest, args.export_host, dns)
    _, server = construct(nodes, manifest, args.server_host)
    _, development = construct(nodes, manifest, "127.0.0.1")
    artifacts = {"config.yaml": yaml.safe_dump(config, allow_unicode=True, sort_keys=False)}
    for prefix, entries in [("selected", selected), ("server", server), ("development", development)]:
        artifacts[prefix + ".create.json"] = json.dumps(entries, ensure_ascii=False, indent=2) + "\n"
        artifacts[prefix + ".import.json"] = json.dumps({"data": {"proxies": entries, "accounts": []}}, ensure_ascii=False, indent=2) + "\n"
    migration = [{"name": record["name"], "identity": record["identity"],
                  "from": {"host": "127.0.0.1", "port": record["port"]},
                  "to": {"host": args.server_host, "port": record["port"]}}
                 for record in manifest["nodes"] if record["active"]]
    artifacts["migration.json"] = json.dumps(migration, ensure_ascii=False, indent=2) + "\n"
    output.mkdir(parents=True, exist_ok=True, mode=0o700)
    os.chmod(output, 0o700)
    private_write(output / ".gitignore", "*\n")
    # Reserve ports before writing config: interrupted renders cannot reassign them.
    private_write(manifest_path, json.dumps(manifest, ensure_ascii=False, indent=2) + "\n")
    for filename, content in artifacts.items():
        private_write(output / filename, content)
    print(f"Rendered {len(nodes)} nodes; keep manifest.json for all future renders.")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    source = parser.add_mutually_exclusive_group(required=True)
    source.add_argument("--input", help="Downloaded Clash YAML file outside source")
    source.add_argument("--subscription-url-env", help="ENV NAME only; never pass a URL")
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
