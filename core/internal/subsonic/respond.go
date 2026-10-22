// Package subsonic implements a Subsonic-compatible /rest API surface so
// third-party clients (Symfonium, play:Sub, …) can browse and stream the
// yimeng library. Auth (D59): OpenSubsonic apiKey, or u + t/s with a
// per-account app password (never the login password); see auth.go.
package subsonic

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
	"strings"
)

const APIVersion = "1.16.1"

type envelope struct {
	XMLName       xml.Name `xml:"subsonic-response" json:"-"`
	Status        string   `xml:"status,attr" json:"status"`
	Version       string   `xml:"version,attr" json:"version"`
	Type          string   `xml:"type,attr" json:"type"`
	ServerVersion string   `xml:"serverVersion,attr" json:"serverVersion"`
	OpenSubsonic  bool     `xml:"openSubsonic,attr" json:"openSubsonic"`

	Error *subErr `xml:"error,omitempty" json:"error,omitempty"`

	MusicFolders  *musicFolders   `xml:"musicFolders,omitempty" json:"musicFolders,omitempty"`
	Artists       *artistsIdx     `xml:"artists,omitempty" json:"artists,omitempty"`
	Indexes       *indexes        `xml:"indexes,omitempty" json:"indexes,omitempty"`
	AlbumList2    *albumList2     `xml:"albumList2,omitempty" json:"albumList2,omitempty"`
	Album         *albumDetail    `xml:"album,omitempty" json:"album,omitempty"`
	Song          *song           `xml:"song,omitempty" json:"song,omitempty"`
	SearchResult3 *searchResult3  `xml:"searchResult3,omitempty" json:"searchResult3,omitempty"`
	Playlists     *playlists      `xml:"playlists,omitempty" json:"playlists,omitempty"`
	Playlist      *playlistDetail `xml:"playlist,omitempty" json:"playlist,omitempty"`
	Lyrics        *lyrics         `xml:"lyrics,omitempty" json:"lyrics,omitempty"`
	License       *license        `xml:"license,omitempty" json:"license,omitempty"`
	Extensions    []extension     `xml:"openSubsonicExtensions,omitempty" json:"openSubsonicExtensions,omitempty"`
	Artist        *artistDetail   `xml:"artist,omitempty" json:"artist,omitempty"`
	Starred2      *starred2       `xml:"starred2,omitempty" json:"starred2,omitempty"`
	LyricsList    *lyricsList     `xml:"lyricsList,omitempty" json:"lyricsList,omitempty"`
	User          *user           `xml:"user,omitempty" json:"user,omitempty"`
	PlayQueue     *playQueue      `xml:"playQueue,omitempty" json:"playQueue,omitempty"`
}

type subErr struct {
	Code    int    `xml:"code,attr" json:"code"`
	Message string `xml:"message,attr" json:"message"`
}

type license struct {
	Valid bool `xml:"valid,attr" json:"valid"`
}

func writeOK(w http.ResponseWriter, r *http.Request, version string, mut func(*envelope)) {
	e := &envelope{
		Status: "ok", Version: APIVersion, Type: "yimeng", ServerVersion: version, OpenSubsonic: true,
	}
	if mut != nil {
		mut(e)
	}
	writeEnv(w, r, e, http.StatusOK)
}

func writeErr(w http.ResponseWriter, r *http.Request, code int, msg string) {
	e := &envelope{
		Status: "failed", Version: APIVersion, Type: "yimeng", OpenSubsonic: true,
		Error: &subErr{Code: code, Message: msg},
	}
	httpStatus := http.StatusOK // Subsonic always returns 200 with status=failed
	writeEnv(w, r, e, httpStatus)
}

func writeEnv(w http.ResponseWriter, r *http.Request, e *envelope, status int) {
	f := strings.ToLower(r.URL.Query().Get("f"))
	if f == "json" || f == "jsonp" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		payload := map[string]any{"subsonic-response": e}
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(payload)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(e)
}
