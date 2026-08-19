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
| POS liveness | POSService #281 certifies packaged `GET /api/v1/health` as stable, minimal and secret-free | CERTIFIED | preserve public supervisor-safe liveness |
| POS readiness | POSService #281 certifies SQLite + durable-device readiness and explicit 503 database/device failure states | CERTIFIED | preserve fail-closed dependency readiness |
| POS SQLite integrity at startup | packaged runtime runs migrations then `IntegrityCheck` and fails startup on integrity failure | CERTIFIED | preserve fail-closed startup behavior |
| POS summary diagnostics | collector exposes database status, outbox status, inbox counts, customer conflicts/pending state, change cursor and latest backup age/size | PARTIAL | certify bounded support-safe response and failure handling |
| POS sync-event diagnostics | bounded endpoint exposes pending/failed/dead-letter outbox, failed/processing inbox, config sync state and local-auth operational state | CERTIFIED | preserve bounded result size and no credential material |
| POS configuration sync diagnostics | last attempt/success/error/ETag already certified in Pricing/Sync work | CERTIFIED | preserve retained-state visibility |
| POS customer/inventory/catalog sync diagnostics | focused domain acceptance already proves failed/dead-letter facts remain support-visible | CERTIFIED | preserve domain identity/error provenance |
| POS local-auth diagnostics | existing Auth/Az certification exposes counts/expiry/lock state without PIN/grant/session secrets | CERTIFIED | preserve credential-safe response |
| POS backup observability | collector reports latest SQLite backup timestamp and bytes; database-quality domain verifies snapshot integrity/recovery | PARTIAL | certify missing/stale/latest backup state without filesystem secrets |
| POS diagnostics bounds | sync-event endpoint clamps requested result count to 1..500 | CERTIFIED | preserve bounded local support queries |
| Central liveness | `/health` and `/api/health` expose uptime/timestamp and optional separately authorized warmup status; Backend #101 preserves warmup-before-query ordering | PARTIAL | add focused stable public liveness response acceptance |
| Central readiness | Backend #101 adds `/ready` and `/api/ready`, requiring master PostgreSQL and returning secret-free 503 on dependency failure | CERTIFIED | preserve minimal dependency readiness |
| Central tenant operational diagnostics | existing admin/reporting/data-quality/device surfaces expose tenant-scoped audit, consistency, registration and recovery facts | PARTIAL | inventory exact support routes and certify auth/tenant isolation |
| Central migration/recovery diagnostics | database-quality domain certifies typed tenant/migration/checksum/backup/restore outcomes and secret-safe runbook | CERTIFIED | preserve typed failure codes and secret exclusion |
| Central request/error hygiene | Backend #102 makes shared 5xx client responses stable and secret-free while preserving explicit client-safe 4xx errors; correlation remains open | PARTIAL | certify request correlation and logging linkage |
| POS request correlation | POS HTTP server returns `X-Request-ID`, but generation/input validation and log linkage are not yet V1-certified | PARTIAL | certify generated/preserved request ID and response/log correlation |
| Health endpoint credential safety | POSService #281 and Backend #101 prove readiness failures do not expose dependency secrets; POS liveness is minimal | CERTIFIED | preserve secret-free public health/readiness responses |
| Support recovery authority | existing Central-approved sync recovery, device recovery, database recovery and POS diagnostics remain separated from read-only support views | CERTIFIED | do not add support-side mutation authority |
| Frontend support presentation | Sync Center/device/customer/inventory/reporting screens already consume read-only diagnostics in several domains | PARTIAL | certify final actionable health/support states without bypassing POS/Central authority |
| Cross-repository release acceptance | no final Observability/Health release gate yet | GAP | merged POS + Backend + Frontend health/diagnostics/build acceptance |

## Current evidence

- POSService #281 certifies packaged POS health/readiness with real SQLite/device state and retains Refund/Partial Return/POS edge regressions green.
- Backend #101 adds and certifies minimal Central PostgreSQL readiness while preserving separate liveness/warmup semantics.
- Backend #102 hardens the shared Central error boundary so internal 5xx messages/codes cannot leak database or infrastructure details to clients.
- POS packaged runtime starts observability collection and fails startup on migration/integrity errors.
- POS diagnostics already include outbox/inbox/config/local-auth support state and bounded sync-event results.
- Database Migration/Recovery V1 provides typed, secret-safe Central migration/backup/restore diagnostics.
- Prior Inventory/Customers/Pricing/Auth/Store-Device certifications provide domain-specific diagnostics evidence that should be reused rather than reimplemented.

## Remaining ordered work

1. Certify POS summary diagnostics, backup age/state and request correlation with focused executable acceptance.
2. Certify Central stable liveness, request correlation and tenant operational-diagnostics authorization/isolation.
3. Close Frontend health/support presentation gaps only where a real operator-facing defect exists.
4. Add final merged-main Observability/Health/Support release acceptance and mark every row CERTIFIED or explicitly justified N/A.

## Release decision

**NOT YET RELEASE CERTIFIED.** Core Central/POS readiness and public health credential safety are now certified. Remaining work is concentrated on bounded summary/backup diagnostics, request correlation, Central tenant-support authorization/isolation, Frontend support presentation, and the final release gate.
