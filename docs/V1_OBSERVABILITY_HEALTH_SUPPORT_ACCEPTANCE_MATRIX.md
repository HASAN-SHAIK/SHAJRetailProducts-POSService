# SHAJRetailProducts V1 Observability / Health / Support Diagnostics Acceptance Matrix

Status: **IN PROGRESS**

## Authority boundary

- Central owns canonical tenant/device/service authority and must expose only support-safe operational state.
- POSService owns local edge runtime health, SQLite integrity, outbox/inbox state, retained configuration state, backup state and local-auth operational diagnostics.
- Frontend is presentation only; it must not become a monitoring, recovery, or credential authority.
- Public health endpoints must remain minimal and secret-free. Detailed diagnostics must stay behind the existing local/Central authorization boundaries.

## Acceptance matrix

| Capability | Current evidence | Status | Release requirement |
|---|---|---|---|
| POS liveness | packaged POS exposes `GET /api/v1/health` with service/environment/start time/uptime | PARTIAL | focused acceptance for stable secret-free response |
| POS readiness | `GET /api/v1/ready` checks SQLite availability and durable device identity, returning 503 when unavailable | PARTIAL | focused healthy + database/device failure acceptance |
| POS SQLite integrity at startup | packaged runtime runs migrations then `IntegrityCheck` and fails startup on integrity failure | CERTIFIED | preserve fail-closed startup behavior |
| POS summary diagnostics | collector exposes database status, outbox status, inbox counts, customer conflicts/pending state, change cursor and latest backup age/size | PARTIAL | certify bounded support-safe response and failure handling |
| POS sync-event diagnostics | bounded endpoint exposes pending/failed/dead-letter outbox, failed/processing inbox, config sync state and local-auth operational state | CERTIFIED | preserve bounded result size and no credential material |
| POS configuration sync diagnostics | last attempt/success/error/ETag already certified in Pricing/Sync work | CERTIFIED | preserve retained-state visibility |
| POS customer/inventory/catalog sync diagnostics | focused domain acceptance already proves failed/dead-letter facts remain support-visible | CERTIFIED | preserve domain identity/error provenance |
| POS local-auth diagnostics | existing Auth/Az certification exposes counts/expiry/lock state without PIN/grant/session secrets | CERTIFIED | preserve credential-safe response |
| POS backup observability | collector reports latest SQLite backup timestamp and bytes; database-quality domain verifies snapshot integrity/recovery | PARTIAL | certify missing/stale/latest backup state without filesystem secrets |
| POS diagnostics bounds | sync-event endpoint clamps requested result count to 1..500 | CERTIFIED | preserve bounded local support queries |
| Central liveness | `/health` and `/api/health` expose uptime/timestamp and optional authorized warmup status | PARTIAL | preserve minimal public liveness; test unauthorized warmup does not query DB |
| Central readiness | no dedicated normal readiness route currently verifies required PostgreSQL availability | GAP | add public minimal readiness that returns 503 when master DB dependency is unavailable |
| Central tenant operational diagnostics | existing admin/reporting/data-quality/device surfaces expose tenant-scoped audit, consistency, registration and recovery facts | PARTIAL | inventory exact support routes and certify auth/tenant isolation |
| Central migration/recovery diagnostics | database-quality domain certifies typed tenant/migration/checksum/backup/restore outcomes and secret-safe runbook | CERTIFIED | preserve typed failure codes and secret exclusion |
| Central request/error hygiene | shared error handler exists, but V1 correlation and secret-safe error/log acceptance is incomplete | PARTIAL | certify request correlation and production-safe client error payloads |
| POS request correlation | POS HTTP server wraps requests in request-ID middleware | PARTIAL | certify generated/preserved request ID and response correlation |
| Health endpoint credential safety | POS health/readiness are intentionally supervisor-safe; Central health response contains no tokens/connection strings | PARTIAL | focused acceptance preventing secret/config leakage |
| Support recovery authority | existing Central-approved sync recovery, device recovery, database recovery and POS diagnostics remain separated from read-only support views | CERTIFIED | do not add support-side mutation authority |
| Frontend support presentation | Sync Center/device/customer/inventory/reporting screens already consume read-only diagnostics in several domains | PARTIAL | certify final actionable health/support states without bypassing POS/Central authority |
| Cross-repository release acceptance | no final Observability/Health release gate yet | GAP | merged POS + Backend + Frontend health/diagnostics/build acceptance |

## Current evidence

- POS packaged runtime starts observability collection and fails startup on migration/integrity errors.
- POS server exposes `/api/v1/health`, `/api/v1/ready`, `/api/v1/diagnostics`, and `/api/v1/diagnostics/sync-events`.
- POS diagnostics already include outbox/inbox/config/local-auth support state and bounded sync-event results.
- Central `src/App.js` exposes minimal `/health` and `/api/health`; optional DB warmup is separately authorized and time-window constrained.
- Database Migration/Recovery V1 provides typed, secret-safe Central migration/backup/restore diagnostics.
- Prior Inventory/Customers/Pricing/Auth/Store-Device certifications provide domain-specific diagnostics evidence that should be reused rather than reimplemented.

## Remaining ordered work

1. Add and certify minimal Central readiness against required PostgreSQL dependency without exposing connection details.
2. Certify POS liveness/readiness, summary diagnostics, backup age/state and request correlation with focused executable acceptance.
3. Certify Central health/warmup and support diagnostics authorization/tenant isolation/error hygiene.
4. Close Frontend health/support presentation gaps only where a real operator-facing defect exists.
5. Add final merged-main Observability/Health/Support release acceptance and mark every row CERTIFIED or explicitly justified N/A.

## Release decision

**NOT YET RELEASE CERTIFIED.** Existing diagnostics are substantial and should be reused. The first confirmed V1 runtime gap is Central readiness; remaining work is primarily focused health/diagnostics certification, support-safe correlation, and the final release gate.
