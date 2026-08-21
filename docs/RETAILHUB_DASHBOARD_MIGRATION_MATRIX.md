# RetailHub Dashboard Migration Matrix

Status: IN PROGRESS

## Goal

Move business-management metrics out of the store POS experience and into RetailHub without changing metric authority or calculation semantics.

Architecture boundary:

- POS remains the store execution and edge-operations surface.
- Central Backend/PostgreSQL remains canonical business/reporting authority.
- RetailHub becomes the owner/manager business-management surface and must consume Central APIs.
- RetailHub must not calculate canonical revenue, profit, inventory, customer-credit, refund, tax, or branch-performance facts in browser code.
- POS-only operational facts remain at the edge: device health, Central connectivity, sync/outbox/dead-letter state, local register/session/shift state, and immediate store continuity alerts.
- A POS management metric is removed only after its RetailHub replacement is acceptance-certified against the same Central authority.

## Current POS dashboard inventory

The current POS Frontend dashboard (`src/components/Dashboard/DashboardOverview/DashboardOverview.js`) contains the following business-management surfaces.

| Metric family | Current POS source | Decision | Target authority | Migration order | Status |
| --- | --- | --- | --- | --- | --- |
| Basic business overview: total products, low stock, expiry counts, order counts, total revenue | `GET /dashboard` | MOVE | Central Backend/PostgreSQL | 1 | INVENTORIED |
| Revenue overview: total revenue, total profit, total orders, average order value | `GET /dashboard/revenue-overview` | MOVE | Central Backend/PostgreSQL | 1 | INVENTORIED |
| Locations selector | `GET /dashboard/locations-list` plus trusted branch scope | MOVE | Central identity/branch authority | Shell | INVENTORIED |
| Growth comparison: revenue, profit, order growth | `GET /dashboard/growth-comparison` | MOVE | Central Backend/PostgreSQL | 2 | INVENTORIED |
| Sales trend, including location grouping | `GET /dashboard/sales-trend` | MOVE | Central Backend/PostgreSQL | 2 | INVENTORIED |
| Category/product performance | `GET /dashboard/category-performance` | MOVE | Central Backend/PostgreSQL | 4 | INVENTORIED |
| Location/branch performance | `GET /dashboard/location-performance` | MOVE | Central Backend/PostgreSQL | 7 | INVENTORIED |
| Customer credit/outstanding | `GET /dashboard/customer-credit` | MOVE | Central Backend/PostgreSQL | 5 | INVENTORIED |
| Smart insights | `GET /dashboard/smart-insights` | MOVE | Central canonical facts / governed analytics | 8 | INVENTORIED |
| Inventory intelligence | local `fetchInventoryIntelligence(...)` | SPLIT | Central for management analytics; POS only for immediate edge-operational inventory truth | 3 | NEEDS CONTRACT SPLIT |
| Owner digest/test-report email action | `POST /owner-digest/send-test-email` | MOVE | Central | 9 | INVENTORIED |
| Dashboard plan/feature entitlements | tenant config + role checks | MOVE | Central identity/permission/entitlement authority | Shell | INVENTORIED |

## Required POS-only dashboard after migration

The final POS dashboard must be operational rather than managerial. It may retain or gain only information needed to run the local store safely:

| Operational family | Authority | Decision |
| --- | --- | --- |
| POSService health/readiness | Local POSService | KEEP |
| Central connectivity/reconnect state | Local POSService | KEEP |
| Durable outbox pending/failed/dead-letter status | Local SQLite/POSService | KEEP |
| Inbox/replay/sync cursor status | Local SQLite/POSService | KEEP |
| Registered device/terminal identity and branch binding | POSService + Central-issued authority | KEEP |
| Current cashier/local session/register state | Local POSService | KEEP |
| Immediate local inventory availability required for selling | Local SQLite/POSService | KEEP |
| Business revenue/profit/growth/category/customer/branch analytics | Central | REMOVE FROM POS after RetailHub certification |

## Migration sequence

1. RetailHub shell
   - authenticated tenant context
   - permission/role boundary
   - branch/store selector
   - date-range selector
   - explicit Loading / Error + Retry / Empty / Data states
   - Central-only API client

2. Sales and revenue
   - total revenue
   - net/canonical revenue semantics already defined by Central reporting
   - order count
   - average order value
   - basic order-status summary where management-relevant

3. Profit and trends
   - total profit
   - revenue/profit/order growth
   - sales trend

4. Inventory management analytics
   - total products
   - low-stock count
   - expiry counts
   - Central inventory intelligence contract
   - preserve local POS availability only for selling/edge operations

5. Products/categories
   - category/product performance

6. Customers/credit
   - canonical customer outstanding/current balance and credit exposure

7. Payments/refunds
   - management-facing payment/refund summaries from canonical Central facts

8. Branch/store comparisons
   - location performance
   - tenant-wide / trusted-branch comparisons

9. Trends/analytics
   - smart insights only from canonical/governed facts

10. Admin/support actions
   - owner digest/report delivery
   - business support views that do not belong on cashier POS

11. POS cleanup
   - remove each management section only after the corresponding RetailHub acceptance is green
   - retain operational edge dashboard only

## Acceptance rules per metric family

Every moved family must prove all of the following before POS removal:

- Central/PostgreSQL is the source of truth.
- RetailHub does not reproduce canonical business formulas in browser code.
- Tenant isolation is enforced server-side.
- Branch/store scope is trusted server-side and cannot be widened by client parameters.
- Role/permission/plan entitlement behavior matches Central authority.
- Date-range and timezone semantics are explicit and regression-tested.
- Refunds, returns, voids, partial returns, and offline sales are reflected only after canonical Central convergence according to existing V1 rules.
- Duplicate/lost-ack/replay scenarios do not double-count metrics.
- Loading, Error + Retry, Empty, and Data states are distinct.
- Production build passes.
- Focused cross-repository acceptance compares the RetailHub result to the certified Central reporting contract.
- POS metric removal happens in a later or dependency-ordered change after RetailHub acceptance is green.

## Repository discovery note

The connected GitHub installation currently exposes `SHAJRetailProducts-AdminDashboard`, but its current product boundary is platform/SaaS administration (tenant management, subscription revenue, plans, platform activity) rather than store/tenant RetailHub management. It must not be silently repurposed as RetailHub without an explicit product/repository decision.

No connected repository named RetailHub is currently present. Therefore UI implementation is blocked only on resolving the authoritative RetailHub repository/location; Central/POS inventory, migration contracts, and acceptance planning can proceed independently.

## Completion criteria

This migration is complete only when:

1. every business-management metric currently rendered by POS is present and certified in RetailHub or intentionally retired;
2. RetailHub consumes Central canonical reporting authority;
3. POS no longer renders management revenue/profit/growth/category/customer/branch analytics;
4. POS retains only edge-operational dashboard information;
5. full cross-repository release acceptance is green.
