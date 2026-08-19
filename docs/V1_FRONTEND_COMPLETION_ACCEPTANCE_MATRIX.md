# SHAJRetailProducts V1 Frontend Screen / Error / Offline / Sync Completion Acceptance Matrix

Status: **RELEASE CERTIFIED**

## Authority boundary

- Central Backend/PostgreSQL remains authoritative for tenant identity, permissions, canonical business data, licensing/configuration, recovery authority, reporting truth, and cross-store state.
- POSService/SQLite remains the small offline edge runtime for cashier-local catalog, customers, orders, payments, returns, inventory projection, local authentication, durable outbox/inbox, effective configuration, device identity, and sync diagnostics.
- Frontend is presentation/orchestration only. It calls the configured Central or local POS repositories, surfaces actionable state/errors, and does not create a second browser-side authority for transactions, inventory, permissions, customer finance, device authority, tax, reporting, or recovery.

## Acceptance matrix

| Capability | Existing evidence / required proof | Status |
|---|---|---|
| Application route/screen inventory | Frontend #48 certifies cashier/admin/mobile V1 routes, compatibility redirects, removed/deferred public entry points and authenticated fallback; Frontend #72 certifies routed setup resilience | CERTIFIED |
| Online/offline authority selection | Frontend #43/#44 keep POSService as local sync authority; #47/#58 block browser product/batch mutation authority; #50 keeps refunds fail-closed on POS authority failures; #52 keeps cashier lookup on POS SQLite; #56 blocks legacy product/batch/supplier delta sync; #62 blocks legacy returns sync; #68 proves offline→online reconnect does not reactivate legacy order/customer/inventory workers | CERTIFIED |
| Frontend authentication boundary | Frontend #41/#42 and Auth/Az V1 release certification | CERTIFIED |
| Cashier transaction flow | Transaction Core release certification, selected-order sync status, refund reconciliation and Frontend #46/#70 checkout single-flight/retry acceptance | CERTIFIED |
| Transaction pending/synced/blocked visibility | Frontend #37 and Transaction Core matrix | CERTIFIED |
| Customer offline search/create/update | Frontend #39/#49 + Customers release certification | CERTIFIED |
| Customer financial snapshot read-only boundary | Frontend #38 + Customers release certification | CERTIFIED |
| Store/device administration | Frontend #40 + Store/Device release certification | CERTIFIED |
| Product/catalog cashier lookup | Products/Catalog release evidence plus Frontend #51/#52 POSService text/barcode authority, accessible single activation, outage state and retry | CERTIFIED |
| Product/admin create/update/import UX | Frontend #47/#58 keep browser mutations non-authoritative; Frontend #71 certifies branch selection, confirmation, parse/validation/loading/import errors, canonical `/products/import-rows`, result summary and refresh | CERTIFIED |
| Inventory screen/operator truth | Frontend #57 routes local-POS inventory reads to authenticated POSService/SQLite; Frontend #80 certifies distinct Loading / Error / Empty / Data states, `Retry POS inventory`, and on-hand/reserved/available truth without browser/Central fallback | CERTIFIED |
| Pricing/discount/tax UX | Pricing/Tax release semantics; Frontend #63 keeps GST authority in Central/POS; POSService #235 + Frontend #69 provide stable actionable price/discount/tax policy failures | CERTIFIED |
| Returns/refunds UX | Frontend #50 certifies POS order/detail authority, loading/error/retry and blocked submit while authoritative refund data is unavailable; refund/approval semantics remain domain-certified | CERTIFIED |
| Reporting/admin routed UX | Frontend #73 prevents fake mobile report zeroes and provides Retry; #74/#75 keep GST reporting on canonical Central facts without fabricated splits; #76 makes Dashboard failures actionable; #77 certifies admin Sync Center operational diagnostics | CERTIFIED |
| Global 401/403 behavior | Frontend #54 certifies one cookie-refresh retry, auth-expired logout, actionable 403 handling and network/auth distinction | CERTIFIED |
| Network/offline transition UX | Frontend #53/#68 plus #56/#62 certify explicit offline behavior and stable POS sync authority across reconnect; durable POS outbox/replay prevents hidden loss | CERTIFIED |
| Sync failure/dead-letter visibility | Frontend #45/#77 restrict support diagnostics to admins and expose POS outbox/inbox/dead-letter/backup/customer-conflict facts without Frontend recovery authority | CERTIFIED |
| Actionable error handling | Customer #49, refund #50, product lookup #52, auth #53/#54, pricing #69, checkout #70, import #71, setup #72, reporting #73/#74/#76, observability #77 and inventory #80 cover critical routed V1 failure surfaces with explicit operator actions or truthful fail-closed presentation | CERTIFIED |
| Loading/empty/retry states | Customer #49, refund #50, billing lookup #51/#52, auth #53, checkout #70, import #71, setup #72, reporting #73/#74/#76, Sync Center #77 and inventory #80 provide deterministic critical-screen loading/error/empty/retry behavior | CERTIFIED |
| Double-submit/idempotent interaction safety | Frontend #46 locks checkout before asynchronous order work; Frontend #53 prevents duplicate auth/registration activation; transaction APIs remain idempotent | CERTIFIED |
| Browser persistence boundary | Frontend #43/#44/#47/#56/#57/#58 remove browser business authority; Frontend #61 certifies no Central access JWT in JS-readable browser/IndexedDB-compatible persistence and metadata-only retained sessions | CERTIFIED |
| Accessibility/basic keyboard cashier flow | Frontend #51/#53/#59/#64/#65/#66/#67/#72 certify native activation, associated controls, live error/status semantics, modal focus/Escape restoration and product/customer/refund-specific accessible names | CERTIFIED |
| Production Frontend build | Exact-head Frontend completion PRs through Frontend #80 built successfully; the final merged-main release gate re-runs the expanded critical-screen suite and production build | CERTIFIED |
| Cross-repository Frontend release acceptance | POS merged-main release workflow validates full POS packages/vet/runtime build plus merged Frontend critical-screen acceptance/build; the matrix-triggered real Order E2E validates POS SQLite → durable outbox → production Central route → PostgreSQL exactly-once projection | CERTIFIED |

## Release evidence and freeze rule

The final merged-main Frontend release workflow covers route inventory, setup, auth response/session behavior, browser-sync containment, Sync Center access/observability, Dashboard/mobile/GST reporting states, cashier checkout/product search/cart/scanner/GST policy presentation, local product/batch/inventory authority including the stock-modal truth states, customers, refunds/returns, browser persistence and reconnect behavior. The matrix change also triggers the real cross-repository Order E2E.

Frontend Completion V1 is frozen after this release decision except for evidenced defects. Frontend remains presentation/orchestration only; future work must not restore browser transaction, inventory, tax, permission, customer-finance, sync, device, reporting or recovery authority.

## Release decision

**V1 FRONTEND COMPLETION RELEASE CERTIFIED.** Every matrix row is CERTIFIED. Merge of this release decision is permitted only after the exact-head POS integration, expanded merged-main Frontend release acceptance, real cross-repository Order E2E, mergeability, and review/thread state are clean.
