# SHAJRetailProducts V1 Observability / Health / Support Diagnostics Acceptance Matrix

Status: **RELEASE CERTIFIED**

## Authority boundary

- Central owns canonical tenant/device/service authority and exposes only support-safe operational state.
- POSService owns local edge runtime health, SQLite integrity, outbox/inbox state, retained configuration state, backup state and local-auth operational diagnostics.
- Frontend is presentation only; it does not become a monitoring, recovery, or credential authority.
- Public health endpoints remain minimal and secret-free. Detailed diagnostics stay behind the existing local/Central authorization boundaries.

## Acceptance matrix

| Capability | Current evidence | Status | Release requirement |
|---|---|---|---|
| POS liveness | POSService #281 certifies packaged `GET /api/v1/health` as stable, minimal and secret-free | CERTIFIED | preserve public supervisor-safe liveness |
| POS readiness | POSService #281 certifies SQLite + durable-device readiness and explicit 503 database/device failure states | CERTIFIED | preserve fail-closed dependency readiness |
| POS SQLite integrity at startup | packaged runtime runs migrations then `IntegrityCheck` and fails startup on integrity failure | CERTIFIED | preserve fail-closed startup behavior |
| POS summary diagnostics | POSService #283 certifies real-SQLite database health, outbox/inbox counts, customer sync state, durable change cursor and backup facts | CERTIFIED | preserve bounded support-safe response and failure handling |
| POS sync-event diagnostics | bounded endpoint exposes pending/failed/dead-letter outbox, failed/processing inbox, config sync state and local-auth operational state | CERTIFIED | preserve bounded result size and no credential material |
| POS configuration sync diagnostics | last attempt/success/error/ETag already certified in Pricing/Sync work | CERTIFIED | preserve retained-state visibility |
| POS customer/inventory/catalog sync diagnostics | focused domain acceptance already proves failed/dead-letter facts remain support-visible | CERTIFIED | preserve domain identity/error provenance |
| POS local-auth diagnostics | Auth/Az certification exposes counts/expiry/lock state without PIN/grant/session secrets | CERTIFIED | preserve credential-safe response |
| POS backup observability | POSService #283 certifies latest SQLite `.db` backup selection plus timestamp/size while ignoring unrelated filesystem files | CERTIFIED | preserve missing/latest backup visibility without filesystem secrets |
| POS diagnostics bounds | sync-event endpoint clamps requested result count to 1..500 | CERTIFIED | preserve bounded local support queries |
| Central liveness | Backend #104 certifies `/health` and `/api/health` as stable public liveness independent from required PostgreSQL readiness unless separately authorized warmup is requested | CERTIFIED | preserve stable secret-free liveness separate from readiness |
| Central readiness | Backend #101 adds `/ready` and `/api/ready`, requiring master PostgreSQL and returning secret-free 503 on dependency failure | CERTIFIED | preserve minimal dependency readiness |
| Central tenant operational diagnostics | Backend #93 certifies Central-admin protection, `req.tenantPool` isolation, bounded support queries and authenticated actor provenance across data-quality/support surfaces | CERTIFIED | preserve tenant-scoped admin/support authority and bounded output |
| Central migration/recovery diagnostics | Database Migration/Recovery V1 certifies typed tenant/migration/checksum/backup/restore outcomes and secret-safe runbook | CERTIFIED | preserve typed failure codes and secret exclusion |
| Central request/error hygiene | Backend #102 keeps internal 5xx client responses stable/secret-free; Backend #103 adds bounded request IDs and response/log correlation without query-string logging | CERTIFIED | preserve stable errors and safe correlation fields |
| POS request correlation | POSService #286 certifies bounded safe caller IDs, opaque generated IDs, request-context/response linkage and structured completion logs without query-string payload leakage | CERTIFIED | preserve one accepted request ID across response and logs |
| Health endpoint credential safety | POSService #281 plus Backend #101/#104 prove public liveness/readiness failures do not expose dependency secrets | CERTIFIED | preserve secret-free public health/readiness responses |
| Support recovery authority | existing Central-approved sync recovery, device recovery, database recovery and POS diagnostics remain separated from read-only support views | CERTIFIED | do not add support-side mutation authority |
| Frontend support presentation | Frontend #77 certifies the admin Sync Center presents POS SQLite/outbox/inbox/customer-conflict/dead-letter/backup diagnostics with explicit unavailable/error state and refresh controls | CERTIFIED | keep Frontend read-only with respect to support authority |
| Cross-repository release acceptance | `v1-observability-health-release.yml` validates merged POS, Backend and Frontend health/diagnostics/build evidence | CERTIFIED | keep the final release gate green on merged-main dependencies |

## Final evidence

- POSService #281 certifies packaged POS health/readiness with real SQLite/device state and retains Refund/Partial Return/POS edge regressions green.
- POSService #283 certifies the existing POS observability collector against real SQLite and backup files: database status, outbox pending/dead-letter state, inbox received/failed state, customer conflict/pending state, durable Central change cursor and latest backup timestamp/size.
- Backend #101 certifies minimal Central PostgreSQL readiness while preserving separate liveness/warmup semantics.
- Backend #102 hardens the shared Central error boundary so internal 5xx messages/codes cannot leak database or infrastructure details to clients.
- Backend #103 adds shared Central request correlation: bounded safe caller IDs are preserved, unsafe/missing IDs are regenerated, and the same ID links the response to structured request logging without query-string secrets.
- Backend #104 certifies the public Central liveness contract independently from PostgreSQL readiness while preserving separately authorized warmup behavior.
- Backend #93 certifies Central support/data-quality routes remain admin-protected, tenant-pool scoped and bounded, with authenticated actor provenance for support mutations.
- POSService #286 adds the corresponding packaged-edge request-correlation contract, including machine-auth failures because correlation wraps the secure local API boundary.
- Frontend #77 certifies the existing admin Sync Center operational presentation instead of introducing another monitoring/recovery subsystem.
- Database Migration/Recovery V1 provides typed, secret-safe Central migration/backup/restore diagnostics.
- Inventory, Customers, Pricing, Auth and Store/Device certifications retain domain-specific diagnostics evidence and recovery authority separation.
- Final merged-main release acceptance validates POS tests/vet/build, Backend Observability/support safety acceptance, and Frontend Sync Center observability acceptance plus production build.

## Release decision

**V1 OBSERVABILITY / HEALTH / SUPPORT DIAGNOSTICS RELEASE CERTIFIED.** Every V1 row is certified. The domain is frozen except for real defects; broader production monitoring infrastructure, external alert routing, deployment secret delivery and operational SLO policy remain in their later ordered Security/Deployment/operations work rather than being duplicated here.
