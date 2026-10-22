package gateway

import (
	"context"
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
	"github.com/my-name-is-alan/yimeng/core/internal/metadata"
)

// Artist image editor: upload / from-match (Spotify, 网易云, QQ音乐, 酷狗,
// MusicBrainz when it has one). Stored like uploaded album covers
// (artist_images), so images survive rescans. Local artists only.

func (d Deps) putArtistImage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !d.requireLocal(w, r, "artist", id) {
		return
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, library.MaxCoverBytes+1))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	d.saveArtistImage(w, r, id, b, "upload")
}

func (d Deps) saveArtistImage(w http.ResponseWriter, r *http.Request, id string, img []byte, source string) {
	if err := d.UserEdits.SetArtistImage(r.Context(), id, img, source); err != nil {
		d.editErr(w, "artist", err)
		return
	}
	d.Hub.Publish("library.artist_image", map[string]string{"artist_id": id})
	a, err := d.Library.GetArtist(r.Context(), id)
	if err != nil {
		d.editErr(w, "artist", err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (d Deps) deleteArtistImage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !d.requireLocal(w, r, "artist", id) {
		return
	}
	if err := d.UserEdits.ClearArtistImage(r.Context(), id); err != nil {
		d.editErr(w, "artist", err)
		return
	}
	d.Hub.Publish("library.artist_image", map[string]string{"artist_id": id})
	w.WriteHeader(http.StatusNoContent)
}

// ArtistImageCandidate is one picture suggestion.
type ArtistImageCandidate struct {
	Provider   string  `json:"provider"`
	ExternalID string  `json:"external_id,omitempty"`
	Name       string  `json:"name"`
	ImageURL   string  `json:"image_url"`
	Followers  int     `json:"followers,omitempty"`
	Score      float64 `json:"score"`
}

func (d Deps) artistImageCandidates(w http.ResponseWriter, r *http.Request) {
	if !d.matcher(w) {
		return
	}
	id := r.PathValue("id")
	if !d.requireLocal(w, r, "artist", id) {
		return
	}
	a, err := d.Library.GetArtist(r.Context(), id)
	if err != nil {
		d.editErr(w, "artist", err)
		return
	}
	name := a.Name
	if v := r.URL.Query().Get("name"); v != "" && len(v) <= 200 {
		name = v
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	infos, err := d.Matcher.LookupArtist(ctx, metadata.ArtistQuery{Name: name})
	if err != nil {
		writeErr(w, http.StatusBadGateway, "provider_error", "metadata providers unavailable: "+err.Error())
		return
	}
	out := []ArtistImageCandidate{}
	for _, in := range infos {
		if len(in.ImageURLs) == 0 {
			continue
		}
		sc := 0.8
		if in.Name == name {
			sc = 1
		}
		out = append(out, ArtistImageCandidate{Provider: string(in.Provider), ExternalID: in.ExternalID, Name: in.Name,
			ImageURL: in.ImageURLs[0], Followers: in.Followers, Score: sc})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (d Deps) artistImageFromMatch(w http.ResponseWriter, r *http.Request) {
	if !d.matcher(w) {
		return
	}
	id := r.PathValue("id")
	if !d.requireLocal(w, r, "artist", id) {
		return
	}
	var b struct {
		Provider string `json:"provider"`
		URL      string `json:"url"`
	}
	if err := decodeBodyMax(r, &b, 4096); err != nil || b.Provider == "" || b.URL == "" {
		writeErr(w, http.StatusBadRequest, "invalid_body", "provider and url are required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	rc, err := d.Matcher.FetchCover(ctx, metadata.CoverCandidate{Provider: metadata.ProviderID(b.Provider), URL: b.URL})
	if err != nil {
		writeErr(w, http.StatusBadGateway, "provider_error", "image fetch failed: "+err.Error())
		return
	}
	defer rc.Close()
	img, err := io.ReadAll(io.LimitReader(rc, library.MaxCoverBytes+1))
	if err != nil {
		writeErr(w, http.StatusBadGateway, "provider_error", "image fetch failed")
		return
	}
	d.saveArtistImage(w, r, id, img, b.Provider)
}
