# SHAJRetailProducts V1 Final Release Acceptance Matrix

Status: **RELEASE CANDIDATE — FINAL EXACT-HEAD VALIDATION REQUIRED**

## Release authority

- Central Backend/PostgreSQL remains canonical for tenant identity, permissions, configuration, recovery authorization, financial/customer/inventory/reporting truth and durable cross-device convergence.
- POSService remains the small offline edge runtime with SQLite, local operator sessions, deterministic pricing/tax execution, transactional outbox/inbox, offline durability and reconciliation diagnostics.
- Frontend remains presentation/orchestration only and does not become canonical business, permission, recovery, sync or secret authority.

## Domain release status

| V1 domain | Release evidence | Status |
|---|---|---|
| Transaction Core | Authoritative transaction matrix; exact-head POS edge + real PostgreSQL Order/Payment/Receipt/Refund/Partial Return E2Es; cashier sync-status certification | CERTIFIED |
| Inventory | Final Inventory release certification #148 covering sale/refund/partial-return effects, batch convergence, branch isolation, adjustment/audit, dead-letter/recovery and support reconciliation | CERTIFIED |
| Products / Catalog | Products/Catalog release certification #166 with Central CRUD, branch/tenant isolation, import, lifecycle, replay and offline lookup evidence | CERTIFIED |
| Pricing / Promotions / Tax / Rounding | Pricing/Tax release certification #180 with Central policy, deterministic offline GST/rounding, discounts, immutable Central snapshots and refund parity; promotions explicitly N/A for V1 | CERTIFIED |
| Customers | Customers release certification #192 with offline lifecycle, bidirectional canonical identity, Central financial authority, outstanding projection, isolation, diagnostics and Frontend UX | CERTIFIED |
| Store / Device Operations | Store/Device release certification #204 with registration/licensing, trusted branch/device authority, revocation/reassignment/replacement, offline retained state, diagnostics and Frontend administration | CERTIFIED |
| Authentication / Authorization | Auth/Az release certification #216 with tenant/platform isolation, permission enforcement, refresh replay protection, device-bound offline grants, POS local auth, HttpOnly browser sessions and protected local-machine boundary | CERTIFIED |
| Frontend Completion | Frontend release certification #310 including routed screens, local-POS authority, loading/error/retry/offline states, inventory truth, accessibility, persistence, production build and real transaction E2E | CERTIFIED |
| Reporting / Admin | Reporting/Admin release certification #263 with canonical/refund-aware sales/profit/daily/GST/customer/category reporting, branch/tenant isolation, bounded ranges/support surfaces and Frontend reporting UX | CERTIFIED |
| Central ↔ POS Synchronization | Sync release certification #268 with effective config, catalog/customer feed, cursor/inbox transactional replay, schema fail-closed behavior, restart/reconnect and tenant/device binding | CERTIFIED |
| Database Migration / Upgrade / Backup / Recovery | Database release certification #278 with POS SQLite fresh/upgrade/backup/restore and Central fresh/existing tenant PostgreSQL migration/native same-tenant restore/isolation | CERTIFIED |
| Observability / Health / Support Diagnostics | Observability release certification #287 with liveness/readiness, request correlation, bounded diagnostics, backup/config/cursor/auth visibility and Central error hygiene | CERTIFIED |
| Security | Security release certification #312 with tenant/store/device/key/token isolation, production fail-closed config, TLS/CORS/rate-limit/upload/path/container/log hardening, dependency residual policy and consolidated three-repository gate | CERTIFIED |
| Deployment / Configuration / Secrets | Deployment release certification #318 with state-preserving installers, complete production config, secret-safe Frontend container, credential rotation, exact artifact provenance, deployment ordering and three-repository container/package gate | CERTIFIED |

## Final exact-head gates

The final release PR must not merge unless all of the following are green on its exact head:

1. **POS full acceptance:** all Go packages, `go vet`, packaged POS build and release-artifact provenance.
2. **Backend merged-main acceptance:** committed-lockfile install, complete Jest suite and production Backend Docker build.
3. **Frontend merged-main acceptance:** committed-lockfile install, complete Frontend test suite, production CRA build and production nginx Docker image.
4. **Real cross-repository transaction acceptance:** restarted POS SQLite durable outbox → production Central sync route → PostgreSQL exactly-once order/payment/receipt/inventory projection + duplicate replay.
5. PR remains mergeable, review-clean, with no unresolved critical issue.

## Release decision

**DO NOT DECLARE V1 COMPLETE UNTIL THE FINAL EXACT-HEAD GATES ABOVE ARE GREEN.**

When they are green and this PR merges, SHAJRetailProducts V1 is **RELEASE CERTIFIED / COMPLETE**. Freeze all V1 domains except for real defects; new capability work belongs to a subsequent release line.
