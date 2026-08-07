# POS integration validation

This module validates the boundaries introduced across the store-local POS, frontend adapter, and central sync gateway.

## Automated coverage

- SQLite migrations on a fresh temporary database.
- Transactional outbox claim ownership and retry delay semantics.
- POS -> central HTTP event envelope, tenant/device/sync-token headers, and idempotency key.
- HTTP 409 from central is treated as an idempotent acknowledgement of an already-committed event.
- Non-loopback central endpoints must use HTTPS and configured POS sync credentials.
- Central -> POS change feed applies through the inbox and advances the durable cursor only after successful application.
- A failed change does not advance the checkpoint.
- Completed sale regression path verifies one SQLite commit produces order completion, receipt, inventory movement, and a pending `sale.completed` outbox event.

## Cross-repository contract heads

Integration was authored against:

- POS: `agent/15-packaging-and-installer`
- Frontend: `agent/16-frontend-local-api-adapter`
- Backend: `agent/17-central-sync-ingestion`

The frontend adapter remains feature-flagged and does not modify React screens or workflows. The backend gateway remains independent of interactive tenant JWT sessions and is intended for headless store service traffic.

## Required deployment smoke test

Before production rollout, run one store/device against a staging tenant and verify:

1. Bootstrap catalog/customer projections.
2. Disconnect central network access.
3. Create and pay a sale through the unchanged checkout UI.
4. Confirm receipt, inventory movement, and pending outbox event exist locally.
5. Restart the POS service while still offline and confirm the sale remains available.
6. Restore connectivity and confirm the event is acknowledged exactly once by central.
7. Confirm central product/customer edits return through the change feed and update local projections.
8. Repeat the same event ID and verify no duplicate central order is created.
9. Validate a backup and restore candidate with pending outbox state preserved.

Do not mark production rollout complete until the staging smoke test passes with the actual installer and tenant configuration.
