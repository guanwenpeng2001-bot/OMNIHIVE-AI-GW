"""Synthetic-only verification; does not start services or contact subscriptions."""
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
                 yaml.safe_dump({"proxies": [{**node(), "type": "trojan"}]}),
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
