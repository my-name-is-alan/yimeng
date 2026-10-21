# Core design decisions

Decisions taken while implementing Core autonomously; each lists the
alternative considered. Newest items at the bottom.

## D1 Backup / restore
- Snapshots use `VACUUM INTO` (consistent, compacted, works while serving) packed
  as a zip (`.ymbak`: `library.db` + `manifest.json`).
- Vault credentials are rows in `vault_entries` (AES-256-GCM), so every snapshot
  contains them **encrypted**. The master key is never written to stored backups;
  `GET /api/system/export?include_key=true` can add it explicitly for migration
  to a new host.
- Restore is **staged**: the upload is validated (zip, manifest, SQLite
  `integrity_check`, schema not newer than the binary) and moved to
  `<db>.restore-pending`; it is swapped in at the next start (old DB kept as
  `<db>.pre-restore-<ts>`). Hot-swapping a live SQLite file under open
  connections/goroutines was rejected as unsafe.
- An automatic backup (`pre-migrate-N`) runs before pending migrations on an
  existing database; failure aborts startup rather than migrating unprotected.
- Scheduled backups: `YIMENG_BACKUP_INTERVAL` (24h), keep `YIMENG_BACKUP_KEEP` (7).
  Rotation applies to all stored backups regardless of reason.
- Cover/transcode caches are not backed up (re-derivable).

## D2 Device roles
- Backup, restore and other system/admin endpoints need an **admin device**.
  Migration 0005 makes the oldest paired device admin; whenever no admin exists
  the next paired device becomes admin. The last admin cannot be demoted.
  Rejected: a separate admin password (one more secret to manage).
