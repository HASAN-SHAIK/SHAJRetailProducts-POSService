# SHAJRetailProducts V1 Database Migration / Upgrade / Backup / Recovery Acceptance Matrix

Status: **IN PROGRESS**

## Authority boundary

- Central Backend/PostgreSQL remains canonical enterprise persistence and tenant authority.
- POSService/SQLite remains the durable offline edge database and must preserve accepted business facts across restart/upgrade/reconnect.
- Schema upgrades must be deterministic, replay-safe, fail closed on incompatible drift, and must never silently leave some tenant databases behind.
- Backup/recovery mechanisms must preserve tenant/store isolation and must not create a second canonical business authority in Frontend/browser storage.

## Acceptance matrix

| Capability | Current evidence | Status | Release requirement |
|---|---|---|---|
| POS ordered schema migrations | POS embeds numbered SQL migrations and applies them in ascending version order | CERTIFIED | preserve deterministic ordering |
| POS transactional migration apply | each pending SQLite migration executes and records its version/checksum inside one DB transaction | CERTIFIED | failed migration must roll back schema + migration record |
| POS migration checksum drift | `schema_migrations` stores SHA-256 checksums; POS #270 explicitly proves tampered history fails closed | CERTIFIED | historical migration files remain immutable |
| POS durability pragmas | SQLite opens with foreign keys, WAL, synchronous FULL, busy timeout and immediate transaction locking | CERTIFIED | release runtime must retain durability/concurrency pragmas |
| POS integrity check | POS #270 proves `PRAGMA quick_check` succeeds after fresh migration and after failed migration rollback; POS #272 validates backup/restore candidates | CERTIFIED | preserve startup and restore integrity validation |
| POS fresh-install migration | POS #270 migrates an empty SQLite file through every embedded migration and verifies the exact version/name/checksum ledger | CERTIFIED | preserve complete embedded migration acceptance |
| POS upgrade migration | POS #275 seeds the immediately previous embedded schema with durable metadata/outbox/inbox/local-auth facts, runs the production migration engine to current, and verifies the full ledger + preserved facts | CERTIFIED | preserve representative N→current data retention acceptance |
| POS migration rerun/idempotency | POS #270 reruns the current migration set and verifies no duplicate ledger/schema work | CERTIFIED | preserve restart/rerun idempotency |
| POS migration failure recovery | POS #270 proves failed transactional schema/data work rolls back and the database remains integrity-checkable | CERTIFIED | preserve rollback-before-version-record behavior |
| POS SQLite backup | existing `VACUUM INTO` snapshot is quick-checked, schema-checked, chmod 0600 and atomically renamed; POS #272 certifies the real snapshot | CERTIFIED | preserve WAL-safe verified snapshot publication |
| POS SQLite restore/recovery | POS #272 validates a snapshot, restores it into a clean runtime, reruns migrations/integrity and proves pending outbox + applied inbox facts survive; corrupt candidate is rejected | CERTIFIED | restore remains an offline operator action with pre-restore forensic copy |
| Central tenant migration execution | Backend #94 applies the supplied migration to every selected tenant, aggregates failures and fails the overall command when any tenant fails | CERTIFIED | preserve fail-closed fleet result |
| Central tenant migration atomicity | Backend #94 wraps each tenant migration in BEGIN/COMMIT with ROLLBACK on failure | CERTIFIED | preserve per-tenant transaction boundary |
| Central fleet consistency | Backend #94 continues selected tenants for an actionable summary but returns `TENANT_MIGRATION_PARTIAL_FAILURE` when any failed | CERTIFIED | deployment must treat non-zero fleet result as failure |
| Central migration history/drift detection | Backend #95 records migration filename + SHA-256 in `tenant_schema_migrations`; same checksum reruns skip and historical drift rolls back/fails closed | CERTIFIED | migration filenames/content remain immutable after application |
| Central fresh tenant schema | Backend #96 makes the bootstrap path apply an explicit audited V1 overlay list for certified auth/inventory/customer/reporting structures missing from the baseline; real fresh-tenant PostgreSQL smoke remains outstanding | PARTIAL | provision a real fresh tenant at current V1 schema and pass core domain smoke checks |
| Central existing-tenant upgrade | domain PostgreSQL E2Es apply many migrations individually | PARTIAL | run the ordered production migration path against an older representative tenant and verify retained data |
| Central JSON support export | admin-only JSON tenant support export remains a support/data-quality artifact and is not canonical PostgreSQL restore authority | N/A | keep it admin/tenant scoped; canonical recovery uses the native PostgreSQL path |
| Central native backup verification | Backend #97 creates a pg_dump custom archive, chmods archive/manifest 0600, binds SHA-256 + tenant DB identity, and verifies archive readability with `pg_restore --list` | CERTIFIED | verify checksum and archive readability before every restore |
| Central native restore | Backend #97 uses exact tenant confirmation, same-tenant manifest binding, `pg_restore --single-transaction --exit-on-error`, and a post-restore core-table smoke check; real PostgreSQL acceptance preserves canonical facts | CERTIFIED | preserve verified same-tenant transactional restore and post-restore consistency check |
| Cross-tenant recovery safety | Backend #97 rejects restoring a backup manifest into a different tenant database before destructive restore work | CERTIFIED | preserve exact backup-tenant/target binding |
| Upgrade rollback policy | POS preserves pre-restore evidence and validates snapshots; Central now has a verified native restore path but the explicit forward-fix/rollback operator policy remains incomplete | PARTIAL | document and test when to forward-fix versus invoke native restore |
| Release diagnostics | Central migration failures expose exact failed tenants; native backup/restore returns typed fail-closed outcomes; POS validates migration/restore integrity, but the final unified operational checklist remains incomplete | PARTIAL | expose actionable migration/backup/recovery outcomes without secrets or silent partial success |
| Cross-repository release acceptance | no final database-quality release gate yet | GAP | final merged Backend + POS acceptance for fresh install, upgrade, failure, integrity, backup/restore and isolation |

## Current certified evidence

- Backend #94 — fail-closed, transactional fleet tenant migration execution.
- Backend #95 — per-tenant migration history, idempotent rerun and checksum-drift rejection.
- Backend #96 — fresh-tenant bootstrap now applies an explicit audited V1 overlay list for certified schema facts missing from the baseline.
- Backend #97 — native PostgreSQL backup, SHA-256/archive verification, same-tenant transactional restore, tamper rejection, cross-tenant restore rejection and post-restore smoke acceptance.
- POSService #270 — fresh-install migration, exact migration ledger, rerun, checksum drift, transactional failure rollback and integrity acceptance.
- POSService #272 — verified SQLite snapshot, corrupt-candidate rejection and restore preservation of durable outbox/inbox facts.
- POSService #275 — representative previous-schema → current-schema upgrade preserving metadata, pending outbox, applied inbox and local-auth facts.

## Remaining ordered work

1. Run a real fresh-tenant PostgreSQL provisioning smoke and a representative existing-tenant ordered upgrade using the production migration path.
2. Close Central forward-fix/rollback policy and database-quality operational diagnostics.
3. Add final merged-main database migration/recovery release acceptance and mark every row CERTIFIED or explicitly justified N/A.

## Release decision

**NOT YET RELEASE CERTIFIED.** POS fresh-install/rerun/failure/upgrade and verified SQLite backup/restore are certified. Central fleet migrations, checksum history, and native PostgreSQL backup/restore with tenant binding are certified; fresh-tenant schema drift is contained through explicit V1 overlays. Remaining release blockers are real fresh/existing-tenant PostgreSQL upgrade smoke, explicit recovery policy/diagnostics, and the final merged-main database-quality gate.
