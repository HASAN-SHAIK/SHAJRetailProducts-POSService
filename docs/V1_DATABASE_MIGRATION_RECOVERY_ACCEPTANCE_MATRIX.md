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
| POS migration checksum drift | `schema_migrations` stores SHA-256 checksums and runtime rejects a changed already-applied migration | CERTIFIED | historical migration files remain immutable |
| POS durability pragmas | SQLite opens with foreign keys, WAL, synchronous FULL, busy timeout and immediate transaction locking | CERTIFIED | release runtime must retain durability/concurrency pragmas |
| POS integrity check | database layer exposes `PRAGMA quick_check` and fails when result is not `ok` | PARTIAL | add release acceptance around healthy/corrupt/unopenable database behavior |
| POS fresh-install migration | no dedicated acceptance yet proves an empty SQLite file reaches the complete current V1 schema | GAP | migrate a fresh database through every embedded migration and verify critical tables/columns |
| POS upgrade migration | domain tests exercise upgraded schemas indirectly, but there is no explicit N→current preserved-data acceptance | PARTIAL | seed an older schema/data set, run remaining migrations, verify business/outbox/inbox/auth facts survive |
| POS migration rerun/idempotency | applied migrations are skipped by version/checksum | PARTIAL | explicitly certify restart/rerun applies zero duplicate schema work |
| POS migration failure recovery | transactional migration code exists | PARTIAL | prove a failing migration leaves prior schema/data usable and does not record the failed version |
| POS SQLite backup | no canonical online SQLite backup/export contract is yet established | GAP | define V1 backup mechanism that is WAL-safe and preserves the accepted database atomically |
| POS SQLite restore/recovery | restart persistence exists, but no restore-from-backup acceptance exists | GAP | restore into a clean runtime, run integrity/migrations, and verify durable outbox/inbox/business facts |
| Central tenant migration execution | `scripts/runTenantMigration.js` discovers tenant databases and applies a supplied SQL file | PARTIAL | migration failure for any selected tenant must fail the operation/deployment rather than silently succeed |
| Central tenant migration atomicity | runner currently sends migration SQL directly to a tenant connection without an explicit transaction boundary | PARTIAL | each tenant migration must commit atomically or roll back |
| Central fleet consistency | runner logs a failed tenant and continues; current process can finish without a non-zero fleet failure | GAP | aggregate failures and exit non-zero with exact failed tenant identities |
| Central migration history/drift detection | dated SQL files exist, but the manual tenant runner has no authoritative per-tenant migration/checksum ledger | GAP | establish or certify an architecture-aligned migration-history mechanism before release |
| Central fresh tenant schema | canonical tenant schema and many dated migrations exist | PARTIAL | prove a fresh tenant can be created at current V1 schema and pass core domain smoke checks |
| Central existing-tenant upgrade | domain PostgreSQL E2Es apply many migrations individually | PARTIAL | run the ordered production migration path against an older representative tenant and verify retained data |
| Central backup export | admin-only tenant backup exists in data-quality support surfaces | PARTIAL | validate size/integrity/tenant scope and define whether it is operational backup or support export |
| Central backup verification | existing support route verifies backup payload/checksum | PARTIAL | certify tamper/corruption detection and tenant-safe recovery expectations |
| Central restore | no complete production restore workflow has been release-certified | GAP | define restore procedure, dry-run verification, rollback/failure behavior and post-restore consistency checks |
| Cross-tenant safety | current DB routing and prior domains certify tenant pool isolation | PARTIAL | migration/backup/restore acceptance must prove one tenant cannot modify/restore another tenant database |
| Upgrade rollback policy | no explicit application/database rollback contract is release-certified | GAP | document forward-fix vs restore policy and test the supported path |
| Release diagnostics | POS has integrity primitives and Central logs migration errors | PARTIAL | expose actionable migration/backup/recovery outcome without secrets or silent partial success |
| Cross-repository release acceptance | no database-quality release gate yet | GAP | final merged Backend + POS acceptance for fresh install, upgrade, failure, integrity, backup/restore and isolation |

## First ordered work

1. Fix Central tenant migration execution so every tenant runs in an explicit transaction and any failed tenant makes the overall migration command fail non-zero with an actionable summary.
2. Add POS fresh-install/rerun/failure migration acceptance around the existing embedded checksum runner.
3. Establish the Central migration-history/drift contract and certify fresh-tenant + existing-tenant upgrade behavior.
4. Define and certify WAL-safe POS backup/restore and Central tenant backup/restore boundaries.
5. Add final merged-main database migration/recovery release acceptance and mark every row CERTIFIED or explicitly justified N/A.

## Release decision

**NOT YET RELEASE CERTIFIED.** The POS migration foundation is strong, but explicit upgrade/failure/backup/restore evidence is incomplete. Central's current tenant migration runner can silently leave a tenant migration failed while the overall run continues, which is the first release-blocking defect to close.
