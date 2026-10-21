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

## D3 Lyrics
- Order: sidecar `.lrc` (same basename) → embedded tag → optional provider;
  synced beats plain. Sidecars are read at request time (no rescan after
  editing), must resolve inside a library root (symlink escape refused), ≤1 MiB,
  BOM stripped, invalid UTF-8 decoded as GB18030.
- Embedded lyrics are stored in `source_tracks.lyrics` (≤256 KiB). Migration 0006
  clears `codec` so the next scan re-reads every file once to pick them up.
- LRC: multiple timestamps per line, `[offset]` applied server-side, enhanced
  word timing `<mm:ss.xx>` stripped (line-level only for now).

## D4 History, favorites, ratings
- A play counts (Last.fm rule) when the track ended, or ≥ min(50 %, 4 min) was
  heard; tracks ≤30 s must complete. Core-managed players are recorded when the
  orchestrator leaves a track (skip, end, replace, stop); seeking is not leaving.
  `POST /api/scrobble` records client-side plays verbatim (client applies the rule).
- `plays`, `track_stats`, `favorites`, `ratings` have **no FK** to tracks: user
  data survives a file being temporarily missing (NAS offline) and reattaches
  because canonical IDs are deterministic. Listings mark such rows `available:false`.
- `track_stats` is a denormalized counter so sorting by plays scales (no
  aggregate over `plays` per list query).
- Single-user model: favorites/ratings are global, not per device.
