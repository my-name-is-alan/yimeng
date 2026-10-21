// Package local is the "local" source adapter: it reads tags, embedded cover
// art and duration from audio files on disk. It returns DTOs only and never
// touches the database (docs §5).
package local

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dhowden/tag"
)

// Supported reports whether the file extension is an audio format we index.
func Supported(path string) bool {
	_, ok := formats[strings.ToLower(filepath.Ext(path))]
	return ok
}

var formats = map[string]string{
	".mp3": "mp3", ".flac": "flac", ".m4a": "m4a", ".m4b": "m4a", ".mp4": "m4a", ".alac": "m4a",
	".ogg": "ogg", ".oga": "ogg", ".opus": "opus", ".wav": "wav", ".dsf": "dsf",
}

type Picture struct {
	MIME string
	Ext  string
	Data []byte
}

// Track is the DTO extracted from one file.
type Track struct {
	Path          string   `json:"path"`
	Format        string   `json:"format"`
	TagFormat     string   `json:"tag_format,omitempty"`
	Title         string   `json:"title"`
	Artist        string   `json:"artist,omitempty"`
	Album         string   `json:"album,omitempty"`
	AlbumArtist   string   `json:"album_artist,omitempty"`
	Composer      string   `json:"composer,omitempty"`
	Genre         string   `json:"genre,omitempty"`
	Year          int      `json:"year,omitempty"`
	TrackNo       int      `json:"track_no,omitempty"`
	TrackTotal    int      `json:"track_total,omitempty"`
	DiscNo        int      `json:"disc_no,omitempty"`
	DiscTotal     int      `json:"disc_total,omitempty"`
	DurationMs    int64    `json:"duration_ms,omitempty"`
	ISRC          string   `json:"isrc,omitempty"`
	MBRecordingID string   `json:"mb_recording_id,omitempty"`
	MBReleaseID   string   `json:"mb_release_id,omitempty"`
	HasTags       bool     `json:"has_tags"`
	Audio         Info     `json:"audio"`
	Lyrics        string   `json:"-"`
	Picture       *Picture `json:"-"`
}

// ReadFile extracts tags, embedded picture and duration. A file without tags
// is not an error: Title falls back to the file name.
func ReadFile(path string) (Track, error) {
	t := Track{Path: path, Format: formats[strings.ToLower(filepath.Ext(path))]}
	f, err := os.Open(path)
	if err != nil {
		return t, err
	}
	defer f.Close()

	m, err := tag.ReadFrom(f)
	switch {
	case err == nil:
		t.HasTags = true
		t.TagFormat = string(m.Format())
		t.Title = clean(m.Title())
		t.Artist = clean(m.Artist())
		t.Album = clean(m.Album())
		t.AlbumArtist = clean(m.AlbumArtist())
		t.Composer = clean(m.Composer())
		t.Genre = clean(m.Genre())
		t.Year = m.Year()
		t.TrackNo, t.TrackTotal = m.Track()
		t.DiscNo, t.DiscTotal = m.Disc()
		if p := m.Picture(); p != nil && len(p.Data) > 0 {
			t.Picture = &Picture{MIME: p.MIMEType, Ext: pictureExt(p), Data: p.Data}
		}
		extractIDs(&t, m.Raw())
		t.Lyrics = embeddedLyrics(m)
		if isID3(m.Format()) {
			fixTrackCharset(&t)
		}
	case errors.Is(err, tag.ErrNoTagsFound):
	default:
		// Corrupt / unsupported tag block: index the file anyway.
	}
	if t.Title == "" {
		base := filepath.Base(path)
		t.Title = strings.TrimSuffix(base, filepath.Ext(base))
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return t, err
	}
	if in, err := Probe(f, t.Format); err == nil {
		t.Audio = in
		t.DurationMs = in.Duration.Milliseconds()
	}
	return t, nil
}

func clean(s string) string { return strings.TrimSpace(strings.Trim(s, "\x00")) }

func pictureExt(p *tag.Picture) string {
	switch strings.ToLower(p.MIMEType) {
	case "image/png":
		return "png"
	case "image/jpeg", "image/jpg":
		return "jpg"
	case "image/webp":
		return "webp"
	case "image/gif":
		return "gif"
	}
	if e := strings.ToLower(strings.TrimPrefix(p.Ext, ".")); e != "" {
		return e
	}
	return "jpg"
}

// extractIDs pulls ISRC / MusicBrainz IDs out of the raw tag map, which uses
// format-specific keys (Vorbis comments lowercase, ID3 frames, MP4 atoms).
func extractIDs(t *Track, raw map[string]any) {
	set := func(dst *string, v string) {
		if *dst == "" {
			*dst = clean(v)
		}
	}
	for k, v := range raw {
		key := strings.ToLower(k)
		var desc, val string
		switch x := v.(type) {
		case string:
			val = x
		case []string:
			if len(x) > 0 {
				val = x[0]
			}
		case *tag.Comm: // ID3 TXXX
			desc, val = strings.ToLower(x.Description), x.Text
		case fmt.Stringer:
			val = x.String()
		}
		if desc != "" {
			key = desc
		}
		switch key {
		case "isrc", "tsrc":
			set(&t.ISRC, val)
		case "musicbrainz_trackid", "musicbrainz track id", "ufid":
			if key != "ufid" {
				set(&t.MBRecordingID, val)
			}
		case "musicbrainz_albumid", "musicbrainz album id":
			set(&t.MBReleaseID, val)
		}
	}
	// Vorbis comments often store "3/10"; dhowden/tag only parses plain ints.
	numPair(raw, "tracknumber", &t.TrackNo, &t.TrackTotal)
	numPair(raw, "discnumber", &t.DiscNo, &t.DiscTotal)
	numPair(raw, "tracktotal", &t.TrackTotal, nil)
	numPair(raw, "totaltracks", &t.TrackTotal, nil)
	numPair(raw, "disctotal", &t.DiscTotal, nil)
	numPair(raw, "totaldiscs", &t.DiscTotal, nil)
	if t.Year == 0 {
		if s, ok := raw["date"].(string); ok && len(s) >= 4 {
			t.Year, _ = strconv.Atoi(s[:4])
		}
	}
}

func numPair(raw map[string]any, key string, n, total *int) {
	s, ok := raw[key].(string)
	if !ok {
		return
	}
	a, b, _ := strings.Cut(strings.TrimSpace(s), "/")
	if *n == 0 {
		*n, _ = strconv.Atoi(strings.TrimSpace(a))
	}
	if total != nil && *total == 0 && b != "" {
		*total, _ = strconv.Atoi(strings.TrimSpace(b))
	}
}

// embeddedLyrics returns USLT (ID3), LYRICS/UNSYNCEDLYRICS (Vorbis) or ©lyr (MP4).
func embeddedLyrics(m tag.Metadata) string {
	if l := strings.TrimSpace(m.Lyrics()); l != "" {
		return capLyrics(l)
	}
	for k, v := range m.Raw() {
		switch strings.ToLower(k) {
		case "lyrics", "unsyncedlyrics", "unsynced lyrics", "\xa9lyr", "©lyr", "uslt":
			switch x := v.(type) {
			case string:
				if strings.TrimSpace(x) != "" {
					return capLyrics(strings.TrimSpace(x))
				}
			case *tag.Comm:
				if strings.TrimSpace(x.Text) != "" {
					return capLyrics(strings.TrimSpace(x.Text))
				}
			}
		}
	}
	return ""
}

func capLyrics(s string) string {
	if len(s) > 256<<10 {
		return s[:256<<10]
	}
	return s
}
