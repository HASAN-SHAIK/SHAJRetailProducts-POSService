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
| Central report permission boundary | Backend #76 aligns all five core report routes with `reports:read`; Backend #84 extends the same permission boundary to mobile reporting; Backend #85 applies `reports:read` plus trusted report scope to `/dashboard/overview`; cashier remains denied by the certified role catalog | CERTIFIED | preserve stable server-side permission denial and trusted Central report scope |
| Sales/revenue report | `reportController.getSalesReport` reads tenant PostgreSQL orders, subtracts `returned_amount`, includes completed/partial/full-return lifecycle and returned item quantities; Backend #78 certifies refund-aware aggregate structure; Backend #80 adds real branch-A/branch-B PostgreSQL isolation; Backend #83 proves full-return net-zero behavior and replay-stable canonical facts | CERTIFIED | preserve immutable order/return snapshots and replay-safe net revenue |
| Daily sales report | Backend #82 derives daily revenue from canonical `orders.total_price - returned_amount`, uses strict UTC `[start,end)` day boundaries, validates `YYYY-MM-DD`, preserves trusted branch filtering and returned-quantity-aware product/profit facts | CERTIFIED | preserve canonical immutable order/return facts, UTC date semantics and trusted branch scope |
| Profit report | Backend #78 certifies returned-quantity-aware profit structure; Backend #80 scopes profit to the trusted branch; Backend #83 proves full-return profit contribution nets to zero and remains replay-stable in PostgreSQL | CERTIFIED | preserve immutable purchase-price/profit snapshots and return-aware replay behavior |
| Inventory report | Backend #87 adds trusted branch inventory reporting from canonical branch facts instead of tenant product totals: physical stock, non-expired sellable stock, expired stock, outstanding provisional/unallocated POS oversell deficit, projected net stock, low/out-of-stock counts and sellable inventory value; real PostgreSQL branch-A/branch-B acceptance proves branch isolation and deficit restoration semantics | CERTIFIED | preserve explicit physical/sellable/expired/provisional-deficit semantics and never collapse provisional offline deficit into fabricated batch stock |
| Dashboard/admin KPI views | Backend #85 now requires `reports:read`, reuses trusted Central report scope for `/dashboard/overview`, replaces caller branch input with the resolved branch, and suppresses uncertified branch low/dead-stock widgets while retaining order-derived KPI facts; Backend #84 scopes mobile dashboard sales/profit/recent-order facts; Frontend #73 makes the routed mobile Reports screen truthful and retryable | PARTIAL | audit remaining routed dashboard/admin KPI APIs and add loading/error/empty/retry acceptance; wire inventory widgets only from the now-certified branch inventory contract |
| Product/category performance | Central sales reporting already exposes best-selling/profit-by-product queries; Backend #78 verifies returned quantities, Backend #80 binds aggregates to trusted branch scope, and Backend #83 proves fully returned quantity contributes zero | PARTIAL | certify deleted/deactivated product historical identity behavior and PostgreSQL result correctness |
| Tax/GST reporting | Frontend exposes GST/tax report surfaces and Pricing/Tax V1 already owns immutable tax snapshots | PARTIAL | prove tax reports consume immutable canonical sale/return snapshots and do not recalculate with current policy |
| Customer/outstanding admin reporting | Customers V1 now owns canonical outstanding/payment/return projection | PARTIAL | certify reporting reads canonical customer balance/ledger facts without POS/browser financial authority |
| Store/branch reporting scope | Backend #77 binds every new canonical POS sale to the trusted active device branch; POS #244 proves restarted SQLite outbox → Central/PostgreSQL branch provenance and replay; Backend #80 applies trusted branch scope to core reports; Backend #83 preserves return/replay correctness; Backend #84/#85 extend trusted scope to mobile/dashboard facts; Backend #87 now serves branch inventory only from certified branch inventory truth | CERTIFIED | preserve trusted branch provenance; historical null POS branch provenance must remain explicit rather than guessed |
| Tenant isolation | Backend #81 proves identical report requests remain bound to separate supplied tenant PostgreSQL pools; Backend #84 removes the mobile reporting fallback to the default/global pool | CERTIFIED | preserve tenant-pool-only reporting authority across every reporting/admin query |
| Date/range validation | Backend #82 certifies strict UTC daily `YYYY-MM-DD` validation; Frontend #73 removes fake custom ranges from the fixed-window mobile report screen; Backend #86 adds one bounded V1 contract for sales/profit explicit ranges: paired real `YYYY-MM-DD` values, UTC day normalization, reversed/invalid rejection and a 366-day maximum while preserving the existing previous-month default when no range is supplied | CERTIFIED | preserve bounded deterministic UTC range semantics and fail closed before PostgreSQL on malformed or oversized explicit ranges |
| Large-result safety | Backend #86 bounds explicit sales/profit date windows to 366 days; several detail lists are independently bounded, including mobile low-stock pagination, but Reporting V1 has no unified pagination/result-size matrix yet | PARTIAL | certify bounded result sizes/pagination for remaining administrative detail queries and avoid unbounded exports |
| Support/data-quality reporting | Backend contains data-quality/support reporting surfaces | PARTIAL | inventory existing routes, certify permissions, tenant scope, actionable diagnostics and credential-safe output |
| Frontend reporting/admin UX | Frontend #73 certifies the routed mobile Reports screen uses the real fixed Central windows, does not show unavailable reporting as fake zero, and exposes an actionable Retry; other dashboard/GST/admin/support surfaces remain under audit | PARTIAL | certify remaining supported routed screens, role visibility, loading/error/empty/retry states, accessibility and no stale browser authority |
| Export/download behavior | existing reporting/export behavior has not yet been authoritatively inventoried | GAP | certify only V1-supported exports, content type/filename, permission and tenant scope; mark unsupported formats N/A |
| Cross-repository release acceptance | Reporting branch-provenance E2E exists and Backend #80/#83/#87 add real PostgreSQL branch, return/replay and branch-inventory acceptance, but no full Reporting/Admin merged-main release gate exists yet | PARTIAL | merged Frontend + POS + Backend/PostgreSQL acceptance using canonical transaction/return/inventory/customer facts and trusted branch scope |

## Current certified evidence

- Backend #76: report permission authority uses the existing `reports:read` catalog and originally failed closed for branch-restricted users until trusted branch query scoping was implemented.
- Backend #77: new canonical POS `sale.completed` orders are transactionally bound to the active Central-registered device branch; missing provenance and cross-branch rebinding fail closed.
- POSService #244: real restarted POS SQLite durable outbox → production Central sync route on merged Backend `main` → PostgreSQL proves canonical POS order branch provenance and duplicate/replay safety.
- Backend #78: existing canonical sales/profit/product aggregates are explicitly certified refund-aware using `orders.returned_amount` and returned item quantities.
- Backend #80: sales, daily, profit, profit graph and product-performance reports use trusted Central branch scope; branch users cannot widen scope with caller input; a real PostgreSQL branch-A/branch-B gate proves isolation plus partial-return net revenue.
- Backend #81: report controllers remain bound to the supplied tenant PostgreSQL pool for identical requests and never fall back to the default/global database authority.
- Backend #82: `/reports/daily` consumes canonical immutable order/return snapshots for revenue, uses strict UTC day boundaries and trusted branch scope, and rejects invalid date-only input before querying PostgreSQL.
- Backend #83: real PostgreSQL branch acceptance includes a fully returned sale, proves that sale contributes zero net revenue/quantity/profit, and verifies replaying the same canonical full-return state does not change reporting output.
- Backend #84: mobile reporting requires `reports:read`, uses certified report scope, scopes mobile sales/profit/recent-order facts to the trusted branch and never falls back to a global DB pool.
- Frontend #73: mobile report UX labels only the fixed Today/Week/Month windows it actually receives, exposes unavailable data as unavailable rather than zero, and provides explicit retry state with production-build acceptance.
- Backend #85: `/dashboard/overview` requires `reports:read`, consumes only trusted Central report branch scope, cannot widen scope from caller `branch_id`, and suppresses legacy branch low/dead-stock widgets/inventory insights until certified inventory truth is used.
- Backend #86: sales/profit explicit report ranges require paired real `YYYY-MM-DD` dates, deterministic UTC day bounds, correct ordering and at most 366 days; the existing previous-month default remains when no range is supplied.
- Backend #87: branch-scoped inventory reports now separate physical stock, sellable non-expired stock, expired stock, provisional/unallocated POS oversell deficit and projected net stock. Real PostgreSQL acceptance seeds distinct branch-A/branch-B non-batch stock, batches, expiry and unallocated sale/return allocations and proves branch isolation without guessing stock authority.
- Historical canonical POS orders whose branch provenance was never recorded are not guessed or backfilled from current device registration because device reassignment can make that inference unsafe; they remain a reporting data-quality/reconciliation concern until exact provenance is available.

## First ordered work

1. Finish remaining Dashboard/Admin KPI route and Frontend-state audit; wire inventory widgets only from the certified branch inventory contract.
2. Certify bounded result sizes/pagination for remaining administrative detail queries and supported exports.
3. Certify product-history, tax/GST and customer/outstanding reporting against their already-certified canonical snapshots.
4. Certify support/data-quality reporting permissions, tenant scope and credential-safe diagnostics.
5. Add the final Reporting/Admin cross-repository release gate and mark every row CERTIFIED or explicitly justified N/A.

## Release decision

**NOT YET RELEASE CERTIFIED.** Reporting/Admin V1 remains acceptance-first. No new reporting subsystem should be added until the existing Central queries, permissions and Frontend surfaces are fully inventoried and the real gaps above are proven.