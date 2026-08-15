# SHAJRetailProducts V1 Customers Acceptance Matrix

Status: **RELEASE CERTIFIED**

## Authority boundary

- Central Backend/PostgreSQL is canonical for customer identity, lifecycle, credit/outstanding balance, ledger/payment history, recovery and tenant/branch authority.
- POSService/SQLite is the small offline edge projection used for cashier lookup and permitted offline customer capture/update.
- POS-originated customer facts converge durably and idempotently to Central. Central-originated canonical customer changes project back to POS through the authenticated change feed.
- Frontend is never the canonical customer or credit authority.

## Acceptance matrix

| Capability | Current evidence | Status | Release requirement |
|---|---|---|---|
| POS offline customer search | POS #186 proves real SQLite search of active customers by name, phone, email and customer code, including inactive-customer exclusion | CERTIFIED | satisfied by merged POS #186 |
| POS offline customer create/update | POS #186 proves validated transactional create/update, monotonic local versions, durable `customer.changed` emission, inactive lifecycle persistence and fail-closed invalid input | CERTIFIED | satisfied by merged POS #186 |
| POS durable customer outbox | create/update emits versioned `customer.changed` in the same SQLite transaction; POS #182 proves restart/dispatch/replay through Central | CERTIFIED | satisfied by merged POS #182 cross-repo Customer E2E |
| Central customer CRUD/search | Backend #48 proves canonical create/update/list/detail stay within the supplied tenant database and that customer CRUD is tenant-global rather than branch-filtered | CERTIFIED | satisfied by merged Backend #48 |
| POS -> Central customer convergence | Backend #46 maps immutable POS customer IDs to canonical Central IDs without applying POS financial snapshots; POS #182 proves real SQLite -> durable outbox -> restart -> Central/PostgreSQL convergence and replay | CERTIFIED | satisfied by merged Backend #46 + POS #182 |
| Central -> POS customer projection | Backend #47 publishes canonical contact, lifecycle and Central-owned financial snapshots with known POS-local identity mappings; POS #184 proves authenticated Central change feed -> POS inbox/SQLite reconciliation without a duplicate canonical-ID row | CERTIFIED | satisfied by merged Backend #47 + POS #184 |
| Duplicate identity handling | Backend #46/#47 maintain immutable POS-local -> canonical identity mapping and source version; POS #184 reuses a mapped local row only when it exists on that device, preserves a newer pending local edit, and otherwise uses stable canonical identity | CERTIFIED | satisfied in both directions by Backend #46/#47 + POS #182/#184 |
| Sale association | Backend #46 resolves mapped POS customer identity into canonical `orders.customer_id`; POS #182 proves a customer-linked completed sale reaches Central associated to that canonical customer | CERTIFIED | satisfied by merged Backend #46 + POS #182 |
| Credit-limit authority | POS may carry an offline snapshot; Backend #46 never applies POS `credit_limit_minor` to canonical `customers.credit_limit`; POS #184 proves Central's canonical credit-limit snapshot projects back to the reconciled POS row | CERTIFIED | Central remains authoritative; satisfied by Backend #46/#47 + POS #182/#184 |
| Outstanding balance authority | Backend #50 deterministically recomputes canonical `customers.current_balance` from customer-linked order principal, captured payments, returned amount and Central customer payments; POS #191 proves a ₹100 sale with ₹60 captured derives ₹40 outstanding and a later ₹15 Central payment derives ₹25 through the real POS -> Central/PostgreSQL path | CERTIFIED | satisfied by merged Backend #50 + POS #191 |
| Customer payments/ledger | Backend #50 unifies stored outstanding with durable order/payment/return facts in PostgreSQL and rebuilds stale balances; POS #191 provides real cross-repository sale/payment evidence while refund and partial-return regressions remain green | CERTIFIED | satisfied by merged Backend #50 + POS #191 and green refund/partial-return exact-head regressions |
| Customer lifecycle/deactivation | Backend #47 projects canonical `is_active` state; POS #184 proves an inactive Central customer updates the same mapped offline row and remains excluded by the existing active-only customer search; POS #186 certifies local inactive lifecycle persistence/search exclusion | CERTIFIED | satisfied by Backend #47 + POS #184/#186 |
| Tenant isolation | Backend #48 proves identical customer identifiers in separate tenant pools cannot cross-read or cross-write | CERTIFIED | satisfied by merged Backend #48 |
| Branch/store applicability | Central canonical `customers` is deliberately tenant-global and has no branch/store identity; Backend #48 proves list/create/update/detail do not add branch/store filtering while remaining tenant-pool isolated | CERTIFIED | tenant-global across stores is the V1 policy; satisfied by merged Backend #48 |
| Offline restart/replay | POS #182 proves outbound customer state/outbox survive restart and duplicate delivery converges idempotently; #184 proves inbound canonical reconciliation through the transactional inbox | CERTIFIED | satisfied by merged POS #182/#184 |
| Diagnostics/support visibility | POS #189 proves poisoned outbound `customer.changed` and failed inbound `customer.upsert` remain support-visible with customer/message identity, attempt count, last error, payload and sync provenance | CERTIFIED | satisfied by merged POS #189 |
| Frontend customer UX | Frontend #38 keeps Central-owned financial snapshots read-only; Frontend #39 keeps configured local POS/SQLite search/create/update usable during internet outages, retains legacy queue only as a repository-failure fallback, and surfaces cached-data fallback errors | CERTIFIED | satisfied by merged Frontend #38/#39 and V1 Customers Frontend CI |

## Final V1 release acceptance

- Backend #50: deterministic PostgreSQL customer outstanding projection from canonical order/payment/return facts.
- POS #191: real SQLite customer/sale path through durable outbox, restart/replay, merged Backend `main`, PostgreSQL canonical outstanding, Central payment recomputation, plus exact-head Order/Payment/Receipt/Refund/Partial Return/POS regressions.
- Frontend #38/#39: Central-owned financial snapshots remain read-only and local POS customer operations remain available offline with actionable fallback behavior.
- No customer recovery, credit, or financial authority was moved into Frontend or POS.

## Release decision

**CUSTOMERS V1 RELEASE CERTIFIED.** Every matrix row is CERTIFIED, the principal cross-repository authority boundaries have executable evidence, and there are no unresolved critical Customers issues in the certified path.

Freeze Customers V1 except for real defects. Proceed to Store/Device Operations V1.
