"""Synthetic verification; optional core tests use isolated network-none containers."""
import contextlib
import copy
import io
import json
import os
from pathlib import Path
import subprocess
import shutil
import sys
import tempfile
import unittest
from unittest.mock import patch

import yaml
import render


def node(name="Unknown real location"):
    return {"name": name, "type": "anytls", "server": "example.invalid",
            "port": 443, "password": "SYNTHETIC-SECRET", "sni": "example.invalid",
            "skip-cert-verify": False, "udp": True}


class RenderTests(unittest.TestCase):
    @unittest.skipUnless(os.environ.get("MIHOMO_TEST_IMAGE"), "Set MIHOMO_TEST_IMAGE for isolated core verification")
    def test_core_hosts_syntax_and_isolated_cname_resolution(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            nodes = [node()]
            manifest = render.allocate(nodes, None, 24000, 24010)
            cname = {"example.invalid": "alias.invalid", "alias.invalid": "terminal.invalid"}
            ip_chain = {**cname, "terminal.invalid": ["192.0.2.50", "2001:db8::50"]}
            configs = {}
            for label, hosts in (("cname", cname), ("chain", ip_chain)):
                for resolver, dns in (("system", None), ("explicit", render.resolver_config(["https://1.1.1.1/dns-query"]))):
                    configs[label + "-" + resolver] = render.construct(nodes, manifest, "node-proxy", dns, hosts)[0]
            configs["subject"] = render.construct(nodes, manifest, "node-proxy", render.resolver_config(["127.0.0.1"]), cname)[0]
            configs["upstream"] = {"hosts": {"terminal.invalid": "192.0.2.50"}, "log-level": "silent",
                                   "dns": {"enable": True, "use-hosts": True, "use-system-hosts": False,
                                           "listen": "127.0.0.1:53", "nameserver": ["192.0.2.53"], "enhanced-mode": "redir-host"}}
            for label, config in configs.items():
                (root / (label + ".yaml")).write_text(yaml.safe_dump(config), encoding="utf-8")
            script = '''set -eu
for variant in cname-system cname-explicit chain-system chain-explicit; do
  /mihomo -t -d /tmp/check -f /fixtures/$variant.yaml
done
/mihomo -d /tmp/upstream -f /fixtures/upstream.yaml > /tmp/upstream.log 2>&1 &
upstream_pid=$!
/mihomo -d /tmp/subject -f /fixtures/subject.yaml > /tmp/subject.log 2>&1 &
subject_pid=$!
trap 'kill "$subject_pid" "$upstream_pid" 2>/dev/null || true' EXIT
sleep 1
nslookup example.invalid 127.0.0.1:1053 > /tmp/lookup.txt 2>&1
cat /tmp/lookup.txt
grep -q '192.0.2.50' /tmp/lookup.txt
'''
            (root / "verify.sh").write_text(script, encoding="utf-8", newline="\n")
            result = subprocess.run(["docker", "run", "--rm", "--pull", "never", "--network", "none",
                                     "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
                                     "--tmpfs", "/tmp", "--mount", f"type=bind,source={root},target=/fixtures,readonly",
                                     "--entrypoint", "/bin/sh", os.environ["MIHOMO_TEST_IMAGE"],
                                     "-c", "timeout 15 sh /fixtures/verify.sh"], capture_output=True, text=True, timeout=30)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertEqual(result.stdout.count("test is successful"), 4)
            self.assertIn("192.0.2.50", result.stdout)

    def test_hosts_dependency_chain_and_metadata_filter(self):
        real = {**node(), "server": "NODE.Example.Invalid.", "sni": "tls.example.invalid"}
        metadata = {**node("Remaining: 1 GB"), "server": "metadata.example.invalid"}
        source = {"NODE.example.invalid.": "Alias.Example.Invalid.",
                  "alias.example.invalid": "final.example.invalid",
                  "final.example.invalid": ["192.0.2.10", "2001:db8::10"],
                  "unrelated.example.invalid": "SECRET-invalid-value/",
                  "metadata.example.invalid": "192.0.2.20", "*.example.invalid": "192.0.2.30"}
        nodes, hosts = render.parse_subscription(yaml.safe_dump({"proxies": [metadata, real], "hosts": source}))
        self.assertEqual(nodes, [real])
        self.assertEqual(hosts, {"node.example.invalid": "alias.example.invalid",
                                 "alias.example.invalid": "final.example.invalid",
                                 "final.example.invalid": ["192.0.2.10", "2001:db8::10"]})
        manifest = render.allocate(nodes, None, 24000, 24010)
        for dns in (None, render.resolver_config(["https://1.1.1.1/dns-query"])):
            config, _ = render.construct(nodes, manifest, "node-proxy", dns, hosts)
            self.assertEqual(config["hosts"], hosts)
            self.assertTrue(config["dns"]["use-hosts"])
            self.assertTrue(config["dns"]["enable"])
            self.assertFalse(config["dns"]["use-system-hosts"])
            self.assertEqual(config["dns"]["listen"], "")
            self.assertEqual(config["proxies"][0]["server"], real["server"])
            self.assertEqual(config["proxies"][0]["sni"], real["sni"])

    def test_hosts_terminal_alias_and_private_ips(self):
        nodes = [node()]
        for value in ("alias.example.invalid", "10.0.0.1", "127.0.0.1", "2001:db8::1", ["10.0.0.1"]):
            self.assertEqual(render.required_hosts({"example.invalid": value}, nodes), {"example.invalid": value})
        self.assertEqual(render.required_hosts({"unrelated.example": "192.0.2.1"}, nodes), {})
        manifest = render.allocate(nodes, None, 24000, 24010)
        hosts = {"example.invalid": "alias.invalid", "alias.invalid": "terminal.invalid"}
        for dns in (None, render.resolver_config(["https://1.1.1.1/dns-query"])):
            config, _ = render.construct(nodes, manifest, "node-proxy", dns, hosts)
            self.assertEqual(config["dns"]["listen"], "127.0.0.1:1053")
            self.assertEqual(config["dns"]["proxy-server-nameserver"], ["udp://127.0.0.1:1053"])
            self.assertEqual(config["dns"]["nameserver"], dns["nameserver"] if dns else ["system"])
            self.assertNotIn("udp://127.0.0.1:1053", config["dns"]["nameserver"])
            self.assertEqual(config["proxies"][0]["server"], nodes[0]["server"])
        self.assertFalse(render.needs_hosts_resolver({"example.invalid": "alias.invalid", "alias.invalid": "192.0.2.1"}))
        with self.assertRaises(render.SafeError):
            render.construct(nodes, render.allocate(nodes, None, 1053, 1053), "node-proxy", hosts=hosts)

    def test_hosts_invalid_cycle_and_depth_errors_are_safe(self):
        nodes = [node()]
        invalid_values = [None, 123, [], ["alias.example.invalid"], ["192.0.2.1", "bad"],
                          "https://example.invalid/SECRET", "alias.example.invalid\nSECRET", "999.999.999.999",
                          "alias..invalid", "*.example.invalid", "singlelabel", "fe80::1%SECRET"]
        invalid_maps = [{"example.invalid": value} for value in invalid_values]
        invalid_maps += [{"example.invalid": "EXAMPLE.INVALID."},
                         {"example.invalid": "alias.invalid", "alias.invalid": "example.invalid"},
                         {"example.invalid": "192.0.2.1", "EXAMPLE.INVALID.": "192.0.2.2"}]
        chain = {"example.invalid": "alias0.invalid"}
        chain.update({f"alias{index}.invalid": f"alias{index + 1}.invalid" for index in range(32)})
        invalid_maps.append(chain)
        for source in invalid_maps:
            with self.subTest(source=source), self.assertRaises(render.SafeError) as raised:
                render.required_hosts(source, nodes)
            self.assertNotIn("SECRET", str(raised.exception))

    def test_hosts_rotation_keeps_manifest_and_exports(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "synthetic.yaml"
            output = root / "generated"
            command = [sys.executable, str(Path(render.__file__)), "--input", str(source), "--output-dir", str(output)]
            baseline = None
            configs = []
            for hosts in ({"example.invalid": "alias.invalid", "alias.invalid": "192.0.2.1"},
                          {"alias.invalid": "192.0.2.1", "EXAMPLE.INVALID.": "ALIAS.INVALID."},
                          {"example.invalid": "rotated.invalid", "rotated.invalid": "192.0.2.2"}):
                source.write_text(yaml.safe_dump({"proxies": [node()], "hosts": hosts}), encoding="utf-8")
                result = subprocess.run(command, capture_output=True, text=True)
                self.assertEqual(result.returncode, 0, result.stderr)
                artifacts = {path.name: path.read_bytes() for path in output.iterdir() if path.name != "config.yaml"}
                if baseline is None:
                    baseline = artifacts
                self.assertEqual(artifacts, baseline)
                configs.append((output / "config.yaml").read_bytes())
            self.assertEqual(configs[0], configs[1])
            self.assertNotEqual(configs[1], configs[2])
            source.write_text(yaml.safe_dump({"proxies": [node()], "hosts": {
                "example.invalid": "https://subscription.invalid/SECRET"}}), encoding="utf-8")
            failed = subprocess.run(command, capture_output=True, text=True)
            self.assertEqual(failed.returncode, 1)
            self.assertNotIn("SECRET", failed.stdout + failed.stderr)
            self.assertEqual((output / "config.yaml").read_bytes(), configs[-1])
            self.assertEqual(baseline, {path.name: path.read_bytes() for path in output.iterdir() if path.name != "config.yaml"})

    def test_dns_opt_in_and_bootstrap(self):
        self.assertIsNone(render.resolver_config(None))
        servers = ["https://1.1.1.1/dns-query", "https://8.8.8.8/dns-query"]
        dns = render.resolver_config(servers)
        self.assertEqual(dns["nameserver"], servers)
        self.assertEqual(dns["proxy-server-nameserver"], servers)
        self.assertEqual(dns["default-nameserver"], servers)
        self.assertTrue(dns["enable"])
        self.assertEqual(dns["enhanced-mode"], "redir-host")
        self.assertEqual(dns["listen"], "")
        self.assertFalse(dns["respect-rules"])
        self.assertFalse(dns["ipv6"])
        self.assertFalse(dns["use-system-hosts"])
        self.assertNotIn("fake-ip-range", dns)
        self.assertEqual(render.resolver_config(["https://resolver.example/dns-query"])["default-nameserver"], servers)
        self.assertEqual(render.resolver_config(["https://resolver.example/dns-query"], ["192.0.2.53"])["default-nameserver"], ["192.0.2.53"])
        for server in ["192.0.2.53", "2001:db8::53", "https://[2001:db8::53]:443/dns-query"]:
            self.assertTrue(render.dns_endpoint(server))

    def test_dns_invalid_values_are_redacted(self):
        invalid = ["", "system", "http://example.org/SECRET", "https://user:SECRET@example.org/dns-query",
                   "https://example.org/dns-query?token=SECRET", "https://example.org/dns-query#SECRET",
                   "https://example.org:99999/SECRET", "https://example.org/SECRET\n",
                   "https://example.org/SECRET\x7f", "https://bad_host/SECRET", "https://example.org",
                   "https://example.org:/SECRET", "https://example.org/%0aSECRET"]
        for server in invalid:
            with self.subTest(server=server), self.assertRaises(render.SafeError) as raised:
                render.resolver_config([server])
            self.assertNotIn("SECRET", str(raised.exception))
        with self.assertRaises(render.SafeError):
            render.resolver_config(None, ["192.0.2.53"])
        with self.assertRaises(render.SafeError):
            render.resolver_config(["192.0.2.53"], ["https://resolver.example/dns-query"])

    def test_metadata_and_unfamiliar_names(self):
        labels = ["到期时间：2099-01-01", "距离下次重置：10天", "获取最新订阅", "剩余流量：1GB", "Remaining: 1 GB"]
        entries = [node(label) for label in labels] + [node("Unknown real location"), node("US remaining node")]
        parsed = render.parse_nodes(yaml.safe_dump({"proxies": entries, "script": {"code": "ignored"}, "rules": ["MATCH,DIRECT"]}))
        self.assertEqual([entry["name"] for entry in parsed], ["Unknown real location", "US remaining node"])

    def test_invalid_and_unsupported_fail_without_secrets(self):
        cases = ["password: [SYNTHETIC-SECRET", "proxies: &x [*x]",
                 yaml.safe_dump({"proxies": [{**node(), "type": "socks5"}]}),
                 yaml.safe_dump({"proxies": [{**node(), "dialer-proxy": "SYNTHETIC-SECRET"}]}),
                 yaml.safe_dump({"proxies": [node(), node()]})]
        for raw in cases:
            with self.subTest(raw=raw):
                with self.assertRaises(render.SafeError) as raised:
                    render.parse_nodes(raw)
                self.assertNotIn("SYNTHETIC-SECRET", str(raised.exception))

    def test_stability_rotation_retirement_and_return(self):
        nodes = [node("first"), node("second")]
        original = render.allocate(nodes, None, 24000, 24010)
        self.assertEqual(original, render.allocate(list(reversed(nodes)), original, 24000, 24010))
        rotated = copy.deepcopy(nodes)
        rotated[0]["password"] = "ROTATED"
        self.assertEqual(original, render.allocate(rotated, original, 24000, 24010))
        changed = render.allocate([nodes[1], node("new")], original, 24000, 24010)
        self.assertEqual(changed["nodes"][-1]["port"], 24002)
        retired = next(item for item in changed["nodes"] if item["name"] == "first")
        self.assertFalse(retired["active"])
        returned = render.allocate(nodes, changed, 24000, 24010)
        self.assertEqual(next(item for item in returned["nodes"] if item["name"] == "first")["port"], retired["port"])
        with self.assertRaises(render.SafeError):
            render.allocate([node("replacement")], render.allocate([node()], None, 24000, 24000), 24000, 24000)

    def test_config_and_export_addresses(self):
        nodes = [node()]
        manifest = render.allocate(nodes, None, 24000, 24001)
        config, entries = render.construct(nodes, manifest, "node-proxy")
        self.assertEqual(config["listeners"][0]["proxy"], config["proxies"][0]["name"])
        self.assertEqual(config["proxies"][0]["type"], "anytls")
        self.assertEqual(config["proxies"][0]["password"], "SYNTHETIC-SECRET")
        self.assertEqual(entries[0]["host"], "node-proxy")
        self.assertEqual(entries[0]["protocol"], "http")
        self.assertNotIn("password", entries[0])
        self.assertNotIn("SYNTHETIC-SECRET", json.dumps(manifest))
        self.assertNotIn("external-controller", config)
        self.assertNotIn("tun", config)
        self.assertEqual(render.construct(nodes, manifest, "127.0.0.1")[1][0]["host"], "127.0.0.1")

    def test_cli_idempotence_exports_and_private_modes(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "synthetic.yaml"
            source.write_text(yaml.safe_dump({"proxies": [node()]}), encoding="utf-8")
            output = root / "generated"
            command = [sys.executable, str(Path(render.__file__)), "--input", str(source), "--output-dir", str(output)]
            first = subprocess.run(command, capture_output=True, text=True)
            self.assertEqual(first.returncode, 0, first.stderr)
            before = {path.name: path.read_bytes() for path in output.iterdir()}
            second = subprocess.run(command, capture_output=True, text=True)
            self.assertEqual(second.returncode, 0, second.stderr)
            self.assertEqual(before, {path.name: path.read_bytes() for path in output.iterdir()})
            self.assertNotIn("dns", yaml.safe_load((output / "config.yaml").read_text()))
            dns_result = subprocess.run(command + ["--dns-server", "https://1.1.1.1/dns-query",
                                                   "--dns-server", "https://8.8.8.8/dns-query"], capture_output=True, text=True)
            self.assertEqual(dns_result.returncode, 0, dns_result.stderr)
            after_dns = {path.name: path.read_bytes() for path in output.iterdir()}
            self.assertEqual({key: value for key, value in before.items() if key != "config.yaml"},
                             {key: value for key, value in after_dns.items() if key != "config.yaml"})
            dns_config = yaml.safe_load((output / "config.yaml").read_text())
            self.assertTrue(dns_config["dns"]["enable"])
            self.assertNotIn("tun", dns_config)
            self.assertNotIn("external-controller", dns_config)
            invalid_dns = subprocess.run(command + ["--dns-server", "https://example.org/dns-query?token=SECRET"],
                                         capture_output=True, text=True)
            self.assertEqual(invalid_dns.returncode, 1)
            self.assertNotIn("SECRET", invalid_dns.stdout + invalid_dns.stderr)
            self.assertEqual(after_dns, {path.name: path.read_bytes() for path in output.iterdir()})
            before = after_dns
            imported = json.loads((output / "server.import.json").read_text())
            self.assertEqual(imported["data"]["accounts"], [])
            self.assertEqual(imported["data"]["proxies"][0]["host"], "node-proxy")
            if os.name != "nt":
                self.assertEqual((output / "config.yaml").stat().st_mode & 0o777, 0o600)
            source.write_text("proxies: [SYNTHETIC-SECRET", encoding="utf-8")
            failed = subprocess.run(command, capture_output=True, text=True)
            self.assertNotEqual(failed.returncode, 0)
            self.assertNotIn("SYNTHETIC-SECRET", failed.stdout + failed.stderr)
            self.assertEqual(before, {path.name: path.read_bytes() for path in output.iterdir()})

    def test_network_exception_is_redacted(self):
        with patch.object(sys, "argv", ["render.py", "--subscription-url-env", "EXAMPLE_URL", "--output-dir", "unused"]), \
                patch.object(render, "render", side_effect=RuntimeError("https://secret.invalid/token")), \
                contextlib.redirect_stderr(io.StringIO()) as errors:
            self.assertEqual(render.main(), 1)
        self.assertNotIn("secret.invalid", errors.getvalue())

    def test_manifest_corruption_rejected(self):
        manifest = render.allocate([node()], None, 24000, 24010)
        manifest["nodes"].append(dict(manifest["nodes"][0]))
        with self.assertRaises(render.SafeError):
            render.allocate([node()], manifest, 24000, 24010)

    @unittest.skipUnless(shutil.which("docker"), "Docker Compose CLI unavailable")
    def test_compose_paths_network_and_loopback_only(self):
        deploy = Path(__file__).resolve().parents[1]
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            config = root / "config.yaml"
            config.write_text("rules: [MATCH,REJECT]\n", encoding="utf-8")
            environment_file = root / "synthetic.env"
            environment_file.write_text("POSTGRES_PASSWORD=synthetic-only\n", encoding="utf-8")
            environment = {key: value for key, value in os.environ.items()
                           if key.upper() in {"PATH", "SYSTEMROOT", "COMSPEC", "TEMP", "TMP", "HOME", "USERPROFILE",
                                              "APPDATA", "LOCALAPPDATA", "PROGRAMDATA", "PROGRAMFILES", "DOCKER_CONFIG"}}
            environment["NODE_PROXY_CONFIG"] = str(config)
            command = ["docker", "compose", "--project-directory", str(deploy),
                       "--env-file", str(environment_file), "-p", "synthetic-node-proxy",
                       "-f", str(deploy / "docker-compose.yml"), "-f", str(deploy / "mihomo/compose.yml")]
            for local in (False, True):
                arguments = command + (["-f", str(deploy / "mihomo/compose.local.yml")] if local else [])
                result = subprocess.run(arguments + ["config", "--format", "json"], env=environment,
                                        capture_output=True, text=True, timeout=30)
                self.assertEqual(result.returncode, 0, result.stderr)
                document = json.loads(result.stdout)
                sidecar = document["services"]["node-proxy"]
                self.assertIn("sub2api-network", sidecar["networks"])
                self.assertIn("sub2api-network", document["services"]["sub2api"]["networks"])
                self.assertEqual(document["networks"]["sub2api-network"]["name"], "synthetic-node-proxy_sub2api-network")
                self.assertEqual(Path(sidecar["volumes"][0]["source"]), config)
                self.assertTrue(sidecar["volumes"][0]["read_only"])
                self.assertNotIn("network_mode", sidecar)
                if local:
                    self.assertTrue(sidecar["ports"])
                    self.assertTrue(all(port["host_ip"] == "127.0.0.1" for port in sidecar["ports"]))
                else:
                    self.assertNotIn("ports", sidecar)


if __name__ == "__main__":
    unittest.main()
