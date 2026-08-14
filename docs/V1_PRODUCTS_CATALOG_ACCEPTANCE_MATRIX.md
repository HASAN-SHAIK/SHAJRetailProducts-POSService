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
| Central product CRUD authority | PARTIAL | Central V1 product service/repository owns product lifecycle and branch-aware reads; focused Products V1 acceptance still required. |
| Product identity + name Central -> POS | PARTIAL | Change feed emits `catalog.product.upsert`; POS inbox persists versioned product projection. Needs real Central -> feed -> POS SQLite E2E. |
| Product deactivate/delete propagation | PARTIAL | Central soft-delete updates `is_deleted`; change feed maps this to POS `is_active=false`. Needs executable propagation acceptance. |
| Category fidelity Central -> POS | GAP | Central owns category as a product category name and exposes encoded category-name IDs, but the current change feed emits `category_id=null` and no `catalog.category.upsert`. |
| Barcode fidelity + offline lookup | PARTIAL | Central emits barcode upserts and POS supports local barcode lookup. Needs cross-repo E2E including barcode update/replay behavior. |
| Canonical price fact transport | PARTIAL | Central emits branch/global price facts and POS resolves effective local price. Detailed pricing, tax, promotions and rounding remain Pricing V1; Products V1 must certify lossless basic price transport only. |
| Branch/store applicability | PARTIAL | Product/price rows carry Central branch scope and POS prices support store scope. Needs device/store-scoped E2E and explicit tenant/branch isolation acceptance. |
| Change-feed cursor ordering / replay | PARTIAL | Backend has deterministic entity cursoring and POS inbox is idempotent/versioned. Needs catalog-specific multi-page/replay acceptance. |
| POS restart persistence | PARTIAL | Catalog lives in SQLite and inbox application is transactional. Needs restart/offline lookup acceptance after Central sync. |
| Offline name lookup | PARTIAL | POS supports local name search. Needs real synchronized-product acceptance. |
| SKU lookup | N/A | Current Central V1 product authority has no canonical SKU field. Do not invent a POS-only SKU authority in this domain. |
| Product description | N/A | Current Central V1 product authority has no canonical description field. |
| Unit-of-measure fidelity | GAP | Central exposes weight-based behavior but no canonical UOM value; POS currently receives hardcoded `unit`. Do not infer kg/g/litre without an authoritative model. |
| HSN/GST/cess transport | PARTIAL | Central owns HSN/GST/cess product facts, while current POS product projection does not carry an equivalent authoritative tax model. Exact taxation behavior is finalized in Pricing/Tax V1. |
| Batch-enabled catalog flag | N/A | Central batch allocation/convergence is already Inventory V1 authority; POS does not need to become batch-allocation authority. Re-open only if a cashier/catalog defect proves the flag is required locally. |
| Manual-price flag | N/A | Current Central product authority does not own a canonical manual-price flag. Pricing V1 will define any such permission/policy. |
| Product import -> canonical catalog | PARTIAL | Central has product import capability and Frontend has current import UI. Needs acceptance proving import writes canonical product facts and converges to POS rather than creating a parallel client authority. |
| Transactional POS inbox application | CERTIFIED | Existing POS inbox applies supported Central catalog messages in one SQLite transaction and records applied/failed state with duplicate-message idempotency. |
| Version monotonicity | CERTIFIED | POS product/category/price upserts reject older versions via `excluded.version >= current.version` semantics. |
| Tenant isolation | PARTIAL | Change feed runs on the resolved tenant DB. Products V1 needs explicit cross-tenant acceptance before release certification. |
| Operator/support sync diagnostics | PARTIAL | Generic POS sync diagnostics exist; catalog-specific stale/failed change visibility is accepted only after catalog E2E failure/recovery evidence. |

## First ordered closure slice

1. Fix category fidelity using the category identity already exposed by Central; do not invent a new category authority.
2. Add focused Backend acceptance for product/category/barcode/active/price change-feed messages and cursor/replay behavior.
3. Add real cross-repository E2E: Central canonical product change -> authenticated change feed -> POS transactional inbox -> SQLite -> offline name/barcode lookup, including restart and replay.
4. Certify soft-delete/deactivation and branch/store applicability on the same path.
5. Audit product import against the same canonical path before expanding any catalog schema.
6. Resolve authoritative UOM/weight semantics only if required for V1 cashier correctness; otherwise carry the gap into the later pricing/tax model rather than guessing units.

## Release rule

Products / Catalog V1 is release-certified only when every non-N/A row above is CERTIFIED or explicitly moved to a later V1 domain with an architecture justification and executable boundary acceptance. Once release-certified, freeze this domain except for real defects and proceed to Pricing / Promotions / Tax / Rounding V1.
