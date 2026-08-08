# POS database recovery

The POS service creates verified SQLite snapshots in `POS_BACKUP_DIRECTORY` (default: `<database>.backups`). Each completed snapshot is written to a temporary file, checked with `PRAGMA quick_check`, checked for required POS tables, permissioned to owner read/write, and only then atomically renamed to its final `.db` name.

## Restore procedure

1. Stop the POS service. Never replace the database while the process is running.
2. Preserve the current database file and any `-wal` / `-shm` companions as a forensic copy.
3. Select the newest known-good snapshot. Validate it with `backup.ValidateRestoreCandidate` or an equivalent `PRAGMA quick_check` before replacement.
4. Replace the configured `POS_SQLITE_PATH` with the validated snapshot. Do not copy only a live WAL file.
5. Start the POS service. Startup migrations and integrity checks must succeed before the local API becomes usable.
6. Confirm `/api/v1/ready`, then inspect pending/failed outbox state. Unsynced completed sales remain in the snapshot and will resume normal HTTPS synchronization.

## Safety rules

- Backups include orders, payments, inventory movements, receipts, inbox checkpoints, device identity, and pending outbox events because they snapshot the entire SQLite database.
- Never restore an older snapshot merely to resolve a sync conflict; doing so can reintroduce already-published events. Central ingestion must remain idempotent.
- The local API token is stored separately from SQLite by default and is not included in database snapshots.
- Keep the forensic pre-restore database until the restored store has synchronized successfully.
