# RetailHub Dashboard Migration Matrix

Status: IN PROGRESS — runtime migration complete; dead-code cleanup and final cross-repository release certification pending.

## Goal

Move business-management metrics out of the store POS experience and into SHAJ Retail Hub without changing metric authority or calculation semantics.

Architecture boundary:

- POS remains the store execution and edge-operations surface.
- Central Backend/PostgreSQL remains canonical business/reporting authority.
- SHAJ Retail Hub lives in `HASAN-SHAIK/SHAJRetailProducts-CustomerHub` and is the owner/manager business-management surface.
- RetailHub consumes Central APIs and must not calculate canonical revenue, profit, inventory, customer-credit, tax, category, or branch-performance facts in browser code.
- POS-only operational facts remain at the edge: POSService health, sync/outbox/dead-letter state, local backup visibility, register/session state, and immediate store-continuity concerns.

## Migration evidence

| Metric family | Canonical authority | RetailHub implementation | Status |
| --- | --- | --- | --- |
| Sales / revenue / order count / average order value | Central `/dashboard/revenue-overview` + PostgreSQL | CustomerHub #2 | CERTIFIED |
| Profit, revenue/order growth, sales trends | Central `/dashboard/growth-comparison`, `/dashboard/sales-trend` | CustomerHub #3 | CERTIFIED |
| Inventory management analytics | Central `/reports/inventory` trusted reporting scope | CustomerHub #4 | CERTIFIED |
| Products / categories | Central `/dashboard/category-performance` immutable sale/category snapshots | CustomerHub #5 | CERTIFIED |
| Customers / credit | Central `/reports/customers-outstanding` canonical customer balance projection | CustomerHub #6 | CERTIFIED |
| Payments / refunds dashboard family | No distinct POS dashboard metric family existed | — | N/A |
| Branch / location performance | Central `/dashboard/location-performance` | CustomerHub #7 | CERTIFIED |
| Smart Insights | Central `/dashboard/smart-insights` governed canonical facts | CustomerHub #8 | CERTIFIED |
| POS management-dashboard runtime removal | Local POS operational authority only | Frontend #84 | CERTIFIED |
| Retired POS management-dashboard source deletion | No runtime authority; cleanup only | Frontend #85 | UNDER VALIDATION |
| Final cross-repository migration release gate | RetailHub + Frontend + Central/POS authority | pending | NOT RUN |

## RetailHub target boundary

RetailHub now owns management analytics for:

- revenue, profit, order count and average order value;
- growth and sales trends;
- Central branch inventory analytics;
- product/category performance;
- customer outstanding and credit exposure;
- location/branch performance;
- governed Smart Insights.

All migrated screens require explicit loading/error/retry/empty/data behavior and consume Central reporting authority rather than POS SQLite or duplicated browser formulas.

## Final POS dashboard boundary

The routed POS `/dashboard` is now the POS Operational Dashboard. It may expose only store-runtime concerns such as:

| Operational family | Authority | Decision |
| --- | --- | --- |
| POSService health/readiness | Local POSService | KEEP |
| Durable outbox pending/dead-letter status | Local SQLite/POSService | KEEP |
| Inbox/replay failure status | Local SQLite/POSService | KEEP |
| Local backup visibility | Local POSService | KEEP |
| Sync Center access and operational diagnostics | Local POSService | KEEP |
| Device/session/register continuity concerns | POSService + Central-issued identity | KEEP |
| Immediate local inventory availability needed to sell | Local SQLite/POSService | KEEP outside management analytics |
| Revenue/profit/growth/inventory-management/category/customer/branch/smart analytics | Central/RetailHub | REMOVED FROM POS |

Frontend #84 replaced the old `DashboardOverview` route with `POSOperationalDashboard`, removed the Central management-dashboard network/retry contract from POS, and certified explicit local POS-unavailable + Refresh POS behavior. Its exact head passed both the focused Frontend completion gate and the complete Frontend Jest/build suite before merge.

## Acceptance rules

The migration is release-ready only when all of the following are true:

- Central/PostgreSQL remains the source of truth for every moved business metric.
- RetailHub does not reproduce canonical business formulas in browser code.
- Tenant isolation and trusted branch/store scope remain server-side.
- Role/permission behavior continues to use Central identity authority.
- Date/range semantics remain those of the canonical Central APIs.
- Returns/refunds/offline sales affect metrics only after canonical Central convergence under existing V1 rules.
- Duplicate/replay behavior cannot double-count canonical reporting facts.
- RetailHub production dashboard acceptance and build are green.
- POS complete Frontend suite/build is green after management dashboard removal.
- Retired management-dashboard code is removed or proven unreachable.
- A final cross-repository gate validates current merged CustomerHub, Frontend, Backend/POS reporting authority and the POS operational boundary.

## Completion criteria

This migration is complete only when:

1. every business-management metric formerly rendered by POS is present and certified in RetailHub or explicitly N/A;
2. RetailHub consumes Central canonical reporting authority;
3. POS no longer renders management analytics or retains reachable management-dashboard code;
4. POS retains only edge-operational dashboard information;
5. the final cross-repository RetailHub dashboard migration release gate is green.
