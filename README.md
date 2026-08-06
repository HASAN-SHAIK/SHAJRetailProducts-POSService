# SHAJRetail POS Service

Lightweight store-local service for SHAJRetail.

## Responsibilities

- Local-first POS transactions
- SQLite persistence
- Transactional outbox and inbox
- Offline synchronization with the central enterprise server
- Device/store identity
- Local catalog, customer, order, payment, inventory, receipt, shift, backup, recovery, and observability modules

## Architecture rule

This repository contains one lightweight modular service. It is not a collection of separately deployed store-side microservices.

The existing React POS UI and the central enterprise backend remain separate systems.
