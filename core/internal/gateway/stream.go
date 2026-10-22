package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
	"github.com/my-name-is-alan/yimeng/core/internal/transcode"
)

// StreamPrefs stores per-device default stream quality.
type StreamPrefs interface {
	Get(ctx context.Context, deviceID string) (transcode.Profile, error)
	Set(ctx context.Context, deviceID string, p transcode.Profile) error
}

// streamProfile resolves the requested profile: explicit query parameters
// win, otherwise the device default. explicit reports a query request.
func (d Deps) streamProfile(r *http.Request) (p transcode.Profile, explicit bool, err error) {
	q := r.URL.Query()
	if q.Has("format") || q.Has("bitrate") {
		p, err = transcode.ParseProfile(q.Get("format"), q.Get("bitrate"))
		return p, true, err
	}
	p = transcode.Profile{Format: transcode.Original}
	if dev, ok := DeviceFrom(r.Context()); ok && d.StreamPrefs != nil {
		if def, err := d.StreamPrefs.Get(r.Context(), dev.ID); err == nil {
			p = def
		}
	}
	return p, false, nil
}

// redundant reports a transcode that cannot improve on the source
// (same format, not lower bitrate) — the original is served instead.
func redundant(p transcode.Profile, s library.StreamInfo) bool {
	switch {
	case p.Format == "flac":
		return s.Format == "flac"
	case p.Format == "mp3" && s.Codec == "mp3", p.Format == "opus" && s.Codec == "opus":
		return s.BitrateKbps > 0 && s.BitrateKbps <= p.BitrateKbps
	}
	return false
}

// stream serves a track: the original file (Range, ETag, If-Range) or an
// ffmpeg transcode. Cached transcodes support Range like files; a first-time
// transcode is streamed progressively (chunked, Accept-Ranges: none) while it
// is being written to the cache.
func (d Deps) stream(w http.ResponseWriter, r *http.Request) {
	if d.streamExternal(w, r, r.PathValue("id")) {
		return
	}
	f, err := d.Library.TrackFile(r.Context(), r.PathValue("id"))
	if errors.Is(err, library.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not_found", "file not found")
		return
	}
	if err != nil {
		d.internal(w, "resolve file", err)
		return
	}
	p, explicit, err := d.streamProfile(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_format", err.Error())
		return
	}
	if !d.Transcoder.Supports(p) {
		if explicit {
			writeErr(w, http.StatusBadRequest, "format_unavailable", "transcoding to "+p.Key()+" is not available on this server")
			return
		}
		p = transcode.Profile{Format: transcode.Original}
	}
	if !p.IsOriginal() && redundant(p, f.Stream) {
		p = transcode.Profile{Format: transcode.Original}
	}
	w.Header().Set("X-Stream-Profile", p.Key())
	w.Header().Add("Vary", "Authorization")
	if p.IsOriginal() {
		localAudioHeaders(w.Header(), f.Stream, "", 0)
		ct := audioTypes[f.Format]
		if ct == "" {
			ct = "application/octet-stream"
		}
		w.Header().Set("ETag", `"o-`+strconv.FormatInt(f.Size, 36)+`-`+strconv.FormatInt(f.Mtime, 36)+`"`)
		d.serveFile(w, r, f.Path, nil, ct, "private, no-transform")
		return
	}

	src := transcode.Source{TrackID: r.PathValue("id"), Path: f.Path, Size: f.Size, Mtime: f.Mtime}
	modTime := time.Unix(0, f.Mtime)
	h := w.Header()
	h.Set("Content-Type", p.ContentType())
	localAudioHeaders(h, f.Stream, p.Format, p.BitrateKbps)
	h.Set("ETag", `"`+transcode.Key(src, p)+`"`)
	h.Set("Cache-Control", "private, no-transform")
	if path, ok := d.Transcoder.Cached(src, p); ok {
		fh, err := os.Open(path)
		if err == nil {
			defer fh.Close()
			http.ServeContent(w, r, "", modTime, fh) // Range, If-Range, HEAD, 304
			return
		}
	}
	// progressive first-time transcode
	h.Set("Last-Modified", modTime.UTC().Format(http.TimeFormat))
	h.Set("Accept-Ranges", "none")
	if inm := r.Header.Get("If-None-Match"); inm != "" && strings.Contains(inm, h.Get("ETag")) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	rc, err := d.Transcoder.Open(r.Context(), src, p)
	if err != nil {
		if r.Context().Err() == nil {
			d.Log.Warn("transcode start", "track", src.TrackID, "profile", p.Key(), "err", err)
			writeErr(w, http.StatusServiceUnavailable, "transcode_failed", "transcode could not be started")
		}
		return
	}
	defer rc.Close()
	w.WriteHeader(http.StatusOK)
	rcw := http.NewResponseController(w)
	buf := make([]byte, 32<<10)
	for {
		n, err := rc.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			rcw.Flush()
		}
		if err == io.EOF {
			return
		}
		if err != nil {
			// headers are gone; abort so the client sees a truncated body, not a clean EOF
			d.Log.Warn("transcode", "track", src.TrackID, "profile", p.Key(), "err", err)
			panic(http.ErrAbortHandler)
		}
	}
}

type streamQuality struct {
	Format      string                 `json:"format"`
	BitrateKbps int                    `json:"bitrate_kbps,omitempty"`
	Available   []transcode.FormatInfo `json:"available"`
}

func (d Deps) getStreamQuality(w http.ResponseWriter, r *http.Request) {
	if d.StreamPrefs == nil {
		writeErr(w, http.StatusNotImplemented, "not_configured", "stream preferences unavailable")
		return
	}
	dev, _ := DeviceFrom(r.Context())
	p, err := d.StreamPrefs.Get(r.Context(), dev.ID)
	if err != nil {
		d.internal(w, "stream prefs", err)
		return
	}
	writeJSON(w, http.StatusOK, streamQuality{Format: p.Format, BitrateKbps: p.BitrateKbps, Available: d.Transcoder.Formats()})
}

func (d Deps) putStreamQuality(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Format      string `json:"format"`
		BitrateKbps *int   `json:"bitrate_kbps"`
	}
	if d.StreamPrefs == nil {
		writeErr(w, http.StatusNotImplemented, "not_configured", "stream preferences unavailable")
		return
	}
	if err := decodeBody(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	br := ""
	if b.BitrateKbps != nil {
		br = strconv.Itoa(*b.BitrateKbps)
	}
	p, err := transcode.ParseProfile(b.Format, br)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_format", err.Error())
		return
	}
	// A default the server cannot produce right now is still stored (ffmpeg may be
	// installed later); streams fall back to the original meanwhile.
	dev, _ := DeviceFrom(r.Context())
	if err := d.StreamPrefs.Set(r.Context(), dev.ID, p); err != nil {
		d.internal(w, "stream prefs", err)
		return
	}
	d.getStreamQuality(w, r)
}
