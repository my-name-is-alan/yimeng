package transcode

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var ErrUnavailable = errors.New("transcoding unavailable")

type Options struct {
	FFmpeg      string // path or name; "" = look up "ffmpeg" in PATH; "off" disables
	CacheDir    string // transcoded files live in <CacheDir>/transcode
	MaxBytes    int64  // cache size limit (LRU); <=0 = 1 GiB
	Concurrency int    // concurrent ffmpeg processes; <=0 = max(1, NumCPU/2)
	Log         *slog.Logger
}

// Transcoder is safe for concurrent use. A nil *Transcoder is valid and
// reports no formats (original only).
type Transcoder struct {
	ffmpeg   string
	dir      string
	max      atomic.Int64
	sem      chan struct{}
	log      *slog.Logger
	encoders map[string]bool

	mu   sync.Mutex
	jobs map[string]*job
}

// New detects ffmpeg and its encoders. It returns (nil, nil) when ffmpeg is
// disabled or not found, so callers can treat transcoding as optional.
func New(o Options) (*Transcoder, error) {
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if strings.EqualFold(o.FFmpeg, "off") || strings.EqualFold(o.FFmpeg, "disabled") {
		return nil, nil
	}
	name := o.FFmpeg
	if name == "" {
		name = "ffmpeg"
	}
	bin, err := exec.LookPath(name)
	if err != nil {
		if o.FFmpeg != "" {
			return nil, fmt.Errorf("ffmpeg %q: %w", o.FFmpeg, err)
		}
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "-hide_banner", "-encoders").Output()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg -encoders: %w", err)
	}
	t := &Transcoder{ffmpeg: bin, dir: filepath.Join(o.CacheDir, "transcode"), log: o.Log,
		encoders: map[string]bool{}, jobs: map[string]*job{}}
	for _, line := range strings.Split(string(out), "\n") {
		if f := strings.Fields(line); len(f) >= 2 && len(f[0]) == 6 && f[0][0] == 'A' {
			t.encoders[f[1]] = true
		}
	}
	t.max.Store(o.MaxBytes)
	if o.MaxBytes <= 0 {
		t.max.Store(1 << 30)
	}
	n := o.Concurrency
	if n <= 0 {
		n = max(1, runtime.NumCPU()/2)
	}
	t.sem = make(chan struct{}, n)
	if err := os.MkdirAll(filepath.Join(t.dir, "tmp"), 0o750); err != nil {
		return nil, err
	}
	// leftovers from a crash are never valid
	if ents, err := os.ReadDir(filepath.Join(t.dir, "tmp")); err == nil {
		for _, e := range ents {
			os.Remove(filepath.Join(t.dir, "tmp", e.Name()))
		}
	}
	t.evict()
	return t, nil
}

// Supports reports whether the profile can be produced.
func (t *Transcoder) Supports(p Profile) bool {
	if p.IsOriginal() {
		return true
	}
	return t != nil && t.encoders[specs[p.Format].encoder]
}

// Formats lists what clients may request (always includes original).
func (t *Transcoder) Formats() []FormatInfo {
	out := []FormatInfo{{Format: Original}}
	for _, f := range []string{"flac", "aac", "mp3", "opus"} {
		if p := (Profile{Format: f}); t.Supports(p) {
			sp := specs[f]
			out = append(out, FormatInfo{Format: f, Lossless: len(sp.bitrates) == 0, Bitrates: sp.bitrates, Default: sp.def})
		}
	}
	return out
}

// Source identifies the input file; Size/Mtime invalidate the cache when the file changes.
type Source struct {
	TrackID string
	Path    string
	Size    int64
	Mtime   int64
}

// Key is the cache key for (source, profile) and doubles as a strong ETag.
func Key(s Source, p Profile) string {
	h := sha256.Sum256([]byte(strings.Join([]string{s.TrackID, s.Path,
		strconv.FormatInt(s.Size, 10), strconv.FormatInt(s.Mtime, 10), p.Key()}, "\x00")))
	return hex.EncodeToString(h[:16]) + "-" + p.Key()
}

func (t *Transcoder) finalPath(key string, p Profile) string {
	return filepath.Join(t.dir, key[:2], key+"."+specs[p.Format].ext)
}

// Cached returns the path of a complete cached transcode, touching it for LRU.
func (t *Transcoder) Cached(s Source, p Profile) (string, bool) {
	if t == nil {
		return "", false
	}
	path := t.finalPath(Key(s, p), p)
	if st, err := os.Stat(path); err == nil && st.Mode().IsRegular() {
		now := time.Now()
		os.Chtimes(path, now, now)
		return path, true
	}
	return "", false
}

type job struct {
	part    string
	cancel  context.CancelFunc
	readers int
	done    chan struct{}
	err     error
	notify  chan struct{} // closed & replaced whenever new bytes may be available
}

// Open starts (or joins) a transcode and returns a reader that follows the
// output as ffmpeg writes it. The reader returns io.EOF only once ffmpeg has
// finished successfully; the result is then moved into the cache. Waiting for
// a transcode slot honours ctx.
func (t *Transcoder) Open(ctx context.Context, s Source, p Profile) (io.ReadCloser, error) {
	if !t.Supports(p) || p.IsOriginal() {
		return nil, ErrUnavailable
	}
	key := Key(s, p)
	t.mu.Lock()
	j := t.jobs[key]
	if j == nil {
		t.mu.Unlock()
		select { // concurrency limit
		case t.sem <- struct{}{}:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		t.mu.Lock()
		if j = t.jobs[key]; j != nil { // someone else started it meanwhile
			<-t.sem
		} else {
			var err error
			if j, err = t.start(key, s, p); err != nil {
				t.mu.Unlock()
				<-t.sem
				return nil, err
			}
		}
	}
	j.readers++
	t.mu.Unlock()
	f, err := os.Open(j.part)
	if errors.Is(err, fs.ErrNotExist) { // finished and moved between lookup and open
		f, err = os.Open(t.finalPath(key, p))
	}
	if err != nil {
		t.release(key, j)
		return nil, err
	}
	return &tail{t: t, key: key, j: j, f: f}, nil
}

// start must hold t.mu.
func (t *Transcoder) start(key string, s Source, p Profile) (*job, error) {
	part := filepath.Join(t.dir, "tmp", key+".part")
	if err := os.WriteFile(part, nil, 0o640); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	j := &job{part: part, cancel: cancel, done: make(chan struct{}), notify: make(chan struct{})}
	t.jobs[key] = j
	cmd := exec.CommandContext(ctx, t.ffmpeg, p.args(s.Path, part)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		delete(t.jobs, key)
		cancel()
		os.Remove(part)
		return nil, err
	}
	go t.pulse(j)
	go func() {
		defer func() { <-t.sem }()
		err := cmd.Wait()
		if err == nil {
			final := t.finalPath(key, p)
			if err = os.MkdirAll(filepath.Dir(final), 0o750); err == nil {
				err = os.Rename(part, final) // open readers keep their fd
			}
		} else {
			msg := strings.TrimSpace(stderr.String())
			if len(msg) > 300 {
				msg = msg[:300]
			}
			if ctx.Err() == nil {
				t.log.Warn("transcode failed", "track", s.TrackID, "profile", p.Key(), "err", err, "stderr", msg)
			}
			err = fmt.Errorf("transcode %s: %w", p.Key(), err)
			os.Remove(part)
		}
		t.mu.Lock()
		j.err = err
		delete(t.jobs, key)
		close(j.done)
		t.mu.Unlock()
		cancel()
		if err == nil {
			t.evict()
		}
	}()
	return j, nil
}

// pulse wakes tailing readers periodically while ffmpeg writes.
func (t *Transcoder) pulse(j *job) {
	tk := time.NewTicker(25 * time.Millisecond)
	defer tk.Stop()
	for {
		select {
		case <-j.done:
			t.mu.Lock()
			close(j.notify)
			t.mu.Unlock()
			return
		case <-tk.C:
			t.mu.Lock()
			close(j.notify)
			j.notify = make(chan struct{})
			t.mu.Unlock()
		}
	}
}

// release drops a reader; the last reader of an unfinished job cancels it
// (the client went away, e.g. skipped the track).
func (t *Transcoder) release(key string, j *job) {
	t.mu.Lock()
	defer t.mu.Unlock()
	j.readers--
	if j.readers == 0 {
		select {
		case <-j.done:
		default:
			j.cancel()
		}
	}
}

type tail struct {
	t      *Transcoder
	key    string
	j      *job
	f      *os.File
	closed bool
}

func (r *tail) Read(p []byte) (int, error) {
	for {
		n, err := r.f.Read(p)
		if n > 0 || (err != nil && err != io.EOF) {
			return n, err
		}
		r.t.mu.Lock()
		wait := r.j.notify
		r.t.mu.Unlock()
		select {
		case <-r.j.done:
			if r.j.err != nil {
				return 0, r.j.err
			}
			// drain whatever was written after our last read
			if n, err := r.f.Read(p); n > 0 || err != io.EOF {
				return n, err
			}
			return 0, io.EOF
		default:
		}
		<-wait
	}
}

func (r *tail) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	r.t.release(r.key, r.j)
	return r.f.Close()
}

// evict removes least-recently-used cached files until the cache fits.
func (t *Transcoder) evict() {
	type ent struct {
		path string
		size int64
		mt   time.Time
	}
	var ents []ent
	var total int64
	filepath.WalkDir(t.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == "tmp" {
				return filepath.SkipDir
			}
			return nil
		}
		if info, err := d.Info(); err == nil && info.Mode().IsRegular() {
			ents = append(ents, ent{p, info.Size(), info.ModTime()})
			total += info.Size()
		}
		return nil
	})
	limit := t.max.Load()
	if total <= limit {
		return
	}
	slices.SortFunc(ents, func(a, b ent) int { return a.mt.Compare(b.mt) })
	for _, e := range ents {
		if total <= limit {
			break
		}
		if os.Remove(e.path) == nil {
			total -= e.size
			t.log.Debug("transcode cache evict", "file", filepath.Base(e.path), "size", e.size)
		}
	}
}

// FFmpeg returns the resolved ffmpeg binary ("" when unavailable).
func (t *Transcoder) FFmpeg() string {
	if t == nil {
		return ""
	}
	return t.ffmpeg
}
