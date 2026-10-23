// Package loudness fills in ReplayGain for files without gain tags by running
// ffmpeg's EBU R128 (ebur128) filter in the background, one file at a time.
package loudness

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os/exec"
	"regexp"
	"strconv"
	"time"
)

const Reference = -18.0 // ReplayGain 2 reference loudness (LUFS)

type Analyzer struct {
	DB     *sql.DB
	FFmpeg string
	Log    *slog.Logger
	// Resolve maps a stored path to a safe readable path (library-root check).
	Resolve func(path string) (string, error)
	// OnDone is called after each analyzed batch (e.g. to publish an event).
	OnDone  func(n int)
	Timeout time.Duration // per file; default 10m
	kick    chan struct{}
}

func (a *Analyzer) init() {
	if a.kick == nil {
		a.kick = make(chan struct{}, 1)
	}
	if a.Log == nil {
		a.Log = slog.Default()
	}
	if a.Timeout <= 0 {
		a.Timeout = 10 * time.Minute
	}
}

// Kick schedules a pass (non-blocking). Call before Run or concurrently.
func (a *Analyzer) Kick() {
	select {
	case a.kick <- struct{}{}:
	default:
	}
}

func New(db *sql.DB, ffmpeg string, log *slog.Logger) *Analyzer {
	a := &Analyzer{DB: db, FFmpeg: ffmpeg, Log: log}
	a.init()
	return a
}

// Run processes pending files whenever kicked until ctx ends.
func (a *Analyzer) Run(ctx context.Context) {
	a.init()
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.kick:
		}
		n, err := a.Pass(ctx)
		if err != nil && ctx.Err() == nil {
			a.Log.Warn("loudness analysis", "err", err)
		}
		if n > 0 {
			a.Log.Info("loudness analysis done", "files", n)
			if a.OnDone != nil {
				a.OnDone(n)
			}
		}
	}
}

// Pass analyzes every file lacking gain, then derives album gains. Returns files analyzed.
func (a *Analyzer) Pass(ctx context.Context) (int, error) {
	a.init()
	n := 0
	for {
		rows, err := a.DB.QueryContext(ctx, `SELECT id, path FROM source_tracks
			WHERE source='local' AND path IS NOT NULL AND rg_track_gain IS NULL AND loudness_error IS NULL LIMIT 20`)
		if err != nil {
			return n, err
		}
		type item struct{ id, path string }
		var batch []item
		for rows.Next() {
			var it item
			rows.Scan(&it.id, &it.path)
			batch = append(batch, it)
		}
		rows.Close()
		if len(batch) == 0 {
			break
		}
		for _, it := range batch {
			if ctx.Err() != nil {
				return n, ctx.Err()
			}
			lufs, peak, err := a.measure(ctx, it.path)
			if err != nil {
				if ctx.Err() != nil {
					return n, ctx.Err()
				}
				msg := err.Error()
				if len(msg) > 200 {
					msg = msg[:200]
				}
				a.DB.ExecContext(ctx, `UPDATE source_tracks SET loudness_error=? WHERE id=?`, msg, it.id)
				continue
			}
			gain := math.Round((Reference-lufs)*100) / 100
			if _, err := a.DB.ExecContext(ctx, `UPDATE source_tracks SET rg_track_gain=?, rg_track_peak=?, loudness_lufs=?,
				rg_source='analyzed' WHERE id=? AND rg_track_gain IS NULL`, gain, peak, lufs, it.id); err != nil {
				return n, err
			}
			n++
		}
	}
	return n, a.albumGains(ctx)
}

// albumGains derives album gain for analyzed files of fully analyzed albums
// (duration-weighted energy mean of track loudness; album peak = max).
func (a *Analyzer) albumGains(ctx context.Context) error {
	_, err := a.DB.ExecContext(ctx, `
		WITH al AS (
			SELECT t.album_id AS album_id,
				10*log10(sum(COALESCE(st.duration_ms,1)*pow(10, st.loudness_lufs/10.0)) / sum(COALESCE(st.duration_ms,1))) AS lufs,
				max(st.rg_track_peak) AS peak,
				sum(st.loudness_lufs IS NULL) AS missing
			FROM tracks t JOIN track_links l ON l.track_id=t.id JOIN source_tracks st ON st.id=l.source_track_id
			WHERE t.album_id IS NOT NULL AND st.rg_source='analyzed'
			GROUP BY t.album_id)
		UPDATE source_tracks SET
			rg_album_gain=round(? - (SELECT lufs FROM al JOIN track_links l2 ON l2.source_track_id=source_tracks.id
				JOIN tracks t2 ON t2.id=l2.track_id AND t2.album_id=al.album_id), 2),
			rg_album_peak=(SELECT peak FROM al JOIN track_links l2 ON l2.source_track_id=source_tracks.id
				JOIN tracks t2 ON t2.id=l2.track_id AND t2.album_id=al.album_id)
		WHERE rg_source='analyzed' AND rg_album_gain IS NULL AND loudness_lufs IS NOT NULL
			AND EXISTS (SELECT 1 FROM al JOIN track_links l2 ON l2.source_track_id=source_tracks.id
				JOIN tracks t2 ON t2.id=l2.track_id AND t2.album_id=al.album_id WHERE al.missing=0)`, Reference)
	return err
}

var (
	reI    = regexp.MustCompile(`(?m)^\s*I:\s+(-?[0-9.]+|-inf)\s+LUFS`)
	rePeak = regexp.MustCompile(`(?m)^\s*Peak:\s+(-?[0-9.]+|-inf)\s+dBFS`)
)

// measure runs ffmpeg ebur128 on a file and returns integrated loudness (LUFS) and linear true peak.
func (a *Analyzer) measure(ctx context.Context, path string) (float64, float64, error) {
	if a.Resolve != nil {
		p, err := a.Resolve(path)
		if err != nil {
			return 0, 0, fmt.Errorf("file not accessible")
		}
		path = p
	}
	ctx, cancel := context.WithTimeout(ctx, a.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, a.FFmpeg, "-nostdin", "-hide_banner", "-nostats", "-i", "file:"+path,
		"-map", "0:a:0", "-vn", "-af", "ebur128=peak=true", "-f", "null", "-")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return 0, 0, fmt.Errorf("ffmpeg: %w", err)
	}
	return parse(stderr.String())
}

func parse(out string) (float64, float64, error) {
	// the summary is printed last; use the final matches
	is := reI.FindAllStringSubmatch(out, -1)
	ps := rePeak.FindAllStringSubmatch(out, -1)
	if len(is) == 0 {
		return 0, 0, errors.New("no loudness summary")
	}
	if is[len(is)-1][1] == "-inf" {
		return 0, 0, errors.New("silent file")
	}
	lufs, err := strconv.ParseFloat(is[len(is)-1][1], 64)
	if err != nil {
		return 0, 0, err
	}
	peak := 1.0
	if len(ps) > 0 && ps[len(ps)-1][1] != "-inf" {
		if db, err := strconv.ParseFloat(ps[len(ps)-1][1], 64); err == nil {
			peak = math.Round(math.Pow(10, db/20)*1e6) / 1e6
		}
	}
	return lufs, peak, nil
}
