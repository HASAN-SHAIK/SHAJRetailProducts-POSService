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
| POS upgrade migration | domain tests exercise upgraded schemas indirectly, but there is no explicit representative N→current preserved-data acceptance | PARTIAL | seed an older schema/data set, run remaining migrations, verify business/outbox/inbox/auth facts survive |
| POS migration rerun/idempotency | POS #270 reruns the current migration set and verifies no duplicate ledger/schema work | CERTIFIED | preserve restart/rerun idempotency |
| POS migration failure recovery | POS #270 proves failed transactional schema/data work rolls back and the database remains integrity-checkable | CERTIFIED | preserve rollback-before-version-record behavior |
| POS SQLite backup | existing `VACUUM INTO` snapshot is quick-checked, schema-checked, chmod 0600 and atomically renamed; POS #272 certifies the real snapshot | CERTIFIED | preserve WAL-safe verified snapshot publication |
| POS SQLite restore/recovery | POS #272 validates a snapshot, restores it into a clean runtime, reruns migrations/integrity and proves pending outbox + applied inbox facts survive; corrupt candidate is rejected | CERTIFIED | restore remains an offline operator action with pre-restore forensic copy |
| Central tenant migration execution | Backend #94 applies the supplied migration to every selected tenant, aggregates failures and fails the overall command when any tenant fails | CERTIFIED | preserve fail-closed fleet result |
| Central tenant migration atomicity | Backend #94 wraps each tenant migration in BEGIN/COMMIT with ROLLBACK on failure | CERTIFIED | preserve per-tenant transaction boundary |
| Central fleet consistency | Backend #94 continues selected tenants for an actionable summary but returns `TENANT_MIGRATION_PARTIAL_FAILURE` when any failed | CERTIFIED | deployment must treat non-zero fleet result as failure |
| Central migration history/drift detection | Backend #95 records migration filename + SHA-256 in `tenant_schema_migrations`; same checksum reruns skip and historical drift rolls back/fails closed | CERTIFIED | migration filenames/content remain immutable after application |
| Central fresh tenant schema | canonical tenant schema and many dated migrations exist | PARTIAL | prove a fresh tenant can be created at current V1 schema and pass core domain smoke checks |
| Central existing-tenant upgrade | domain PostgreSQL E2Es apply many migrations individually | PARTIAL | run the ordered production migration path against an older representative tenant and verify retained data |
| Central backup export | admin-only tenant backup exists in data-quality support surfaces | PARTIAL | validate size/integrity/tenant scope and define whether it is operational backup or support export |
| Central backup verification | existing support route verifies backup payload/checksum | PARTIAL | certify tamper/corruption detection and tenant-safe recovery expectations |
| Central restore | no complete production restore workflow has been release-certified | GAP | define restore procedure, dry-run verification, rollback/failure behavior and post-restore consistency checks |
| Cross-tenant safety | prior domains certify tenant pool isolation; migration/backup/restore-specific cross-tenant acceptance is incomplete | PARTIAL | prove one tenant cannot modify or restore another tenant database |
| Upgrade rollback policy | POS recovery documentation preserves the pre-restore database and uses validated snapshots; Central policy is not yet release-certified | PARTIAL | document and test supported forward-fix/restore policy for Central |
| Release diagnostics | Central migration failures now expose exact failed tenants; POS validates migration/restore integrity, but unified operational outcome evidence remains incomplete | PARTIAL | expose actionable migration/backup/recovery outcome without secrets or silent partial success |
| Cross-repository release acceptance | no database-quality release gate yet | GAP | final merged Backend + POS acceptance for fresh install, upgrade, failure, integrity, backup/restore and isolation |

## Current certified evidence

- Backend #94 — fail-closed, transactional fleet tenant migration execution.
- Backend #95 — per-tenant migration history, idempotent rerun and checksum-drift rejection.
- POSService #270 — fresh-install migration, exact migration ledger, rerun, checksum drift, transactional failure rollback and integrity acceptance.
- POSService #272 — verified SQLite snapshot, corrupt-candidate rejection and restore preservation of durable outbox/inbox facts.

## Remaining ordered work

1. Certify a representative POS older-schema → current-schema upgrade while preserving business/outbox/inbox/auth data.
2. Certify Central fresh-tenant provisioning at the current V1 schema and a representative existing-tenant ordered upgrade using the production migration path.
3. Certify Central tenant backup verification/restore boundaries, cross-tenant isolation and the supported forward-fix/restore policy.
4. Close database-quality operational diagnostics and post-restore consistency evidence.
5. Add final merged-main database migration/recovery release acceptance and mark every row CERTIFIED or explicitly justified N/A.

## Release decision

**NOT YET RELEASE CERTIFIED.** Fail-closed Central fleet migrations, Central migration checksum history, POS fresh-install/rerun/failure behavior, and POS verified backup/restore are now certified. Remaining release blockers are representative upgrade paths, Central fresh-tenant/backup/restore quality, migration/recovery isolation/diagnostics, and the final merged-main database-quality gate.
