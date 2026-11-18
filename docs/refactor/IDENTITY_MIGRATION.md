# OmniHive deployment identity migration v1

The first independent releases use the `omnihive` executable and `omnihive_<version>_<os>_<arch>.tar.gz` release archives. Releases and the update panel use the OmniHive repository. The native installer and panel accept the previous `omnihive-ai-gw` and `sub2api` archive names from that same release source. Current archives require an entry in `checksums.txt`. Existing release headers, the native installer and GoReleaser agree on `<version>` without the `v` prefix. The Windows release build continues to publish a ZIP for manual installation.

## Native systemd installations

New installations use `omnihive.service`, the `omnihive` system account, `/opt/omnihive/omnihive`, `/etc/omnihive/config.yaml` and `/opt/omnihive/data`. Take a recoverable backup of application files and PostgreSQL before upgrading. Preserve deployment-specific systemd overrides, database connection details, JWT and TOTP keys and any data encryption keys.

On `install`, `upgrade` or `rollback`, the installer detects an existing legacy binary under `/opt/sub2api` when the current installation is absent. The transition installs the new basename in the existing directory and retains a legacy executable link for the existing unit. It leaves that unit, service account, and data paths in use to preserve local overrides and ownership. The installer logs the selected legacy identity and retains the previous binary for recovery.

To move an existing host to the new service and directories, schedule a cutover. Stop the old service, back up its unit and overrides, copy the files and secrets into the new layout, and set ownership for the new system user. Verify the service reads the existing database and data directory before enabling it on boot. Keep the earlier unit and files available until the application and data checks pass. Cutover and rollback should run one unit at a time against the database.

## Docker Compose deployments

Keep the original Compose project name, PostgreSQL and Redis volume bindings, and `.env`. If your existing environment relies on defaults, record the actual database name and user from the old deployment and set those values explicitly in the new configuration. Copy the new templates separately and compare settings before cutover.

For an existing named application volume, obtain its exact physical name from the old container mounts. Set `OMNIHIVE_DATA_VOLUME` to that name and `OMNIHIVE_DATA_VOLUME_EXTERNAL=true`. The new Compose file then requires the existing volume, so a misspelling cannot silently initialize empty data. Bind-mounted deployments should keep the original host path and use the same project directory.

Stop the existing stack with its original Compose file before launching the new `omnihive` service. Keep the named volumes and verify the rendered Compose configuration resolves the expected volume and database settings. Inspect the existing data and authentication after startup. If migrating from a legacy image, the new entrypoint leaves a legacy `.updates/current/sub2api` file in the volume and starts the image executable. For subsequent explicit image upgrades, move any current panel-staged `omnihive` executable into a backup before starting the new image if the image should take precedence.

## Apple Container deployments

The Apple Container script creates `omnihive-apple` resources for new deployments. If it finds a legacy stack, it uses that stack's containers, network, ownership label and volumes and logs the fallback. It requires an explicit resolution when both sets of resources exist. The new `OMNIHIVE_ENV_FILE` and `APPLE_CONTAINER_OMNIHIVE_IMAGE` keys take precedence over their deprecated counterparts. A configured image change replaces the persistent executable while preserving the application volume.

## Configuration precedence

`OMNIHIVE_DATA_DIR` is shared by setup, logging, configuration lookup and the update panel. `DATA_DIR` remains a deprecated fallback. `CONFIG_FILE` selects an explicit configuration and is also the setup wizard's write destination when configured. When no explicit file is configured, the existing search order is used with `/etc/omnihive` ahead of `/etc/sub2api`. Existing deployments can continue using an explicit data location. New default log files use `omnihive.log`; older logs can remain as an archive.

The same precedence applies to pairs of `OMNIHIVE_*` and `SUB2API_*` variables for image model selection, profiling, the Claude CLI pin and gateway debugging. Applications log deprecated fallback use without logging values. Update and database environment keys that were already product-independent keep their existing names. PostgreSQL and Redis state, installed credentials and coordinated namespaces stay on their current identities through the transition.

The signed release manifest, plugin v2 and coordinated database, browser and protocol migrations are tracked in the [identity inventory](IDENTITY_INVENTORY.md). The transition window should be set against the active deployment inventory before retiring the compatibility paths.
