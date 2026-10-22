// Package system reports version, library statistics, subsystem state and
// self-diagnostics for /api/system.
package system

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/backup"
	"github.com/my-name-is-alan/yimeng/core/internal/library"
	"github.com/my-name-is-alan/yimeng/core/internal/transcode"
)

type Collector struct {
	DB              *sql.DB
	DBPath          string
	Roots           func() []string
	CacheDir        string
	CacheMaxBytes   int64
	Transcoder      *transcode.Transcoder
	Backup          *backup.Service
	LatestScan      func(ctx context.Context) (library.Job, error)
	Version         string
	SchemaVersion   int
	Started         time.Time
	LoudnessEnabled bool
	Features        map[string]bool
}

type RootStatus struct {
	Path      string `json:"path"`
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	Files     int    `json:"files"`
	FreeBytes int64  `json:"free_bytes,omitempty"`
}

type Diagnostic struct {
	Level   string `json:"level"` // info | warn | error
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Info struct {
	Version       string    `json:"version"`
	GoVersion     string    `json:"go_version"`
	OS            string    `json:"os"`
	Arch          string    `json:"arch"`
	StartedAt     time.Time `json:"started_at"`
	UptimeSec     int64     `json:"uptime_sec"`
	SchemaVersion int       `json:"schema_version"`
	Library       struct {
		Tracks     int   `json:"tracks"`
		Albums     int   `json:"albums"`
		Artists    int   `json:"artists"`
		Files      int   `json:"files"`
		Lossless   int   `json:"lossless_files"`
		Playlists  int   `json:"playlists"`
		Plays      int   `json:"plays"`
		DurationMs int64 `json:"duration_ms"`
		Bytes      int64 `json:"bytes"`
	} `json:"library"`
	Scan     *library.Job `json:"scan,omitempty"`
	Roots    []RootStatus `json:"roots"`
	Database struct {
		SizeBytes int64 `json:"size_bytes"`
		WALBytes  int64 `json:"wal_bytes"`
		FreeBytes int64 `json:"free_bytes,omitempty"`
	} `json:"database"`
	Transcode struct {
		Available     bool                   `json:"available"`
		Formats       []transcode.FormatInfo `json:"formats"`
		CacheBytes    int64                  `json:"cache_bytes"`
		CacheMaxBytes int64                  `json:"cache_max_bytes"`
	} `json:"transcode"`
	Loudness struct {
		Enabled bool `json:"enabled"`
		Pending int  `json:"pending"`
		Failed  int  `json:"failed"`
	} `json:"loudness"`
	Backups struct {
		Count int        `json:"count"`
		Last  *time.Time `json:"last,omitempty"`
	} `json:"backups"`
	Devices struct {
		Paired int `json:"paired"`
		Active int `json:"active_24h"`
	} `json:"devices"`
	Features    map[string]bool `json:"features"`
	Diagnostics []Diagnostic    `json:"diagnostics"`
}

func size(p string) int64 {
	if st, err := os.Stat(p); err == nil {
		return st.Size()
	}
	return 0
}

func (c *Collector) Collect(ctx context.Context) (Info, error) {
	var in Info
	in.Version, in.GoVersion, in.OS, in.Arch = c.Version, runtime.Version(), runtime.GOOS, runtime.GOARCH
	in.StartedAt, in.UptimeSec, in.SchemaVersion = c.Started.UTC(), int64(time.Since(c.Started).Seconds()), c.SchemaVersion
	in.Features = c.Features
	diag := func(level, code, f string, a ...any) {
		in.Diagnostics = append(in.Diagnostics, Diagnostic{level, code, fmt.Sprintf(f, a...)})
	}
	l := &in.Library
	q := func(dst any, sql string) {
		if err := c.DB.QueryRowContext(ctx, sql).Scan(dst); err != nil {
			diag("warn", "stats_failed", "statistics query failed")
		}
	}
	q(&l.Tracks, `SELECT count(*) FROM tracks`)
	q(&l.Albums, `SELECT count(*) FROM albums`)
	q(&l.Artists, `SELECT count(*) FROM artists`)
	q(&l.Files, `SELECT count(*) FROM source_tracks WHERE source='local'`)
	q(&l.Lossless, `SELECT count(*) FROM source_tracks WHERE source='local' AND lossless=1`)
	q(&l.Playlists, `SELECT count(*) FROM playlists`)
	q(&l.Plays, `SELECT count(*) FROM plays`)
	q(&l.DurationMs, `SELECT COALESCE(sum(duration_ms),0) FROM tracks`)
	q(&l.Bytes, `SELECT COALESCE(sum(size),0) FROM source_tracks WHERE source='local'`)
	q(&in.Devices.Paired, `SELECT count(*) FROM players WHERE token_hash IS NOT NULL`)
	c.DB.QueryRowContext(ctx, `SELECT count(*) FROM players WHERE token_hash IS NOT NULL AND last_seen_at>=?`,
		time.Now().Add(-24*time.Hour).UnixMilli()).Scan(&in.Devices.Active)
	if c.LatestScan != nil {
		if j, err := c.LatestScan(ctx); err == nil {
			in.Scan = &j
			if j.Status == "failed" {
				diag("error", "scan_failed", "last scan failed: %s", j.Error)
			} else if j.Failed > 0 {
				diag("warn", "scan_file_errors", "%d files could not be read in the last scan", j.Failed)
			}
		}
	}

	in.Roots = []RootStatus{}
	var roots []string
	if c.Roots != nil {
		roots = c.Roots()
	}
	if len(roots) == 0 {
		diag("error", "no_roots", "no library roots are configured")
	}
	for _, r := range roots {
		rs := RootStatus{Path: r}
		c.DB.QueryRowContext(ctx, `SELECT count(*) FROM source_tracks WHERE root=?`, r).Scan(&rs.Files)
		st, err := os.Stat(r)
		switch {
		case err != nil:
			rs.Error = "not accessible"
			if os.IsPermission(err) {
				rs.Error = "permission denied"
			}
			diag("error", "root_unavailable", "library root %s: %s", r, rs.Error)
		case !st.IsDir():
			rs.Error = "not a directory"
			diag("error", "root_unavailable", "library root %s is not a directory", r)
		default:
			if f, err := os.Open(r); err != nil {
				rs.Error = "cannot list directory"
				diag("error", "root_permission", "library root %s cannot be read (check permissions / UID)", r)
			} else {
				f.Close()
				rs.OK = true
			}
			if rs.OK && rs.Files == 0 {
				diag("info", "root_empty", "library root %s has no indexed audio files yet", r)
			}
		}
		rs.FreeBytes, _ = freeBytes(r)
		in.Roots = append(in.Roots, rs)
	}

	in.Database.SizeBytes, in.Database.WALBytes = size(c.DBPath), size(c.DBPath+"-wal")
	if free, ok := freeBytes(filepath.Dir(c.DBPath)); ok {
		in.Database.FreeBytes = free
		if free < 1<<30 {
			diag("warn", "low_disk", "less than 1 GiB free next to the database (%d MiB)", free>>20)
		}
	}
	if w := inotifyWatches(); w > 0 && w < 65536 && in.Library.Files > w/2 {
		diag("warn", "inotify_limit", "fs.inotify.max_user_watches=%d is low for this library; raise it or use YIMENG_WATCH=poll", w)
	}

	in.Transcode.Available = c.Transcoder != nil
	in.Transcode.Formats = c.Transcoder.Formats()
	in.Transcode.CacheBytes, in.Transcode.CacheMaxBytes = c.Transcoder.CacheBytes(), c.CacheMaxBytes
	if c.Transcoder == nil {
		diag("info", "no_ffmpeg", "ffmpeg not found: only original streams, no loudness analysis")
	}
	in.Loudness.Enabled = c.LoudnessEnabled && c.Transcoder != nil
	c.DB.QueryRowContext(ctx, `SELECT count(*) FROM source_tracks WHERE source='local' AND rg_track_gain IS NULL AND loudness_error IS NULL`).Scan(&in.Loudness.Pending)
	c.DB.QueryRowContext(ctx, `SELECT count(*) FROM source_tracks WHERE loudness_error IS NOT NULL`).Scan(&in.Loudness.Failed)

	if c.Backup != nil {
		if list, err := c.Backup.List(); err == nil {
			in.Backups.Count = len(list)
			if len(list) > 0 {
				t := list[0].CreatedAt
				in.Backups.Last = &t
				if time.Since(t) > 8*24*time.Hour {
					diag("warn", "backup_stale", "newest backup is older than 8 days")
				}
			}
		}
	}
	if in.Diagnostics == nil {
		in.Diagnostics = []Diagnostic{}
	}
	return in, nil
}
