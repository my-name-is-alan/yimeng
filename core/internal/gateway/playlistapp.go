package gateway

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
)

// App playlist item API (D45), alongside POST /playlists/{id}/items ops:
//
//	GET    /api/v1/playlists/{id}/tracks            list envelope; {id} may be an
//	                                                online playlist / chart id ("netease:3778678")
//	POST   /api/v1/playlists/{id}/tracks {track_ids[, before|after|position]}  add
//	PUT    /api/v1/playlists/{id}/tracks {track_ids}                           reorder / replace
//	DELETE /api/v1/playlists/{id}/tracks?indexes=0,3  or  ?track_ids=a,b       remove

func (d Deps) decoratePlaylists(r *http.Request, ps []library.Playlist) {
	dev, _ := DeviceFrom(r.Context())
	for i := range ps {
		ids, err := d.Playlists.PlaylistTrackIDs(r.Context(), ps[i].ID)
		if err != nil {
			continue
		}
		for _, tid := range ids {
			t, err := d.Library.GetTrack(r.Context(), tid)
			if err == nil && t.AlbumID != "" && t.CoverPath != "" {
				ps[i].Cover = d.coverRef(dev.ID, t.AlbumID, t.CoverPath)
				break
			}
		}
	}
}

func (d Deps) playlistTracks(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if library.IsExternalID(id) && d.Online != nil {
		pl, err := d.Online.Playlist(r.Context(), id)
		if err != nil {
			d.platformErr(w, "playlist", err)
			return
		}
		p, _ := listParams(r)
		if !applyCursor(r, &p) {
			writeErr(w, http.StatusBadRequest, "invalid_cursor", "cursor does not belong to this list")
			return
		}
		if p.Limit <= 0 {
			p.Limit = 50
		}
		items := pl.Tracks
		total := len(items)
		end := min(p.Offset+p.Limit, total)
		page := items[min(p.Offset, total):end]
		d.decorateOnlineTracks(page)
		d.writeList(w, r, map[string]any{"items": page, "total": total, "limit": p.Limit, "offset": p.Offset,
			"playlist": map[string]any{"id": pl.ID, "name": pl.Name, "origin": pl.Origin, "cover": d.onlineCover(pl.CoverURL), "track_count": pl.TrackCount}},
			p.Limit, p.Offset, total)
		return
	}
	p, _ := listParams(r)
	if !applyCursor(r, &p) {
		writeErr(w, http.StatusBadRequest, "invalid_cursor", "cursor does not belong to this list")
		return
	}
	if p.Limit <= 0 {
		p.Limit = 50
	}
	pg, err := d.Playlists.GetPlaylist(r.Context(), id, p.Limit, p.Offset)
	if err != nil {
		d.plErr(w, err)
		return
	}
	d.decorateTracks(r.Context(), pg.Tracks.Items)
	d.writeList(w, r, pg.Tracks, pg.Tracks.Limit, pg.Tracks.Offset, pg.Tracks.Total)
}

func (d Deps) playlistTracksEdit(w http.ResponseWriter, r *http.Request) {
	var op library.PlaylistOp
	switch r.Method {
	case http.MethodPost, http.MethodPut:
		var b struct {
			TrackIDs []string `json:"track_ids"`
			Before   string   `json:"before"`
			After    string   `json:"after"`
			Position *int     `json:"position"`
		}
		if err := decodeBodyMax(r, &b, 1<<20); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		op = library.PlaylistOp{Op: "append", TrackIDs: b.TrackIDs}
		if r.Method == http.MethodPut {
			op.Op = "replace"
		} else if b.Before != "" || b.After != "" || b.Position != nil {
			op = library.PlaylistOp{Op: "insert", TrackIDs: b.TrackIDs, Before: b.Before, After: b.After, Position: b.Position}
		}
	case http.MethodDelete:
		q := r.URL.Query()
		if v := q.Get("track_ids"); v != "" {
			op = library.PlaylistOp{Op: "remove", TrackIDs: strings.Split(v, ",")}
			break
		}
		var idx []int
		for _, s := range strings.Split(q.Get("indexes"), ",") {
			if s = strings.TrimSpace(s); s == "" {
				continue
			}
			n, err := strconv.Atoi(s)
			if err != nil || n < 0 {
				writeErr(w, http.StatusBadRequest, "invalid_query", "indexes must be non-negative integers")
				return
			}
			idx = append(idx, n)
		}
		if len(idx) == 0 {
			writeErr(w, http.StatusBadRequest, "invalid_query", "give indexes=0,3 or track_ids=a,b")
			return
		}
		// remove from the highest index down so earlier indexes stay valid
		sort.Sort(sort.Reverse(sort.IntSlice(idx)))
		var p library.Playlist
		var err error
		last := -1
		for _, i := range idx {
			if i == last {
				continue
			}
			last = i
			if p, err = d.Playlists.EditPlaylist(r.Context(), r.PathValue("id"), library.PlaylistOp{Op: "remove", Index: i}); err != nil {
				d.plErr(w, err)
				return
			}
		}
		d.plDone(w, http.StatusOK, p, nil)
		return
	}
	p, err := d.Playlists.EditPlaylist(r.Context(), r.PathValue("id"), op)
	d.plDone(w, http.StatusOK, p, err)
}
