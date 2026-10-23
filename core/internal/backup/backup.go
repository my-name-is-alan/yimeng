// Package backup creates consistent SQLite snapshots (VACUUM INTO) packed as
// zip archives, rotates scheduled backups, and stages restores that are
// applied at the next start (a live SQLite file is never swapped underneath
// open connections).
//
// Vault credentials live in the database (vault_entries, AES-256-GCM), so a
// snapshot contains them encrypted. The master key is NOT included unless
// explicitly requested, so a leaked backup alone does not reveal secrets.
package backup

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	dbEntry       = "library.db"
	keyEntry      = "master.key"
	manifestEntry = "manifest.json"
	Ext           = ".ymbak"
)

var (
	ErrNotFound = errors.New("backup not found")
	ErrInvalid  = errors.New("invalid backup archive")
	nameRe      = regexp.MustCompile(`^yimeng-[0-9]{8}-[0-9]{6}(-[a-z0-9-]{1,32})?\.ymbak$`)
)

type Manifest struct {
	Format        int       `json:"format"`
	CreatedAt     time.Time `json:"created_at"`
	Reason        string    `json:"reason"`
	SchemaVersion int       `json:"schema_version"`
	CoreVersion   string    `json:"core_version"`
	IncludesKey   bool      `json:"includes_key"`
}

type Info struct {
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
	Reason    string    `json:"reason,omitempty"`
}

type Service struct {
	DB          *sql.DB
	DBPath      string
	Dir         string // backups directory
	KeyFile     string // vault master key (only packed on request)
	Keep        int    // scheduled/auto backups retained; <=0 = 7
	CoreVersion string
	Log         *slog.Logger
	mu          sync.Mutex
}

func (s *Service) log() *slog.Logger {
	if s.Log == nil {
		return slog.Default()
	}
	return s.Log
}

func schemaVersion(ctx context.Context, db *sql.DB) int {
	var v sql.NullInt64
	db.QueryRowContext(ctx, `SELECT max(version) FROM schema_migrations`).Scan(&v)
	return int(v.Int64)
}

// Snapshot writes a consistent zip archive of the database to w.
func (s *Service) Snapshot(ctx context.Context, w io.Writer, reason string, includeKey bool) (Manifest, error) {
	tmp, err := os.MkdirTemp(filepath.Dir(s.DBPath), ".snap-")
	if err != nil {
		return Manifest{}, err
	}
	defer os.RemoveAll(tmp)
	snap := filepath.Join(tmp, dbEntry)
	// VACUUM INTO produces a transactionally consistent, compacted copy.
	if _, err := s.DB.ExecContext(ctx, `VACUUM INTO ?`, snap); err != nil {
		return Manifest{}, fmt.Errorf("vacuum into: %w", err)
	}
	m := Manifest{Format: 1, CreatedAt: time.Now().UTC(), Reason: reason, SchemaVersion: schemaVersion(ctx, s.DB),
		CoreVersion: s.CoreVersion}
	zw := zip.NewWriter(w)
	add := func(name, path string) error {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		hw, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: m.CreatedAt})
		if err != nil {
			return err
		}
		_, err = io.Copy(hw, f)
		return err
	}
	if err := add(dbEntry, snap); err != nil {
		return m, err
	}
	if includeKey && s.KeyFile != "" {
		if _, err := os.Stat(s.KeyFile); err == nil {
			if err := add(keyEntry, s.KeyFile); err != nil {
				return m, err
			}
			m.IncludesKey = true
		}
	}
	mw, err := zw.Create(manifestEntry)
	if err != nil {
		return m, err
	}
	if err := json.NewEncoder(mw).Encode(m); err != nil {
		return m, err
	}
	return m, zw.Close()
}

func sanitizeReason(r string) string {
	r = strings.ToLower(r)
	var b strings.Builder
	for _, c := range r {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			b.WriteRune(c)
		}
	}
	out := b.String()
	if len(out) > 32 {
		out = out[:32]
	}
	return out
}

// Create stores a snapshot in Dir (never includes the master key) and rotates.
func (s *Service) Create(ctx context.Context, reason string) (Info, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return Info{}, err
	}
	name := "yimeng-" + time.Now().UTC().Format("20060102-150405")
	if r := sanitizeReason(reason); r != "" {
		name += "-" + r
	}
	name += Ext
	final := filepath.Join(s.Dir, name)
	f, err := os.CreateTemp(s.Dir, ".tmp-*")
	if err != nil {
		return Info{}, err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return Info{}, err
	}
	if _, err := s.Snapshot(ctx, f, reason, false); err != nil {
		f.Close()
		return Info{}, err
	}
	if err := f.Close(); err != nil {
		return Info{}, err
	}
	if err := os.Rename(f.Name(), final); err != nil {
		return Info{}, err
	}
	s.rotate()
	st, _ := os.Stat(final)
	s.log().Info("backup created", "name", name, "size", st.Size())
	return Info{Name: name, Size: st.Size(), CreatedAt: st.ModTime().UTC(), Reason: sanitizeReason(reason)}, nil
}

func (s *Service) List() ([]Info, error) {
	ents, err := os.ReadDir(s.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Info{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Info{}
	for _, e := range ents {
		if !nameRe.MatchString(e.Name()) {
			continue
		}
		st, err := e.Info()
		if err != nil {
			continue
		}
		in := Info{Name: e.Name(), Size: st.Size(), CreatedAt: st.ModTime().UTC()}
		parts := strings.SplitN(strings.TrimSuffix(e.Name(), Ext), "-", 4)
		if len(parts) == 4 {
			in.Reason = parts[3]
		}
		out = append(out, in)
	}
	slices.SortFunc(out, func(a, b Info) int { return strings.Compare(b.Name, a.Name) })
	return out, nil
}

// Path resolves a backup name strictly (no traversal: names must match nameRe).
func (s *Service) Path(name string) (string, error) {
	if !nameRe.MatchString(name) {
		return "", ErrNotFound
	}
	p := filepath.Join(s.Dir, name)
	if st, err := os.Stat(p); err != nil || !st.Mode().IsRegular() {
		return "", ErrNotFound
	}
	return p, nil
}

func (s *Service) Delete(name string) error {
	p, err := s.Path(name)
	if err != nil {
		return err
	}
	return os.Remove(p)
}

func (s *Service) rotate() {
	keep := s.Keep
	if keep <= 0 {
		keep = 7
	}
	list, _ := s.List()
	for i, b := range list {
		if i >= keep {
			os.Remove(filepath.Join(s.Dir, b.Name))
		}
	}
}

// Run creates a backup every interval until ctx ends (interval<=0: disabled).
func (s *Service) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := s.Create(ctx, "scheduled"); err != nil {
				s.log().Warn("scheduled backup", "err", err)
			}
		}
	}
}

// PendingPath is where a validated restore waits for the next start.
func PendingPath(dbPath string) string { return dbPath + ".restore-pending" }

// Stage validates an uploaded archive (read from r, at most maxBytes) and
// stages its database for the next start. maxSchema is the newest schema this
// binary understands. It returns the archive manifest.
func (s *Service) Stage(ctx context.Context, r io.Reader, maxBytes int64, maxSchema int) (Manifest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Dir(s.DBPath)
	up, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return Manifest{}, err
	}
	defer os.Remove(up.Name())
	defer up.Close()
	n, err := io.Copy(up, io.LimitReader(r, maxBytes+1))
	if err != nil {
		return Manifest{}, err
	}
	if n > maxBytes {
		return Manifest{}, fmt.Errorf("%w: larger than %d bytes", ErrInvalid, maxBytes)
	}
	zr, err := zip.NewReader(up, n)
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	var m Manifest
	var dbFile *zip.File
	for _, f := range zr.File {
		switch f.Name { // only known names are read; nothing is extracted by archive path
		case manifestEntry:
			rc, err := f.Open()
			if err != nil {
				return m, fmt.Errorf("%w: %v", ErrInvalid, err)
			}
			err = json.NewDecoder(io.LimitReader(rc, 64<<10)).Decode(&m)
			rc.Close()
			if err != nil {
				return m, fmt.Errorf("%w: manifest: %v", ErrInvalid, err)
			}
		case dbEntry:
			dbFile = f
		}
	}
	if dbFile == nil || m.Format != 1 {
		return m, fmt.Errorf("%w: missing database or manifest", ErrInvalid)
	}
	if m.SchemaVersion > maxSchema {
		return m, fmt.Errorf("%w: schema %d is newer than this Core (%d)", ErrInvalid, m.SchemaVersion, maxSchema)
	}
	tmp, err := os.CreateTemp(dir, ".restore-*")
	if err != nil {
		return m, err
	}
	defer os.Remove(tmp.Name())
	rc, err := dbFile.Open()
	if err != nil {
		tmp.Close()
		return m, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	_, err = io.Copy(tmp, io.LimitReader(rc, maxBytes*8)) // bound decompression
	rc.Close()
	tmp.Close()
	if err != nil {
		return m, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if err := checkDB(ctx, tmp.Name()); err != nil {
		return m, err
	}
	if err := os.Rename(tmp.Name(), PendingPath(s.DBPath)); err != nil {
		return m, err
	}
	os.Chmod(PendingPath(s.DBPath), 0o600)
	s.log().Warn("restore staged; it is applied at the next start", "schema", m.SchemaVersion, "created_at", m.CreatedAt)
	return m, nil
}

func checkDB(ctx context.Context, path string) error {
	d, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	defer d.Close()
	var res string
	if err := d.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&res); err != nil || res != "ok" {
		return fmt.Errorf("%w: integrity check failed", ErrInvalid)
	}
	var n int
	if err := d.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil || n == 0 {
		return fmt.Errorf("%w: not a yimeng database", ErrInvalid)
	}
	return nil
}

// ApplyPending swaps in a staged restore before the database is opened. The
// current database is preserved as <db>.pre-restore-<ts>. Returns true if applied.
func ApplyPending(dbPath string) (bool, error) {
	p := PendingPath(dbPath)
	if _, err := os.Stat(p); err != nil {
		return false, nil
	}
	if _, err := os.Stat(dbPath); err == nil {
		keep := dbPath + ".pre-restore-" + time.Now().UTC().Format("20060102-150405")
		if err := os.Rename(dbPath, keep); err != nil {
			return false, err
		}
	}
	for _, sfx := range []string{"-wal", "-shm"} {
		os.Remove(dbPath + sfx)
	}
	return true, os.Rename(p, dbPath)
}
