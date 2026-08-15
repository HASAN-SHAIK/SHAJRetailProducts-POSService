# SHAJRetailProducts V1 Customers Acceptance Matrix

Status: **IN PROGRESS**

## Authority boundary

- Central Backend/PostgreSQL is canonical for customer identity, lifecycle, credit/outstanding balance, ledger/payment history, recovery and tenant/branch authority.
- POSService/SQLite is the small offline edge projection used for cashier lookup and permitted offline customer capture/update.
- POS-originated customer facts must converge durably and idempotently to Central. Central-originated canonical customer changes must project back to POS before this domain is release-certified.
- Frontend is never the canonical customer or credit authority.

## Acceptance matrix

| Capability | Current evidence | Status | Release requirement |
|---|---|---|---|
| POS offline customer search | SQLite repository searches active customers by name, phone, email and customer code | PARTIAL | focused restart/offline acceptance |
| POS offline customer create/update | SQLite repository validates and persists customer facts transactionally | PARTIAL | focused local lifecycle acceptance |
| POS durable customer outbox | create/update emits versioned `customer.changed` in the same SQLite transaction | PARTIAL | dispatcher/restart/duplicate acceptance |
| Central customer CRUD/search | existing Central Customers module owns canonical create/update/list/detail behavior | PARTIAL | focused authority/isolation acceptance |
| POS -> Central customer convergence | no release evidence yet that `customer.changed` becomes one canonical Central customer exactly once | GAP | real SQLite -> outbox -> Central/PostgreSQL E2E |
| Central -> POS customer projection | no release evidence yet for canonical Central customer lifecycle/change projection to POS | GAP | authenticated Central change -> POS inbox/SQLite E2E |
| Duplicate identity handling | Central has phone/name dedupe behavior; POS owns local IDs | PARTIAL | define immutable POS/Central identity mapping and replay semantics |
| Sale association | transaction model supports customer association, but customer identity convergence is not yet certified | PARTIAL | offline sale with customer -> Central canonical association E2E |
| Credit-limit authority | POS stores `credit_limit_minor`; Central owns canonical credit fields | PARTIAL | Central-authoritative offline snapshot + enforcement boundary |
| Outstanding balance authority | POS stores `outstanding_minor`; Central owns customer balance/ledger | PARTIAL | prove POS cannot become financial authority and refreshes canonical balance |
| Customer payments/ledger | Central Customers module exposes payment and ledger behavior | PARTIAL | transactional ledger/payment acceptance; POS scope decision |
| Customer lifecycle/deactivation | POS search excludes inactive customers; Central has `is_active` lifecycle | PARTIAL | Central deactivate/reactivate -> POS projection acceptance |
| Tenant isolation | tenant DB boundary exists in Central | PARTIAL | explicit cross-tenant customer isolation acceptance |
| Branch/store applicability | not yet certified for customer visibility/creation | GAP | define V1 global-vs-branch customer policy and test device/store behavior |
| Offline restart/replay | POS customer facts persist locally | PARTIAL | restart + lost-ack + duplicate convergence acceptance |
| Diagnostics/support visibility | generic outbox/inbox diagnostics exist | PARTIAL | customer-specific failed/dead-letter identity visible to support |
| Frontend customer UX | existing screens/APIs not yet audited against certified POS/Central authority | GAP | cashier create/search/select/error/offline/sync acceptance |

## Ordered V1 work

1. Trace Central sync ingestion for `customer.changed`; implement only the missing canonical projection/idempotency boundary.
2. Add real POS SQLite -> durable outbox -> Central/PostgreSQL customer convergence E2E.
3. Define immutable POS-to-Central customer identity mapping and duplicate/lost-ack semantics.
4. Establish Central -> POS canonical customer projection and lifecycle sync.
5. Certify sale association and customer identity across offline transaction synchronization.
6. Freeze credit-limit/outstanding/ledger authority in Central and certify the minimum offline snapshot/enforcement behavior.
7. Certify tenant and branch/store visibility policy.
8. Audit and complete cashier/customer Frontend UX and support diagnostics.
9. Run final Customers V1 release acceptance; then freeze the domain except for real defects.

## Release decision

Customers V1 is release-certified only when every row above is **CERTIFIED** or explicitly justified **N/A**, with real cross-repository acceptance for the authority boundaries and no unresolved critical issues.
