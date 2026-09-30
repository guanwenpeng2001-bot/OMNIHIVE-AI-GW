"""Synthetic protocol and multi-source coverage; optional core syntax checks only."""
import base64
import copy
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

import yaml
import render


def protocol_nodes():
    common = {"server": "node.example.invalid", "port": 443, "udp": True}
    identifier = "00000000-0000-4000-8000-000000000001"
    return [
        {**common, "name": "anytls", "type": "anytls", "password": "SYNTHETIC", "sni": "tls.example.invalid", "skip-cert-verify": False},
        {**common, "name": "ss", "type": "ss", "password": "SYNTHETIC", "cipher": "aes-128-gcm"},
        {**common, "name": "trojan", "type": "trojan", "password": "SYNTHETIC", "sni": "tls.example.invalid",
         "skip-cert-verify": False, "network": "grpc", "grpc-opts": {"grpc-service-name": "synthetic-service"}},
        {**common, "name": "vmess", "type": "vmess", "uuid": identifier, "alterId": 0, "cipher": "auto",
         "tls": True, "servername": "tls.example.invalid", "skip-cert-verify": False},
        {**common, "name": "vless-ws", "type": "vless", "uuid": identifier, "tls": True, "servername": "tls.example.invalid",
         "network": "ws", "ws-opts": {"path": "/synthetic", "headers": {"Host": "ws.example.invalid"}}},
        {**common, "name": "vless-reality", "type": "vless", "uuid": identifier, "tls": True, "servername": "tls.example.invalid",
         "flow": "xtls-rprx-vision", "client-fingerprint": "chrome", "reality-opts": {
             "public-key": base64.urlsafe_b64encode(bytes([9]) + bytes(31)).decode().rstrip("="), "short-id": "abcd"}},
        {**common, "name": "hysteria2", "type": "hysteria2", "password": "SYNTHETIC", "sni": "tls.example.invalid",
         "skip-cert-verify": False, "obfs": "salamander", "obfs-password": "SYNTHETIC-OBFS", "ports": "443,8443-8445"},
    ]


def subscription(nodes, **extra):
    # JSON round trip prevents fixture object sharing from introducing YAML aliases.
    return yaml.safe_dump(json.loads(json.dumps({"proxies": nodes, **extra})))


class ProtocolTests(unittest.TestCase):
    def test_six_sources_dedupe_and_upper_range_expansion(self):
        template = protocol_nodes()[0]
        def distinct(prefix, count):
            return [{**template, "name": f"{prefix}-{index}", "server": f"{prefix}{index}.example.invalid"} for index in range(count)]
        original, longterm, lanpang = distinct("original", 87), distinct("longterm", 34), distinct("lanpang", 52)
        mixed = [{**node, "name": "alias-" + node["name"]} for node in lanpang[:21]] + distinct("mixed", 28)
        new_anytls = distinct("newanytls", 88)
        duplicate_longterm = [{**node, "name": "alias-" + node["name"]} for node in longterm[:32]] + distinct("newlongterm", 2)
        initial = render.allocate(original, None, 24000, 24199)
        additions = [(label, subscription(nodes)) for label, nodes in [
            ("长期订阅", longterm), ("蓝胖云", lanpang), ("微信混合订阅", mixed),
            ("微信AnyTLS订阅", new_anytls), ("微信长期订阅", duplicate_longterm)]]
        combined, hosts, summary = render.combine_sources(subscription(original), additions, initial)
        self.assertEqual(len(combined), 291)
        self.assertEqual(summary["duplicates"], 53)
        self.assertEqual(sum(item["real_nodes"] for item in summary["sources"]), 344)
        self.assertNotIn("SYNTHETIC", json.dumps(summary))
        self.assertEqual(hosts, {})
        expanded = render.allocate(combined, initial, 24000, 24399)
        self.assertEqual(expanded["nodes"][:87], initial["nodes"])
        self.assertEqual(expanded["nodes"][-1]["port"], 24290)
        reordered, _, reordered_summary = render.combine_sources(subscription(original), additions[::-1], expanded)
        self.assertEqual(render.allocate(reordered, expanded, 24000, 24399), expanded)
        self.assertEqual(reordered_summary, summary)
        for start, end in [(23999, 24399), (24000, 24398)]:
            with self.assertRaises(render.SafeError):
                render.allocate(combined, expanded, start, end)
        retired = render.allocate(original, expanded, 24000, 24399)
        returned = render.allocate(combined, retired, 24000, 24499)
        self.assertEqual(returned["nodes"], expanded["nodes"])

    def test_existing_canonical_wins_over_new_label_and_credentials_distinguish(self):
        original = protocol_nodes()[0]
        added = {**original, "server": "additional.invalid"}
        primary = subscription([original])
        merged, _, _ = render.combine_sources(primary, [("Zed", subscription([added]))])
        manifest = render.allocate(merged, None, 24000, 24199)
        incoming = {**added, "name": "other-display"}
        combined, _, summary = render.combine_sources(primary, [("Alpha", subscription([incoming])), ("Zed", subscription([added]))], manifest)
        self.assertEqual(render.allocate(combined, manifest, 24000, 24199), manifest)
        self.assertEqual(summary["duplicates"], 1)
        self.assertEqual(next(item for item in summary["aliases"] if item["source"] == "Alpha")["canonical_name"], "[Zed] anytls")
        incoming["password"] = "DIFFERENT-SYNTHETIC"
        distinct_nodes, _, distinct_summary = render.combine_sources(primary, [("Alpha", subscription([incoming])), ("Zed", subscription([added]))], manifest)
        self.assertEqual(len(distinct_nodes), 3)
        self.assertEqual(distinct_summary["duplicates"], 0)

    def test_all_families_and_nested_settings_preserved(self):
        nodes = protocol_nodes()
        parsed, hosts = render.parse_subscription(subscription(nodes, rules=["MATCH,DIRECT"], script={"code": "ignored"},
                                                              dns={"nameserver": ["secret.invalid"]}, hosts={"unrelated.invalid": "127.0.0.1"}))
        self.assertEqual(parsed, nodes)
        self.assertEqual(hosts, {})
        config, entries = render.construct(parsed, render.allocate(parsed, None, 24000, 24199), "node-proxy")
        self.assertEqual(config["rules"], ["MATCH,REJECT"])
        self.assertNotIn("dns", config)
        self.assertNotIn("script", config)
        self.assertTrue(all(entry["protocol"] == "http" for entry in entries))

    def test_protocol_required_fields_and_bad_nested_values(self):
        for node in protocol_nodes():
            required = ["server", "port", "uuid" if node["type"] in {"vmess", "vless"} else "password"]
            if node["type"] in {"ss", "vmess"}:
                required.append("cipher")
            if node["type"] == "vmess":
                required.append("alterId")
            for field in required:
                invalid = {key: value for key, value in node.items() if key != field}
                with self.subTest(protocol=node["type"], field=field), self.assertRaises(render.SafeError):
                    render.parse_nodes(subscription([invalid]))
        invalid_nodes = [
            {**protocol_nodes()[4], "ws-opts": {"headers": {"Host": ["SECRET"]}}},
            {**protocol_nodes()[2], "grpc-opts": {"script": "SECRET"}},
            {**protocol_nodes()[5], "tls": False},
            {**protocol_nodes()[5], "reality-opts": {"public-key": "SECRET"}},
            {**protocol_nodes()[3], "uuid": "SECRET"},
            {**protocol_nodes()[3], "alterId": True},
            {**protocol_nodes()[-1], "obfs-password": ""},
        ]
        for invalid in invalid_nodes:
            with self.assertRaises(render.SafeError) as raised:
                render.parse_nodes(subscription([invalid]))
            self.assertNotIn("SECRET", str(raised.exception))

    def test_hysteria_mport_normalization_and_conflicts(self):
        original = protocol_nodes()[-1]
        for ports, mport in [("443,8443-8445", "443,8443-8445"), (None, "443,8443-8445"), ("443,8443-8445", "")]:
            node = {**original, "mport": mport}
            if ports is None:
                del node["ports"]
            self.assertEqual(render.parse_nodes(subscription([node]))[0], original)
        for invalid in ["0", "65536", "500-400", "443:445", "SECRET"]:
            with self.assertRaises(render.SafeError):
                render.parse_nodes(subscription([{**original, "mport": invalid}]))
        with self.assertRaises(render.SafeError):
            render.parse_nodes(subscription([{**original, "mport": "500-600"}]))

    def test_multisource_173_nodes_preserve_existing_87_ports(self):
        base_node = protocol_nodes()[0]
        original = [{**base_node, "name": f"original-{index}"} for index in range(87)]
        fixture_by_type = {node["type"]: node for node in protocol_nodes()}
        profiles = []
        for counts in ({"trojan": 30, "anytls": 2, "hysteria2": 3},
                       {"vmess": 5, "trojan": 19, "vless": 22, "hysteria2": 7, "ss": 2}):
            profiles.append([{**fixture_by_type[protocol], "name": f"{protocol}-{index}", "server": f"{protocol}{index}.extra.invalid"}
                             for protocol, count in counts.items() for index in range(count)])
        profiles[0][0]["name"] = "有效期2027-12-15, 剩余 64 GB"
        for index, label in enumerate(["剩余流量：86 GB", "套餐到期：2027-03-05", "最新官网:example.invalid"]):
            profiles[1][index]["name"] = label
        self.assertEqual(len(render.parse_nodes(subscription(profiles[0]))), 34)
        self.assertEqual(len(render.parse_nodes(subscription(profiles[1]))), 52)
        # Distinct real sources can share an endpoint/name but have different credentials.
        for node in profiles[1]:
            if "password" in node:
                node["password"] = "OTHER-SYNTHETIC"
        original_manifest = render.allocate(original, None, 24000, 24199)
        additions = [("Longterm", subscription(profiles[0])), ("Lanpang", subscription(profiles[1]))]
        combined, _, _ = render.combine_sources(subscription(original), additions)
        self.assertEqual(len(combined), 173)
        self.assertEqual(sorted(combined[:87], key=lambda node: node["name"]), sorted(original, key=lambda node: node["name"]))
        manifest = render.allocate(combined, original_manifest, 24000, 24199)
        self.assertEqual(manifest["nodes"][:87], original_manifest["nodes"])
        self.assertEqual(manifest["nodes"][-1]["port"], 24172)
        reordered, _, _ = render.combine_sources(subscription(original), list(reversed(additions)))
        self.assertEqual(render.allocate(reordered, manifest, 24000, 24199), manifest)
        rotated = copy.deepcopy(combined)
        for node in rotated:
            if "password" in node:
                node["password"] += "-ROTATED"
        self.assertEqual(render.allocate(rotated, manifest, 24000, 24199), manifest)
        for host in ("node-proxy", "127.0.0.1"):
            before = render.construct(original, original_manifest, host)[1]
            after = render.construct(combined, manifest, host)[1]
            self.assertEqual(after[:87], before)

    def test_labels_exact_duplicates_and_host_conflicts(self):
        original = protocol_nodes()[0]
        different = {**original, "password": "OTHER-SYNTHETIC"}
        combined, _, _ = render.combine_sources(subscription([original]), [("Extra", subscription([different]))])
        self.assertEqual([node["name"] for node in combined], ["anytls", "[Extra] anytls"])
        duplicates, _, summary = render.combine_sources(subscription([original]), [("Extra", subscription([original]))])
        self.assertEqual(len(duplicates), 1)
        self.assertEqual(summary["duplicates"], 1)
        for additions in [[("bad=label", subscription([different]))], [("primary", subscription([different]))],
                          [("Extra", subscription([different])), ("extra", subscription([different]))]]:
            with self.assertRaises(render.SafeError):
                render.combine_sources(subscription([original]), additions)
        renamed = {**different, "name": "another"}
        with self.assertRaises(render.SafeError):
            render.combine_sources(subscription([original], hosts={"node.example.invalid": "192.0.2.1"}),
                                   [("Extra", subscription([renamed], hosts={"node.example.invalid": "192.0.2.2"}))])
        with self.assertRaises(render.SafeError):
            render.combine_sources(subscription([{**original, "name": "[Extra] anytls"}]), [("Extra", subscription([different]))])
        names = ["有效期优化香港", "最新官网线路日本", "US remaining node"]
        self.assertEqual(len(render.parse_nodes(subscription([{**original, "name": name} for name in names]))), 3)

    def test_additional_input_cli_and_secret_free_error(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "primary.yaml").write_text(subscription([protocol_nodes()[0]]), encoding="utf-8")
            (root / "extra.yaml").write_text(subscription(protocol_nodes()[1:]), encoding="utf-8")
            command = [sys.executable, render.__file__, "--input", str(root / "primary.yaml"),
                       "--additional-input", "Extra=" + str(root / "extra.yaml"), "--output-dir", str(root / "output")]
            result = subprocess.run(command, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            entries = json.loads((root / "output/server.create.json").read_text())
            self.assertEqual(len(entries), 7)
            self.assertEqual(sum(entry["name"].startswith("[Extra] ") for entry in entries), 6)
            failed = subprocess.run(command + ["--additional-input", "SECRET"], capture_output=True, text=True)
            self.assertEqual(failed.returncode, 1)
            self.assertNotIn("SECRET", failed.stdout + failed.stderr)

    @unittest.skipUnless(os.environ.get("MIHOMO_SCHEMA_TEST_IMAGE"), "Set MIHOMO_SCHEMA_TEST_IMAGE for core syntax checks")
    def test_all_protocols_core_syntax_without_network(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fixtures = protocol_nodes()
            fixtures[-1]["mport"] = fixtures[-1]["ports"]
            nodes, hosts = render.parse_subscription(subscription(fixtures, hosts={"node.example.invalid": "alias.example.invalid"}))
            config, _ = render.construct(nodes, render.allocate(nodes, None, 24000, 24199), "node-proxy",
                                         render.resolver_config(["https://1.1.1.1/dns-query"]), hosts)
            self.assertEqual(config["dns"]["proxy-server-nameserver"], ["udp://127.0.0.1:1053"])
            self.assertNotIn("mport", yaml.safe_dump(config))
            (root / "config.yaml").write_text(yaml.safe_dump(config), encoding="utf-8")
            result = subprocess.run(["docker", "run", "--rm", "--pull", "never", "--network", "none", "--read-only",
                                     "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--tmpfs", "/tmp",
                                     "--mount", f"type=bind,source={root},target=/fixtures,readonly",
                                     os.environ["MIHOMO_SCHEMA_TEST_IMAGE"], "-t", "-d", "/tmp/mihomo", "-f", "/fixtures/config.yaml"],
                                    capture_output=True, text=True, timeout=30)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertIn("test is successful", result.stdout)


if __name__ == "__main__":
    unittest.main()
