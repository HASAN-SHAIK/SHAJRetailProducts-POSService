# SHAJRetailProducts V1 Frontend Screen / Error / Offline / Sync Completion Acceptance Matrix

Status: **IN PROGRESS**

## Authority boundary

- Central Backend/PostgreSQL remains authoritative for tenant identity, permissions, canonical business data, licensing/configuration, recovery authority, reporting truth, and cross-store state.
- POSService/SQLite remains the small offline edge runtime for cashier-local catalog, customers, orders, payments, returns, inventory projection, local authentication, durable outbox/inbox, effective configuration, device identity, and sync diagnostics.
- Frontend is presentation/orchestration only. It must call the configured Central or local POS repositories, surface actionable state/errors, and never create a second browser-side authority for transactions, inventory, permissions, customer finance, device authority, or recovery.

## Acceptance matrix

| Capability | Existing evidence / required proof | Status |
|---|---|---|
| Application route/screen inventory | Audit current routed V1 screens and remove/defer dead or legacy-only entry points without mixing unrelated redesign | NEEDS ACCEPTANCE |
| Online/offline authority selection | Frontend #43/#44 certify local POS as the only sync execution authority; Frontend #47 prevents orphan browser product mutations in local POS mode; remaining screen-level repository/fallback audit still required | PARTIAL |
| Frontend authentication boundary | Frontend #41/#42 and Auth/Az release certification | CERTIFIED |
| Cashier transaction flow | Transaction Core release certification, selected-order sync status, refund reconciliation | CERTIFIED |
| Transaction pending/synced/blocked visibility | Frontend #37 and Transaction Core matrix | CERTIFIED |
| Customer offline search/create/update | Frontend #39 + Customers release certification | CERTIFIED |
| Customer financial snapshot read-only boundary | Frontend #38 + Customers release certification | CERTIFIED |
| Store/device administration | Frontend #40 + Store/Device release certification | CERTIFIED |
| Product/catalog cashier lookup | Products/Catalog release evidence; Frontend screen-level offline/error acceptance still required | PARTIAL |
| Product/admin create/update/import UX | Frontend #47 fails closed on legacy browser product create/update/delete while local POS mode is authoritative; Central product/import UX validation, loading/error and canonical refresh acceptance still required | PARTIAL |
| Inventory screen/operator truth | Inventory domain provides POS/Central read models and diagnostics; Frontend must avoid direct stock authority and expose actionable state | PARTIAL |
| Pricing/discount/tax UX | Pricing/Tax release semantics certified outside Frontend; screen must display/use server/POS-calculated immutable facts and actionable rejection reasons | PARTIAL |
| Returns/refunds UX | Existing refund reconciliation acceptance; complete loading/error/offline/approval-status coverage | PARTIAL |
| Global 401/403 behavior | Cookie-only Central auth + POS local session certified; screen-level redirect/denial messaging acceptance required | PARTIAL |
| Network/offline transition UX | Existing local POS pathways; certify reconnect without duplicate action or hidden loss | PARTIAL |
| Sync failure/dead-letter visibility | Frontend #45 restricts the support/recovery console to admins; existing POSService outbox/inbox diagnostics expose pending/failed/dead-letter identity, attempts and errors while cashier transaction status stays on Orders | CERTIFIED |
| Actionable error handling | Replace silent catches/generic failures on V1 paths with user-safe actionable errors while retaining diagnostic detail outside the UI | GAP |
| Loading/empty/retry states | Audit and certify each V1 routed screen for deterministic loading, empty, retry and disabled-action behavior | GAP |
| Double-submit/idempotent interaction safety | Frontend #46 certifies checkout confirmation locks before asynchronous order work, local-POS checkout fails closed without legacy browser fallback, and the production build remains green; transaction APIs remain idempotent | CERTIFIED |
| Browser persistence boundary | Frontend #43/#44 remove local-POS browser sync execution authority and Frontend #47 prevents product cache mutations from becoming local-POS authority; remaining IndexedDB/localStorage cache/UI-state audit still required | PARTIAL |
| Accessibility/basic keyboard cashier flow | Focus/keyboard/disabled-state acceptance for critical login, billing, customer and refund paths | GAP |
| Production Frontend build | Frontend #43/#44/#45/#46/#47 exact heads built successfully; final merged-main domain gate still required | PARTIAL |
| Cross-repository Frontend release acceptance | Final merged Frontend -> POS SQLite -> Central/PostgreSQL happy/error/offline/reconnect paths | GAP |

## Ordered work

1. Inventory the current routed V1 screens and their repository/authority dependencies on current Frontend `main`.
2. Close silent/generic error, loading/empty/retry, offline transition, and duplicate-submit defects on critical cashier paths first: login/local auth, billing/order completion, customer selection, payment, refund/partial return and sync status.
3. Close product/catalog, inventory, pricing/tax and device/admin screen gaps without reintroducing browser-side business authority.
4. Keep the now-certified admin-only Sync Center read surfaces presentation-only; do not add Frontend recovery authority.
5. Audit browser persistence so IndexedDB/localStorage is cache/UI state only where Central/POS already owns authoritative facts.
6. Add focused Frontend acceptance and production-build gates, then a final cross-repository release gate against merged Backend/POS/Frontend `main`.

## Release criteria

Frontend Completion V1 can be release-certified only when every matrix row is CERTIFIED or explicitly justified N/A, critical V1 screens have actionable loading/error/offline/sync behavior, no browser-side authority bypass remains, exact-head Frontend validation/build is green, and the final cross-repository Frontend -> POS SQLite -> Central/PostgreSQL acceptance is green.
