package gateway

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
	"github.com/my-name-is-alan/yimeng/core/internal/transcode"
)

// download serves a track for offline caching: Content-Disposition attachment,
// full Content-Length when possible (original or already-cached transcode).
// Query format/bitrate mirrors /api/stream; originals are the default.
func (d Deps) download(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	f, err := d.Library.TrackFile(r.Context(), id)
	if errors.Is(err, library.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not_found", "file not found")
		return
	}
	if err != nil {
		d.internal(w, "resolve file", err)
		return
	}
	tr, _ := d.Library.GetTrack(r.Context(), id)

	p, _, err := d.streamProfile(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_format", err.Error())
		return
	}
	if !p.IsOriginal() {
		if d.Transcoder == nil || !d.Transcoder.Supports(p) {
			writeErr(w, http.StatusBadRequest, "format_unavailable", "transcoding to "+p.Key()+" is not available on this server")
			return
		}
		if redundant(p, f.Stream) {
			p = transcode.Profile{Format: transcode.Original}
		}
	}

	ext := f.Format
	ct := audioTypes[f.Format]
	path := f.Path
	modTime := time.Unix(0, f.Mtime)

	if !p.IsOriginal() {
		src := transcode.Source{TrackID: id, Path: f.Path, Size: f.Size, Mtime: f.Mtime}
		cached, ok := d.Transcoder.Cached(src, p)
		if !ok {
			// Materialize into cache before answering so the client gets a
			// finite file (Range + Content-Length) suitable for offline store.
			rc, err := d.Transcoder.Open(r.Context(), src, p)
			if err != nil {
				d.Log.Warn("download transcode", "track", id, "err", err)
				writeErr(w, http.StatusServiceUnavailable, "transcode_failed", "transcode could not be started")
				return
			}
			_, _ = io.Copy(io.Discard, rc)
			_ = rc.Close()
			cached, ok = d.Transcoder.Cached(src, p)
			if !ok {
				writeErr(w, http.StatusServiceUnavailable, "transcode_failed", "transcode did not finish")
				return
			}
		}
		path = cached
		ext = string(p.Format)
		ct = p.ContentType()
		modTime = time.Unix(0, f.Mtime)
	}
	if ct == "" {
		ct = "application/octet-stream"
	}
	name := downloadFilename(tr, ext)
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", contentDisposition(name))
	w.Header().Set("X-Download-Profile", p.Key())
	w.Header().Set("Cache-Control", "private, no-transform")
	w.Header().Add("Vary", "Authorization")
	fh, err := os.Open(path)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not_found", "file not found")
		return
	}
	defer fh.Close()
	http.ServeContent(w, r, name, modTime, fh)
}

var unsafeFile = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

func downloadFilename(t library.Track, ext string) string {
	ext = strings.TrimPrefix(strings.ToLower(ext), ".")
	if ext == "" {
		ext = "bin"
	}
	base := strings.TrimSpace(t.Title)
	if base == "" {
		base = t.ID
	}
	if art := strings.TrimSpace(t.ArtistName); art != "" {
		base = art + " - " + base
	}
	base = unsafeFile.ReplaceAllString(base, "_")
	base = strings.TrimSpace(base)
	// collapse runs of space/underscore
	base = strings.Join(strings.FieldsFunc(base, func(r rune) bool {
		return unicode.IsSpace(r)
	}), " ")
	if base == "" {
		base = "track"
	}
	if len(base) > 180 {
		base = base[:180]
	}
	return base + "." + ext
}

func contentDisposition(filename string) string {
	// ASCII fallback + RFC 5987 filename*
	ascii := unsafeFile.ReplaceAllString(filename, "_")
	ascii = strings.Map(func(r rune) rune {
		if r > 0x7e || r < 0x20 {
			return '_'
		}
		return r
	}, ascii)
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, ascii, pctEncode(filename))
}

func pctEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '.' || c == '_' || c == '~' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
