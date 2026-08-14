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
| Central product CRUD authority | PARTIAL | Central V1 product service/repository owns product lifecycle and branch-aware reads; focused Products V1 CRUD acceptance still required. |
| Product identity + name Central -> POS | CERTIFIED | Backend #35 + POSService #150 exercise the authenticated Central feed into the transactional POS inbox and real SQLite, including offline product identity/name lookup. |
| Product deactivate/delete propagation | CERTIFIED | POSService #156 proves Central soft-delete -> authenticated feed -> inactive POS SQLite projection, removal from cashier barcode/name lookup, restart persistence and replay idempotency. |
| Category fidelity Central -> POS | CERTIFIED | Backend #35/#36 + POSService #150/#151/#152 preserve category identity and certify authoritative category rename/removal snapshots without ghost categories. |
| Barcode fidelity + offline lookup | CERTIFIED | POSService #150/#152 certify initial Central barcode transport; POSService #153 atomically replaces stale primary barcodes and proves Central barcode change -> authenticated feed -> SQLite offline lookup/restart/replay. |
| Canonical price fact transport | CERTIFIED | POSService #150/#152 prove Central branch-scoped `selling_price` reaches POS SQLite as the effective INR price for the synchronized store. Detailed pricing/tax/promotion semantics remain Pricing V1. |
| Branch/store applicability | PARTIAL | Backend #37 + POSService #157 bind the feed to an active Central device registration and prove a branch-A device receives only global/branch-A product/category facts, with branch-B barcode/name/category/price absent from SQLite. Branch reassignment/removal cleanup for a product already projected to the old branch remains open. |
| Change-feed cursor ordering / replay | PARTIAL | Persisted-cursor restart/replay is certified by POSService #150/#152/#153/#156/#157. Catalog-specific multi-page ordering acceptance remains open. |
| POS restart persistence | CERTIFIED | POSService #150/#152/#153/#156/#157 restart the same SQLite database and verify synchronized catalog state remains authoritative offline. |
| Offline name lookup | CERTIFIED | POSService #150/#152/#156/#157 verify synchronized active Central products are searchable offline and deactivated/foreign-branch products are not. |
| SKU lookup | N/A | Current Central V1 product authority has no canonical SKU field. Do not invent a POS-only SKU authority in this domain. |
| Product description | N/A | Current Central V1 product authority has no canonical description field. |
| Unit-of-measure fidelity | GAP | Central exposes weight-based behavior but no canonical UOM value; POS currently receives hardcoded `unit`. Do not infer kg/g/litre without an authoritative model. |
| HSN/GST/cess transport | PARTIAL | Central owns HSN/GST/cess product facts, while current POS product projection does not carry an equivalent authoritative tax model. Exact taxation behavior is finalized in Pricing/Tax V1. |
| Batch-enabled catalog flag | N/A | Central batch allocation/convergence is already Inventory V1 authority; POS does not need to become batch-allocation authority. Re-open only if a cashier/catalog defect proves the flag is required locally. |
| Manual-price flag | N/A | Current Central product authority does not own a canonical manual-price flag. Pricing V1 will define any such permission/policy. |
| Product import -> canonical catalog | PARTIAL | Central `imports.service` inserts/updates the canonical `products` table and the current Frontend uses the Central import API. Needs executable import -> Central change feed -> POS SQLite convergence acceptance. |
| Transactional POS inbox application | CERTIFIED | Existing POS inbox applies supported Central catalog messages in one SQLite transaction and records applied/failed state with duplicate-message idempotency. |
| Version monotonicity | CERTIFIED | POS product/category/price upserts reject older versions via `excluded.version >= current.version` semantics; category snapshots also preserve newer category facts. |
| Tenant isolation | PARTIAL | Change feed runs on the resolved tenant DB and now requires a registered branch device. Products V1 still needs explicit cross-tenant token/database acceptance before release certification. |
| Operator/support sync diagnostics | PARTIAL | Generic POS sync diagnostics exist; catalog-specific stale/failed change visibility is accepted only after catalog E2E failure/recovery evidence. |

## Ordered closure work

1. Close branch reassignment/removal cleanup so a product that moves away from a device branch cannot remain as a stale active local projection.
2. Add explicit cross-tenant catalog token/database isolation acceptance.
3. Add catalog-specific multi-page cursor ordering/replay acceptance.
4. Certify Frontend/Backend product import -> canonical Central product -> POS convergence without introducing client catalog authority.
5. Resolve authoritative UOM/weight semantics only if required for V1 cashier correctness; otherwise move the exact tax/UOM transport boundary into Pricing/Tax V1 with executable boundary acceptance.
6. Certify catalog-specific failure/diagnostic visibility and run final Products/Catalog release acceptance.

## Release rule

Products / Catalog V1 is release-certified only when every non-N/A row above is CERTIFIED or explicitly moved to a later V1 domain with an architecture justification and executable boundary acceptance. Once release-certified, freeze this domain except for real defects and proceed to Pricing / Promotions / Tax / Rounding V1.
