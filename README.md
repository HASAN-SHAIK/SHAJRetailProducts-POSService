# SHAJRetail POS Service

Lightweight store-local service for SHAJRetail.

## Role

This repository owns the local POS runtime only:

- loopback HTTP API for the existing POS UI
- SQLite persistence
- local transaction boundaries
- transactional outbox and inbox
- offline-first synchronization with the central enterprise server
- store, terminal, device, shift, receipt and recovery concerns

It does not replace the central backend and it does not contain UI code.

## Architecture principles

1. The existing React UI remains visually and behaviorally unchanged.
2. The UI talks to this service through repository adapters.
3. A completed sale is fully durable in SQLite before any network call.
4. Orders, items, payments, inventory movements, receipt snapshots and outbox messages commit atomically.
5. Synchronization is idempotent and retry-safe.
6. RabbitMQ may be used centrally, but local selling depends only on SQLite and the local process.
7. The service is a modular monolith distributed as a single binary.
8. The service binds to loopback by default.

## Planned modules

- foundation
- database and migrations
- store/device identity
- catalog
- customers
- orders
- payments
- inventory
- receipts
- transactional outbox
- synchronization
- inbox/change feed
- authentication and local authorization
- backup and recovery
- observability
- packaging and installer
- frontend local API adapter
- central synchronization ingestion
- integration tests

Each module is developed on an isolated branch and reviewed before integration.
