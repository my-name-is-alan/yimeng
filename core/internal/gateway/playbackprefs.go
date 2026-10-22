package gateway

import (
	"net/http"
)

const prefPlayback = "playback"

// PlaybackPrefs are per-account playback settings applied by clients (D53).
type PlaybackPrefs struct {
	ReplayGain      string  `json:"replaygain"`       // off | track | album
	PreampDB        float64 `json:"preamp_db"`        // -15..15
	PreventClipping bool    `json:"prevent_clipping"` // cap gain by 1/peak
	Gapless         bool    `json:"gapless"`
}

var defaultPlayback = PlaybackPrefs{ReplayGain: "track", PreventClipping: true, Gapless: true}

func (d Deps) playbackPrefs(r *http.Request) PlaybackPrefs {
	p := defaultPlayback
	if dev, ok := DeviceFrom(r.Context()); ok && d.UserData != nil {
		_, _ = d.UserData.GetPref(r.Context(), ownerKey(dev), prefPlayback, &p)
	}
	return p
}

// GET /api/v1/me/playback
func (d Deps) getPlaybackPrefs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, d.playbackPrefs(r))
}

// PUT /api/v1/me/playback (partial)
func (d Deps) putPlaybackPrefs(w http.ResponseWriter, r *http.Request) {
	var b struct {
		ReplayGain      *string  `json:"replaygain"`
		PreampDB        *float64 `json:"preamp_db"`
		PreventClipping *bool    `json:"prevent_clipping"`
		Gapless         *bool    `json:"gapless"`
	}
	if err := decodeBody(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	p := d.playbackPrefs(r)
	if b.ReplayGain != nil {
		switch *b.ReplayGain {
		case "off", "track", "album":
			p.ReplayGain = *b.ReplayGain
		default:
			writeErr(w, http.StatusBadRequest, "invalid_body", "replaygain must be off, track or album")
			return
		}
	}
	if b.PreampDB != nil {
		if *b.PreampDB < -15 || *b.PreampDB > 15 {
			writeErr(w, http.StatusBadRequest, "invalid_body", "preamp_db must be -15..15")
			return
		}
		p.PreampDB = *b.PreampDB
	}
	if b.PreventClipping != nil {
		p.PreventClipping = *b.PreventClipping
	}
	if b.Gapless != nil {
		p.Gapless = *b.Gapless
	}
	dev, _ := DeviceFrom(r.Context())
	if err := d.UserData.SetPref(r.Context(), ownerKey(dev), prefPlayback, p); err != nil {
		d.internal(w, "prefs", err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}
