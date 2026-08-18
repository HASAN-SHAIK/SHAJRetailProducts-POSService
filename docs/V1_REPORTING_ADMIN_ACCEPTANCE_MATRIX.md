# SHAJRetailProducts V1 Reporting / Admin Acceptance Matrix

Status: **IN PROGRESS**

## Authority boundary

- Central Backend/PostgreSQL is canonical for business reporting facts, administrative reporting permissions, tenant isolation, historical sales/returns, inventory value, profit and operational support views.
- POSService/SQLite may expose local operational diagnostics needed during offline operation, but is not the canonical business-reporting engine.
- Frontend is presentation/orchestration only. It must not recompute canonical revenue, profit, tax, inventory value or historical business truth from browser caches.
- Certified Transaction, Inventory, Products/Catalog, Pricing/Tax, Customers, Store/Device and Auth/Az facts are inputs to reporting; Reporting V1 must consume those authorities rather than create parallel truth.

## Acceptance matrix

| Capability | Current evidence | Status | Release requirement |
|---|---|---|---|
| Central report permission boundary | Backend #76 aligns all five core report routes with `reports:read`; cashier remains denied by the certified role catalog; branch-restricted users fail closed with `REPORT_BRANCH_SCOPE_REQUIRED` until trusted branch filters are implemented | CERTIFIED | preserve stable server-side permission denial while branch-scoped report queries are added |
| Sales/revenue report | `reportController.getSalesReport` reads tenant PostgreSQL orders, subtracts `returned_amount`, includes completed/partial/full-return lifecycle and returned item quantities | PARTIAL | focused PostgreSQL acceptance for sale + partial/full return + duplicate/replay-safe canonical facts |
| Daily sales report | Central has a dedicated daily report path and date validation | PARTIAL | certify deterministic date boundary/timezone semantics and refund-aware totals |
| Profit report | Central derives profit from canonical order items and returned quantities | PARTIAL | certify immutable purchase-price/profit snapshots and full/partial-return effects; avoid current-product-price recomputation |
| Inventory report | Central reports total stock, low/out-of-stock, inventory value and cost value from canonical products | PARTIAL | establish branch/store versus tenant-global scope, batch-aware consistency expectations and focused tenant/branch acceptance |
| Dashboard/admin KPI views | Frontend Dashboard and Central reporting/dashboard infrastructure already exist | PARTIAL | inventory actual routed KPI APIs, remove browser-cache authority, add loading/error/empty/retry acceptance and production build evidence |
| Product/category performance | Central sales reporting already exposes best-selling/profit-by-product queries | PARTIAL | certify returned quantities, deleted/deactivated products and historical product naming/identity behavior |
| Tax/GST reporting | Frontend exposes GST/tax report surfaces and Pricing/Tax V1 already owns immutable tax snapshots | PARTIAL | prove tax reports consume immutable canonical sale/return snapshots and do not recalculate with current policy |
| Customer/outstanding admin reporting | Customers V1 now owns canonical outstanding/payment/return projection | PARTIAL | certify reporting reads canonical customer balance/ledger facts without POS/browser financial authority |
| Store/branch reporting scope | Backend #77 binds every new canonical POS sale to the trusted active device branch inside the Central sync transaction; POS #244 proves restarted SQLite outbox → merged Backend `main` → PostgreSQL branch provenance and replay | PARTIAL | apply the trusted Central branch resolver to every report query; restricted users must see only their assigned branch, all-branch users may explicitly scope or aggregate; do not guess historical null POS branch provenance |
| Tenant isolation | report controllers use `req.tenantPool` when present | PARTIAL | explicit two-tenant PostgreSQL acceptance proving identical IDs/data cannot cross-report |
| Date/range validation | some report paths validate dates and apply UTC ranges | PARTIAL | establish one V1 date-range contract, invalid-range behavior and bounded query limits |
| Large-result safety | several detail lists are bounded, but Reporting V1 has no unified pagination/range matrix yet | GAP | certify bounded result sizes / date windows and avoid unbounded administrative queries |
| Support/data-quality reporting | Backend contains data-quality/support reporting surfaces | PARTIAL | inventory existing routes, certify permissions, tenant scope, actionable diagnostics and credential-safe output |
| Frontend reporting/admin UX | Dashboard, mobile reports, GST/tax and admin/support routes exist | PARTIAL | certify supported routed screens, role visibility, loading/error/empty/retry states, accessibility and no stale browser authority |
| Export/download behavior | existing reporting/export behavior has not yet been authoritatively inventoried | GAP | certify only V1-supported exports, content type/filename, permission and tenant scope; mark unsupported formats N/A |
| Cross-repository release acceptance | Reporting branch-provenance E2E now exists, but no full Reporting/Admin merged-main release gate exists yet | PARTIAL | merged Frontend + POS + Backend/PostgreSQL acceptance using canonical transaction/return/inventory/customer facts and trusted branch scope |

## Current certified evidence

- Backend #76: report permission authority uses the existing `reports:read` catalog and fails closed for branch-restricted users until trusted branch query scoping is present.
- Backend #77: new canonical POS `sale.completed` orders are transactionally bound to the active Central-registered device branch; missing provenance and cross-branch rebinding fail closed.
- POSService #244: real restarted POS SQLite durable outbox → production Central sync route on merged Backend `main` → PostgreSQL proves canonical POS order branch provenance and duplicate/replay safety.
- Historical canonical POS orders whose branch provenance was never recorded are not guessed or backfilled from current device registration because device reassignment can make that inference unsafe; they remain a reporting data-quality/reconciliation concern until exact provenance is available.

## First ordered work

1. Apply `resolveBranchIdFromRequest`-based trusted branch filtering to sales, daily, profit, product-performance and inventory report queries, then remove the temporary branch-restricted fail-closed report guard.
2. Add explicit branch-A/branch-B PostgreSQL acceptance proving restricted users cannot widen scope and all-branch users can intentionally aggregate or select a branch.
3. Certify refund-aware sales/revenue/profit math against PostgreSQL using canonical order/return facts.
4. Add explicit two-tenant reporting isolation and one bounded V1 date/range contract.
5. Audit the actual Frontend Dashboard/report/admin routes and close loading/error/empty/retry and browser-authority gaps.
6. Add the final Reporting/Admin cross-repository release gate and mark every row CERTIFIED or explicitly justified N/A.

## Release decision

**NOT YET RELEASE CERTIFIED.** Reporting/Admin V1 remains acceptance-first. No new reporting subsystem should be added until the existing Central queries, permissions and Frontend surfaces are fully inventoried and the real gaps above are proven.