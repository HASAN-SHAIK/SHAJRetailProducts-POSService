# POS Service Packaging and Installation

The store-local service is deployed as one native process plus one SQLite database. It is not deployed as multiple local microservices.

## Runtime layout

Production installations should keep binaries, mutable data, backups, and the local API token separate:

- Binary: `/opt/shajretail-pos/shajretail-pos` or `%ProgramFiles%\SHAJRetail\POSService\shajretail-pos.exe`
- Database: `/var/lib/shajretail-pos/shajretail-pos.db` or `%ProgramData%\SHAJRetail\POSService\shajretail-pos.db`
- Token: beside the database with owner-only permissions
- Backups: a separate backup directory with owner-only permissions

The service must bind only to loopback. Never expose port 4782 on the store LAN or Internet.

## Build

Use `scripts/build-release.sh`. Every release artifact is accompanied by `SHA256SUMS.txt` and `RELEASE-MANIFEST.txt`.

The release manifest binds the artifact set to the exact source commit and version:

- `version` is the release version supplied to the build;
- `git_commit` is an exact 40-character Git commit SHA, resolved from the checkout unless explicitly supplied by CI;
- `checksums` points to `SHA256SUMS.txt`, which must verify before installation.

Release automation must retain the manifest and checksum file beside the installers/binaries. Do not relabel an artifact with a version or commit different from the manifest. CGO is enabled because the SQLite driver is native.

## First installation

1. Create a dedicated OS service account.
2. Install the binary into the immutable application directory.
3. Create data and backup directories writable only by the service account.
4. Copy `packaging/pos.env.example` and customize the central API URL and allowed browser origins.
5. Start the service and verify `/api/v1/health` and `/api/v1/ready` over loopback.
6. Preserve the generated local API token; the frontend adapter uses it to authenticate to the local service.

## Upgrade

1. Create or confirm a recent verified SQLite backup.
2. Stop the service.
3. Verify the incoming artifact checksum against `SHA256SUMS.txt` and retain `RELEASE-MANIFEST.txt` for the deployed version.
4. Replace only the binary/package files. Never replace the SQLite database or token during a normal upgrade.
5. Start the service. Versioned migrations run automatically before readiness becomes healthy.
6. Verify readiness and outbox health before considering the upgrade complete.

If startup or migration validation fails, stop the new binary and restore the previous binary. Restore the database only when there is evidence the database itself was damaged; follow `docs/RECOVERY.md`.

## Uninstall

An uninstall should remove the service registration and executable but retain `%ProgramData%`/`/var/lib` by default. Local database, token, and backups are business records and must be deleted only through an explicit data-destruction operation.

## Release signing

Production distribution should add platform signing in CI: Authenticode for Windows and the appropriate signing/notarization flow for macOS. Signing credentials must live in the CI secret store, not in this repository.
