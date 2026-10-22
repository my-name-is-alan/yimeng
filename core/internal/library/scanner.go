// Package library owns the local scan: it walks configured roots, reads files
// through the local adapter, and upserts rows into source_tracks.
// Merge into canonical tables is P0 step 4 (not implemented here).
package library

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/adapters/local"
)

const SourceLocal = "local"

var (
	ErrScanRunning = errors.New("scan already running")
	ErrUnknownRoot = errors.New("root is not a configured library root")
	ErrNotFound    = errors.New("scan job not found")
)

type Job struct {
	ID         string     `json:"id"`
	Roots      []string   `json:"roots"`
	Status     string     `json:"status"` // queued | running | completed | failed
	Progress   float64    `json:"progress"`
	Total      int        `json:"total"`
	Processed  int        `json:"processed"`
	Added      int        `json:"added"`
	Updated    int        `json:"updated"`
	Unchanged  int        `json:"unchanged"`
	Removed    int        `json:"removed"`
	Failed     int        `json:"failed"`
	Error      string     `json:"error,omitempty"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

type Options struct {
	Roots    []string // configured library roots (absolute)
	CoverDir string   // where embedded cover art is extracted
	Logger   *slog.Logger
	// OnProgress, if set, receives the job after every progress flush and at
	// completion (feeds WS scan.progress).
	OnProgress func(Job)
	// OnMerged, if set, runs after a successful scan + merge (e.g. enrichment).
	OnMerged func()
}

type Scanner struct {
	db          *sql.DB
	extResolver ExternalResolver
	opts        Options
	log         *slog.Logger

	mu      sync.Mutex
	running string // job id
	mergeMu sync.Mutex
	split   atomic.Pointer[Splitter]
	wg      sync.WaitGroup
}

func NewScanner(db *sql.DB, opts Options) *Scanner {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Scanner{db: db, opts: opts, log: opts.Logger}
}

// RecoverInterrupted marks jobs left running by a previous process as failed.
func (s *Scanner) RecoverInterrupted(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE scan_jobs SET status='failed', error='interrupted (core restarted)', finished_at=?
		WHERE status IN ('queued','running')`, time.Now().UnixMilli())
	return err
}

type StartRequest struct {
	Root string // optional: limit to one configured root (or a subdir of one)
	Full bool   // ignore mtime/size and re-read every file
}

// Start creates a scan job and runs it in the background. ctx bounds the
// background work (pass the server lifetime context, not a request context).
func (s *Scanner) Start(ctx context.Context, req StartRequest) (Job, error) {
	roots := s.opts.Roots
	if req.Root != "" {
		r := filepath.Clean(req.Root)
		if !s.underRoot(r) {
			return Job{}, ErrUnknownRoot
		}
		roots = []string{r}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running != "" {
		j, _ := s.Get(ctx, s.running)
		return j, ErrScanRunning
	}
	id := newID()
	now := time.Now()
	rootsJSON, _ := json.Marshal(roots)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO scan_jobs(id, root, status, started_at) VALUES (?,?, 'running', ?)`,
		id, string(rootsJSON), now.UnixMilli()); err != nil {
		return Job{}, err
	}
	s.running = id
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.run(context.WithoutCancel(ctx), ctx, id, roots, req.Full)
		s.mu.Lock()
		s.running = ""
		s.mu.Unlock()
	}()
	return s.Get(ctx, id)
}

// Wait blocks until the current background scan (if any) finishes.
func (s *Scanner) Wait() { s.wg.Wait() }

func (s *Scanner) underRoot(p string) bool {
	for _, r := range s.opts.Roots {
		if p == r || strings.HasPrefix(p, r+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// rootOf returns the configured root containing p.
func (s *Scanner) rootOf(p string) string {
	best := ""
	for _, r := range s.opts.Roots {
		if (p == r || strings.HasPrefix(p, r+string(filepath.Separator))) && len(r) > len(best) {
			best = r
		}
	}
	return best
}

type counters struct{ total, processed, added, updated, unchanged, removed, failed int }

func (s *Scanner) run(dbctx, ctx context.Context, id string, roots []string, full bool) {
	var c counters
	var lastErr string
	fail := func(err error) {
		s.log.Error("scan failed", "job", id, "err", err)
		s.finish(dbctx, id, &c, "failed", err.Error())
	}

	// Pass 1: collect candidate files (cheap) so progress has a denominator.
	var files []string
	for _, root := range roots {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if p == root {
					return err
				}
				c.failed++
				lastErr = err.Error()
				return nil // unreadable subdir: skip, keep going
			}
			name := d.Name()
			if d.IsDir() {
				if p != root && skipDirName(name) {
					return filepath.SkipDir // hidden + Synology/fnOS/QNAP system dirs
				}
				return nil
			}
			if strings.HasPrefix(name, ".") || !d.Type().IsRegular() || !local.Supported(p) {
				return nil
			}
			files = append(files, p)
			return ctx.Err()
		})
		if err != nil {
			fail(fmt.Errorf("walk %s: %w", root, err))
			return
		}
	}
	c.total = len(files)
	s.progress(dbctx, id, &c, lastErr)

	lastFlush := time.Now()
	folderCovers := map[string]string{}
	for _, p := range files {
		if err := ctx.Err(); err != nil {
			fail(err)
			return
		}
		res, err := s.scanFile(dbctx, id, p, full, folderCovers)
		c.processed++
		switch {
		case err != nil:
			c.failed++
			lastErr = fmt.Sprintf("%s: %v", p, err)
			s.log.Warn("scan file", "path", p, "err", err)
		case res == "added":
			c.added++
		case res == "updated":
			c.updated++
		default:
			c.unchanged++
		}
		if time.Since(lastFlush) > 500*time.Millisecond {
			s.progress(dbctx, id, &c, lastErr)
			lastFlush = time.Now()
		}
	}

	// Remove rows for files that disappeared under the scanned roots.
	for _, root := range roots {
		res, err := s.db.ExecContext(dbctx, `DELETE FROM source_tracks WHERE source=? AND (path=? OR path LIKE ? ESCAPE '\') AND last_scan_id<>?`,
			SourceLocal, root, likePrefix(root+string(filepath.Separator)), id)
		if err != nil {
			fail(err)
			return
		}
		n, _ := res.RowsAffected()
		c.removed += int(n)
	}
	if err := s.Merge(dbctx); err != nil {
		fail(fmt.Errorf("merge: %w", err))
		return
	}
	s.finish(dbctx, id, &c, "completed", lastErr)
	s.log.Info("scan completed", "job", id, "total", c.total, "added", c.added, "updated", c.updated,
		"unchanged", c.unchanged, "removed", c.removed, "failed", c.failed)
}

func likePrefix(p string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(p) + "%"
}

func (s *Scanner) scanFile(ctx context.Context, jobID, path string, full bool, folderCovers map[string]string) (string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	size, mtime := st.Size(), st.ModTime().UnixNano()

	var rowID string
	var oldSize, oldMtime sql.NullInt64
	var probed bool
	err = s.db.QueryRowContext(ctx, `SELECT id, size, mtime, codec IS NOT NULL FROM source_tracks WHERE source=? AND source_track_id=?`,
		SourceLocal, path).Scan(&rowID, &oldSize, &oldMtime, &probed)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	now := time.Now().UnixMilli()
	if exists && !full && probed && oldSize.Int64 == size && oldMtime.Int64 == mtime {
		_, err := s.db.ExecContext(ctx, `UPDATE source_tracks SET last_scan_id=? WHERE id=?`, jobID, rowID)
		return "unchanged", err
	}

	t, err := local.ReadFile(path)
	if err != nil {
		return "", err
	}
	hash, err := quickHash(path, size)
	if err != nil {
		return "", err
	}
	coverPath, coverSrc := "", ""
	if t.Picture != nil && s.opts.CoverDir != "" {
		coverPath, err = s.saveCover(t.Picture)
		if err != nil {
			s.log.Warn("save cover", "path", path, "err", err)
			coverPath = ""
		} else {
			coverSrc = "embedded"
		}
	}
	if coverPath == "" {
		dir := filepath.Dir(path)
		fc, ok := folderCovers[dir]
		if !ok {
			fc = findFolderCover(dir)
			folderCovers[dir] = fc
		}
		if fc != "" {
			coverPath, coverSrc = fc, "folder"
		}
	}
	raw, _ := json.Marshal(t)

	if !exists {
		rowID = newID()
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO source_tracks(
		id, source, source_track_id, raw_json, path, root, size, mtime, hash, format,
		title, artist, album, album_artist, track_no, track_total, disc_no, disc_total, year, genre,
		duration_ms, isrc, mb_recording_id, mb_release_id, cover_path, cover_source, last_scan_id, created_at, updated_at,
		codec, sample_rate, bit_depth, channels, bitrate_kbps, lossless, lyrics,
		rg_track_gain, rg_track_peak, rg_album_gain, rg_album_peak, rg_source, loudness_lufs, loudness_error)
		VALUES (?,?,?,?,?,?,?,?,?,?, ?,?,?,?,?,?,?,?,?,?, ?,?,?,?,?,?,?,?,?, ?,?,?,?,?,?,?, ?,?,?,?,?,NULL,NULL)
		ON CONFLICT(source, source_track_id) DO UPDATE SET
		raw_json=excluded.raw_json, root=excluded.root, size=excluded.size, mtime=excluded.mtime, hash=excluded.hash,
		format=excluded.format, title=excluded.title, artist=excluded.artist, album=excluded.album,
		album_artist=excluded.album_artist, track_no=excluded.track_no, track_total=excluded.track_total,
		disc_no=excluded.disc_no, disc_total=excluded.disc_total, year=excluded.year, genre=excluded.genre,
		duration_ms=excluded.duration_ms, isrc=excluded.isrc, mb_recording_id=excluded.mb_recording_id,
		mb_release_id=excluded.mb_release_id, cover_path=excluded.cover_path, cover_source=excluded.cover_source,
		last_scan_id=excluded.last_scan_id, updated_at=excluded.updated_at, codec=excluded.codec,
		sample_rate=excluded.sample_rate, bit_depth=excluded.bit_depth, channels=excluded.channels,
		bitrate_kbps=excluded.bitrate_kbps, lossless=excluded.lossless, lyrics=excluded.lyrics,
		rg_track_gain=excluded.rg_track_gain, rg_track_peak=excluded.rg_track_peak, rg_album_gain=excluded.rg_album_gain,
		rg_album_peak=excluded.rg_album_peak, rg_source=excluded.rg_source, loudness_lufs=NULL, loudness_error=NULL`,
		rowID, SourceLocal, path, string(raw), path, s.rootOf(path), size, mtime, hash, t.Format,
		t.Title, nz(t.Artist), nz(t.Album), nz(t.AlbumArtist), ni(t.TrackNo), ni(t.TrackTotal), ni(t.DiscNo), ni(t.DiscTotal), ni(t.Year), nz(t.Genre),
		ni64(t.DurationMs), nz(t.ISRC), nz(t.MBRecordingID), nz(t.MBReleaseID), nz(coverPath), nz(coverSrc), jobID, now, now,
		codecOf(t), ni(t.Audio.SampleRate), ni(t.Audio.BitDepth), ni(t.Audio.Channels), ni(t.Audio.BitrateKbps), t.Audio.Lossless, nz(t.Lyrics),
		t.Gain.TrackGain, t.Gain.TrackPeak, t.Gain.AlbumGain, t.Gain.AlbumPeak, rgSource(t.Gain))
	if err != nil {
		return "", err
	}
	if exists {
		return "updated", nil
	}
	return "added", nil
}

func nz(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func ni(n int) any {
	if n == 0 {
		return nil
	}
	return n
}
func ni64(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}

// quickHash fingerprints a file cheaply (size + first 64 KiB); full-content
// hashing of a NAS library would be too slow for P0.
func quickHash(path string, size int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha1.New()
	fmt.Fprintf(h, "%d:", size)
	if _, err := io.CopyN(h, f, 64*1024); err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// saveCover writes embedded art content-addressed under CoverDir/ab/<sha1>.<ext>.
func (s *Scanner) saveCover(p *local.Picture) (string, error) {
	sum := sha1.Sum(p.Data)
	name := hex.EncodeToString(sum[:])
	dir := filepath.Join(s.opts.CoverDir, name[:2])
	dst := filepath.Join(dir, name+"."+p.Ext)
	if _, err := os.Stat(dst); err == nil {
		return dst, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".cover-*")
	if err != nil {
		return "", err
	}
	if _, err := tmp.Write(p.Data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	tmp.Close()
	return dst, os.Rename(tmp.Name(), dst)
}

var folderCoverNames = []string{"cover", "folder", "front", "album", "albumart"}

func findFolderCover(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	byName := map[string]string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := strings.ToLower(e.Name())
		ext := filepath.Ext(n)
		if ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".webp" {
			byName[strings.TrimSuffix(n, ext)] = filepath.Join(dir, e.Name())
		}
	}
	for _, n := range folderCoverNames {
		if p, ok := byName[n]; ok {
			return p
		}
	}
	return ""
}

func (s *Scanner) progress(ctx context.Context, id string, c *counters, lastErr string) {
	prog := 0.0
	if c.total > 0 {
		prog = float64(c.processed) / float64(c.total)
	}
	_, err := s.db.ExecContext(ctx, `UPDATE scan_jobs SET progress=?, total=?, processed=?, added=?, updated=?, unchanged=?, removed=?, failed=?, error=? WHERE id=?`,
		prog, c.total, c.processed, c.added, c.updated, c.unchanged, c.removed, c.failed, nz(lastErr), id)
	if err != nil {
		s.log.Warn("scan progress", "err", err)
	}
	s.notify(ctx, id)
}

func (s *Scanner) notify(ctx context.Context, id string) {
	if s.opts.OnProgress != nil {
		if j, err := s.Get(ctx, id); err == nil {
			s.opts.OnProgress(j)
		}
	}
}

func (s *Scanner) finish(ctx context.Context, id string, c *counters, status, errMsg string) {
	s.progress(ctx, id, c, errMsg)
	prog := 1.0
	if status != "completed" && c.total > 0 {
		prog = float64(c.processed) / float64(c.total)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE scan_jobs SET status=?, progress=?, finished_at=? WHERE id=?`,
		status, prog, time.Now().UnixMilli(), id); err != nil {
		s.log.Error("scan finish", "err", err)
	}
	s.notify(ctx, id)
	if status == "completed" && s.opts.OnMerged != nil {
		s.opts.OnMerged()
	}
}

const jobCols = `id, root, status, progress, total, processed, added, updated, unchanged, removed, failed, error, started_at, finished_at`

func scanJob(row interface{ Scan(...any) error }) (Job, error) {
	var j Job
	var roots string
	var errMsg sql.NullString
	var started, finished sql.NullInt64
	if err := row.Scan(&j.ID, &roots, &j.Status, &j.Progress, &j.Total, &j.Processed, &j.Added, &j.Updated,
		&j.Unchanged, &j.Removed, &j.Failed, &errMsg, &started, &finished); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return j, ErrNotFound
		}
		return j, err
	}
	_ = json.Unmarshal([]byte(roots), &j.Roots)
	j.Error = errMsg.String
	if started.Valid {
		t := time.UnixMilli(started.Int64).UTC()
		j.StartedAt = &t
	}
	if finished.Valid {
		t := time.UnixMilli(finished.Int64).UTC()
		j.FinishedAt = &t
	}
	return j, nil
}

func (s *Scanner) Get(ctx context.Context, id string) (Job, error) {
	return scanJob(s.db.QueryRowContext(ctx, `SELECT `+jobCols+` FROM scan_jobs WHERE id=?`, id))
}

func (s *Scanner) Latest(ctx context.Context) (Job, error) {
	return scanJob(s.db.QueryRowContext(ctx, `SELECT `+jobCols+` FROM scan_jobs ORDER BY started_at DESC, rowid DESC LIMIT 1`))
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func statDir(p string) (bool, error) {
	st, err := os.Stat(p)
	if err != nil {
		return false, err
	}
	return st.IsDir(), nil
}

// codecOf never returns NULL so a probed row is not re-read on every scan.
func codecOf(t local.Track) string {
	if t.Audio.Codec != "" {
		return t.Audio.Codec
	}
	return "unknown"
}

func rgSource(g local.Gain) any {
	if g.Empty() {
		return nil
	}
	return "tag"
}

// Roots returns a copy of the configured library roots.
func (s *Scanner) Roots() []string {
	out := make([]string, len(s.opts.Roots))
	copy(out, s.opts.Roots)
	return out
}
