# V1 Products / Catalog Acceptance Matrix

## Authority and scope

Central Backend is the canonical authority for tenant products, product lifecycle, category names, barcode identity, branch applicability, and canonical product pricing facts. POSService is a small offline projection that receives authenticated Central change-feed facts through its transactional/idempotent inbox and serves local cashier lookup. Frontend is an administrative/cashier client only and does not become catalog authority.

Inventory V1 is release-certified and frozen except for real defects. Pricing/promotion/tax/rounding semantics beyond catalog fact transport remain owned by the later Pricing V1 domain.

## Status legend

- **CERTIFIED** — implementation and focused executable acceptance exist.
- **PARTIAL** — capability exists but V1 end-to-end acceptance or fidelity is incomplete.
- **GAP** — a real release gap is identified.
- **N/A** — intentionally outside Products / Catalog V1 and justified here.

## Acceptance matrix

| Capability | Status | Current evidence / required acceptance |
| --- | --- | --- |
| Central product CRUD authority | CERTIFIED | Backend #41 proves branch-scoped listing/barcode lookup, resolved-branch default creation, explicit admin branch targeting, canonical update, and soft deletion through the Central product repository. |
| Product identity + name Central -> POS | CERTIFIED | Backend #35 + POSService #150 exercise the authenticated Central feed into the transactional POS inbox and real SQLite, including offline product identity/name lookup. |
| Product deactivate/delete propagation | CERTIFIED | POSService #156 proves Central soft-delete -> authenticated feed -> inactive POS SQLite projection, removal from cashier barcode/name lookup, restart persistence and replay idempotency. |
| Category fidelity Central -> POS | CERTIFIED | Backend #35/#36 + POSService #150/#151/#152 preserve category identity and certify authoritative category rename/removal snapshots without ghost categories. |
| Barcode fidelity + offline lookup | CERTIFIED | POSService #150/#152 certify initial Central barcode transport; POSService #153 atomically replaces stale primary barcodes and proves Central barcode change -> authenticated feed -> SQLite offline lookup/restart/replay. |
| Canonical price fact transport | CERTIFIED | POSService #150/#152 prove Central branch-scoped `selling_price` reaches POS SQLite as the effective INR price for the synchronized store. Detailed pricing/tax/promotion semantics remain Pricing V1. |
| Branch/store applicability | CERTIFIED | Backend #37 + POSService #157 bind the feed to an active Central device registration and isolate branch-A devices from branch-B name/barcode/category/price facts. Backend #38 + POSService #159 add an ID/version-only removal tombstone so a product reassigned away from the trusted branch is deactivated and its stale barcode/price lookup facts are removed; returning it to the branch reactivates the canonical projection. |
| Change-feed cursor ordering / replay | CERTIFIED | POSService #150/#152/#153/#156/#157/#159 certify persisted restart/replay; Backend #39 certifies deterministic mixed product/customer multi-page ordering, exact cursor-boundary continuation, and final replay without duplicate entities. |
| POS restart persistence | CERTIFIED | POSService #150/#152/#153/#156/#157/#159 restart the same SQLite database and verify synchronized catalog state remains authoritative offline. |
| Offline name lookup | CERTIFIED | POSService #150/#152/#156/#157/#159 verify synchronized active Central products are searchable offline and deactivated/reassigned/foreign-branch products are not. |
| SKU lookup | N/A | Current Central V1 product authority has no canonical SKU field. Do not invent a POS-only SKU authority in this domain. |
| Product description | N/A | Current Central V1 product authority has no canonical description field. |
| Unit-of-measure fidelity | N/A | Products/Catalog V1 does not infer kg/g/litre from Central's current placeholder `unit` label. POSService #165 proves cashier math is driven by explicit `QuantityMilli`, preserving correct fractional calculation without inventing product-unit semantics. Canonical UOM/weight semantics are transferred to Pricing/Tax V1. |
| HSN/GST/cess transport | N/A | Central owns HSN/GST/cess facts, but exact tax interpretation and transport semantics are explicitly transferred to Pricing/Tax V1 rather than duplicated in Products/Catalog. Products/Catalog only transports canonical product/price identity facts. |
| Batch-enabled catalog flag | N/A | Central batch allocation/convergence is already Inventory V1 authority; POS does not need to become batch-allocation authority. Re-open only if a cashier/catalog defect proves the flag is required locally. |
| Manual-price flag | N/A | Current Central product authority does not own a canonical manual-price flag. Pricing V1 will define any such permission/policy. |
| Product import -> canonical catalog | CERTIFIED | Backend #40 certifies Frontend-shaped import rows become one branch-scoped canonical Central product/batch and re-import updates rather than duplicates. POSService #163 proves the merged Central import path through PostgreSQL, authenticated change feed, transactional POS inbox/SQLite, and offline barcode/name/effective-price lookup. |
| Transactional POS inbox application | CERTIFIED | Existing POS inbox applies supported Central catalog messages in one SQLite transaction and records applied/failed state with duplicate-message idempotency. |
| Version monotonicity | CERTIFIED | POS product/category/price upserts and branch-removal tombstones reject older versions; category snapshots also preserve newer category facts. |
| Tenant isolation | CERTIFIED | POSService #161 runs the production authenticated Central route against two independent tenant PostgreSQL databases and registered devices: tenant A sees only tenant-A catalog facts, tenant B sees only tenant-B facts, and a tenant-A token is rejected when presented for tenant B. |
| Operator/support sync diagnostics | CERTIFIED | POSService #165 proves failed Central catalog inbox messages remain visible through existing support diagnostics with message identity/type, failed status, attempts, last error, source, and payload. |

## Release certification

**Products / Catalog V1 is RELEASE-CERTIFIED.** Every capability owned by this domain is certified, while SKU/description, concrete UOM/tax semantics, batch authority, and manual-price policy are explicitly outside this domain with architecture justification. Transaction Core and Inventory remain frozen except for real defects.

The final release evidence is the combined green exact-head acceptance already merged across Backend #35-#41 and POSService #150-#165, including real PostgreSQL -> authenticated Central change feed -> POS transactional inbox/SQLite -> offline lookup, restart/replay, branch/tenant isolation, lifecycle tombstones, product import convergence, cursor ordering, CRUD authority, fractional-quantity boundary behavior, and catalog diagnostics.

## Ordered closure work

1. Freeze Products / Catalog V1 except for real defects.
2. Proceed to Pricing / Promotions / Tax / Rounding V1, taking ownership of canonical UOM/weight interpretation, HSN/GST/cess behavior, manual-price policy, promotion semantics, tax inclusivity, and rounding.

## Release rule

Products / Catalog V1 is release-certified only when every non-N/A row above is CERTIFIED or explicitly moved to a later V1 domain with an architecture justification and executable boundary acceptance. This condition is now satisfied.
