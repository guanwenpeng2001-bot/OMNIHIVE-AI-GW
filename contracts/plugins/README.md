# OmniHive extension SDK v2

An `.ohplugin` is a ZIP containing `manifest.json`, `signature.json`, one WASI Preview 1 command module and the declared static assets. The host verifies the Ed25519 signature on the exact manifest bytes, checks each SHA-256 payload digest, and installs the package disabled. The v2 publisher namespace and keys are local to the deployment.

## Build a plugin

From the repository's `backend` directory, build the [working example](../../backend/pkg/pluginapi/v2/example/main.go).

```sh
mkdir -p build/example/ui
GOOS=wasip1 GOARCH=wasm CGO_ENABLED=0 go build -o build/example/runtime.wasm ./pkg/pluginapi/v2/example
echo '<!doctype html><title>Example</title>' >build/example/ui/index.html
go build -o build/ohplugin ./pkg/pluginapi/v2/cmd/ohplugin
```

Create `build/example/manifest.json` using the [v2 manifest schema](../../backend/pkg/pluginapi/v2/manifest.schema.json). Set the host range and tested versions to releases you have verified. Replace each file-hash placeholder with the lowercase digest of that file from `sha256sum`.

```json
{
  "schema_version": 2, "product_id": "omnihive",
  "publisher_id": "team-2026", "id": "com.example.openai-transport",
  "name": "Example", "version": "0.1.0",
  "requires": {
    "omnihive": ">=0.1.170 <0.2.0", "plugin_protocol": 2,
    "transport_api": 1, "ui_bridge": 1
  },
  "capabilities": [
    {"id":"provider","platform":"openai","account_type":"oauth"},
    {"id":"ui","platform":"","account_type":""}
  ],
  "runtimes": {"wasip1-wasm":{"path":"runtime.wasm"}},
  "ui": {"entrypoint":"ui/index.html"},
  "files": {"runtime.wasm":"<module-sha256>","ui/index.html":"<ui-sha256>"}
}
```

The [protobuf contract](../../backend/pkg/pluginapi/v2/extension.proto) uses package and handshake identifier `omnihive.plugin.v2`. A fresh WASI instance receives one serialized `Request` on stdin. Its only stdout output is one serialized `Reply`. The Go SDK writes the ID, version and complete capability list on each reply. The host checks these against the signed manifest.

| Operation | Input | Host action |
| --- | --- | --- |
| `start` | Handshake | Verify runtime identity and signed declarations before activation |
| `configure` | JSON object | Validate, bound, normalize and encrypt the accepted configuration |
| `test` | Saved configuration | Return the guest's acceptance |
| `authorize` | Host-selected capability and configuration | An accepted request may continue through the existing host action |

`provider` is scoped to OpenAI OAuth, `task` reserves the host authorization contract for BE-26, and `ui` authorizes signed UI resources. A plugin with no UI omits the UI entrypoint. Each capability requires a publisher/ID-specific administrator grant and an enabled installation binding. The module gets no socket, preopened directory, environment variables, wallet fields, request headers, account secrets or redirect controls. The host selects destinations, executes outbound requests and settles observed usage. A rejected provider request fails closed. Each invocation has a 5-second deadline, a 64 MiB linear-memory limit and a 256 KiB message limit. Each host permits eight concurrent invocations. A trap terminates that invocation. Hot disable stops guest admission, lets active calls finish for up to ten seconds, then closes the runtime. An already authorized host operation finishes under its original gateway context.

## Bootstrap and rotate keys

On the offline signing machine run `build/ohplugin keygen -key team-2026.seed`. This creates a mode-0600 seed file and prints the base64 public key. The signing command reads a file containing the base64 **32-byte seed**. Verify the public key fingerprint and publisher ownership through your existing signed release chain or a separately authenticated operator exchange. Copy the public policy to `<plugins.data_dir>/trust-v2.json` on **every** host and to the signing machine. The service account needs read access. Distribute changes by atomic replacement. V2 accepts signatures only from the local policy, including when development mode permits unsigned v1 packages.

```json
{
  "publishers": {
    "team-2026": {
      "public_key":"<base64-ed25519-public-key>",
      "grants":{"com.example.openai-transport":["provider","ui"]},
      "predecessors":{
        "com.example.openai-transport":{"plugin_id":"com.example.openai-transport","key_id":"team-v1"}
      }
    }
  }
}
```

```sh
build/ohplugin sign -src build/example -trust trust-v2.json -key team-2026.seed -out example.ohplugin
```

For rotation, add the next key under a new publisher ID with the reviewed grants and predecessor mapping. Distribute the overlap policy, sign new artifacts, disable the old release, install the replacement and test activation. Keep the old public key through the rollback window, then remove it from every policy. Admissions recheck the local policy, including a change of public key under an existing ID. Keep private keys outside the service.

## Migrate a v1 package

1. Inventory the original archive, its complete SHA-256, publisher key ID and public fingerprint, plugin ID, account binding, host range, tested releases, encrypted configuration and rollout. Export and retain the original archive in operator-controlled rollback storage.
2. Review the original publisher out of band. Imported v1 public keys use a separate JSON object mapping key ID to base64 public key. The compatibility verifier retains its pinned, plugin-specific official roots. A v1 key gains v2 authority only by explicit entry in the v2 policy. Configure the old and new owner mapping under `predecessors`.
3. Port the implementation to the v2 SDK and rebuild the WASI module. Check whether the original native transport's TLS or proxy behavior can be achieved by the host egress implementation. Keep that cohort on its verified v1 transport until it passes real provider acceptance. Preserve a reviewed UI or update its bridge compatibility before activation.
4. From `backend`, run the offline rebuild with the actual host version. It first verifies the original byte signature and every original payload, then writes a v2 envelope with the rebuilt module, signed UI assets, origin digests and the byte-exact original archive in `provenance.s2plugin`. Every installing or restoring host verifies the original signer and hashes again. Import the same v1 public keys into the service configuration at `plugins.trusted_publishers` on every node. Use a migration machine supported by the original package. Both host namespaces in one v1 manifest trigger manual review. Tested-version claims are cleared for a rebuilt binary.

   ```sh
   build/ohplugin migrate -src original.s2plugin -module build/example/runtime.wasm \
     -id com.example.openai-transport -publisher team-2026 -host 0.1.179 \
     -legacy-roots v1-public-keys.json -trust trust-v2.json \
     -key team-2026.seed -out rebuilt.ohplugin
   sha256sum original.s2plugin rebuilt.ohplugin
   ```

5. Store both archives with the migration record. Compare the signed `origin.archive_sha256`, `origin.manifest_sha256`, previous plugin/key IDs and v2 publisher. Untrusted signatures, ambiguous ownership, corrupt files and incompatible host mappings stop the rebuild before publication. The command verifies the final archive and refuses to overwrite its destination.
6. Disable the old binding, upload the verified v2 artifact, test configuration and inspect compatibility. Enable a small rollout. Verify denied capabilities, egress routing, receipts, UI behavior and in-flight shutdown. Record installation ID, grants and v2 digest from the inventory. The administrator must explicitly accept untested versions.
7. To roll back, disable the v2 installation, reimport the saved original with its v1 signer still trusted, restore the correct configuration and rollout, then re-enable. Keep old signatures, archives and ownership mappings through the BE-30 observation window.
