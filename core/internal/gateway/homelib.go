package gateway

import (
	"context"
	"hash/fnv"
	"math/rand"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
	"github.com/my-name-is-alan/yimeng/core/internal/online"
)

// Library part of /api/v1/home (D42/D45), merged into the top level next to
// "sections". Objective data only — Core has no recommendation system; the
// platform "sections" are the logged-in account's own platform
// recommendations passed through as-is.
//
//	banner           albums with covers (≤5); falls back beyond local if needed
//	recently_added   albums, newest first
//	recently_played  albums of the latest plays (unique)
//	frequent         albums played most in the last 90 days
//	random           albums, a fixed random pick per day (随机播放)
//	top_artists      artists played most in the last 90 days (else by track count)
//	playlists        Core playlists
//	charts           {title, origins} entry to the charts page
//	recently_played_tracks / frequent_tracks   the same as tracks
func (d Deps) homeLibrary(ctx context.Context) map[string]any {
	albums := func() []library.Album { return []library.Album{} }
	out := map[string]any{"banner": albums(), "recently_added": albums(), "recently_played": albums(), "frequent": albums(),
		"random": albums(), "top_artists": []library.Artist{}, "playlists": []library.Playlist{},
		"recently_played_tracks": []library.Track{}, "frequent_tracks": []library.Track{}}
	if d.Library == nil {
		return out
	}
	if pg, err := d.Library.ListAlbums(ctx, library.ListParams{Sort: "added", Desc: true, Limit: 20, Origin: library.OriginLocal}); err == nil {
		d.decorateAlbums(ctx, pg.Items)
		out["recently_added"] = pg.Items
		var banner []library.Album
		for _, a := range pg.Items {
			if a.Cover != nil && len(banner) < 5 {
				banner = append(banner, a)
			}
		}
		// Empty local music root: still fill banner from any albums that have covers
		// (recently_added first, then a broader ListAlbums without origin filter).
		if len(banner) == 0 {
			banner = albumsWithCover(pg.Items, 5)
		}
		if len(banner) == 0 {
			if all, err := d.Library.ListAlbums(ctx, library.ListParams{Sort: "added", Desc: true, Limit: 40}); err == nil {
				d.decorateAlbums(ctx, all.Items)
				banner = albumsWithCover(all.Items, 5)
			}
		}
		if len(banner) > 0 {
			out["banner"] = banner
		}
	}
	if pg, err := d.Library.ListAlbums(ctx, library.ListParams{Limit: 500, Origin: library.OriginLocal}); err == nil && len(pg.Items) > 0 {
		h := fnv.New64a()
		h.Write([]byte(time.Now().Format("2006-01-02")))
		rng := rand.New(rand.NewSource(int64(h.Sum64())))
		rng.Shuffle(len(pg.Items), func(i, j int) { pg.Items[i], pg.Items[j] = pg.Items[j], pg.Items[i] })
		pick := pg.Items[:min(6, len(pg.Items))]
		d.decorateAlbums(ctx, pick)
		out["random"] = pick
		if b, ok := out["banner"].([]library.Album); (!ok || len(b) == 0) {
			if fill := albumsWithCover(pick, 5); len(fill) > 0 {
				out["banner"] = fill
			}
		}
	} else if b, ok := out["banner"].([]library.Album); (!ok || len(b) == 0) {
		// Local empty — try any-origin albums for a day-stable random banner.
		if pg, err := d.Library.ListAlbums(ctx, library.ListParams{Limit: 200}); err == nil && len(pg.Items) > 0 {
			h := fnv.New64a()
			h.Write([]byte(time.Now().Format("2006-01-02")))
			rng := rand.New(rand.NewSource(int64(h.Sum64())))
			rng.Shuffle(len(pg.Items), func(i, j int) { pg.Items[i], pg.Items[j] = pg.Items[j], pg.Items[i] })
			pick := pg.Items[:min(6, len(pg.Items))]
			d.decorateAlbums(ctx, pick)
			out["random"] = pick
			if fill := albumsWithCover(pick, 5); len(fill) > 0 {
				out["banner"] = fill
			}
		}
	}
	albumOf := func(ts []library.Track, max int) []library.Album {
		res := []library.Album{}
		seen := map[string]bool{}
		for _, t := range ts {
			if t.AlbumID == "" || seen[t.AlbumID] || len(res) >= max {
				continue
			}
			seen[t.AlbumID] = true
			if a, err := d.Library.GetAlbum(ctx, t.AlbumID); err == nil {
				res = append(res, a)
			}
		}
		d.decorateAlbums(ctx, res)
		return res
	}
	if d.UserData != nil {
		var played []library.Track
		if items, _, err := d.UserData.History(ctx, "", 100, 0); err == nil {
			seen := map[string]bool{}
			for _, h := range items {
				if seen[h.TrackID] || !h.Available || len(played) >= 20 {
					continue
				}
				seen[h.TrackID] = true
				t, err := d.Library.GetTrack(ctx, h.TrackID)
				if err != nil {
					t = library.Track{ID: h.TrackID, Title: h.Title, ArtistName: h.ArtistName, AlbumID: h.AlbumID, AlbumTitle: h.AlbumTitle, Playable: true}
					if o, _, ok := library.SplitExternalID(h.TrackID); ok {
						t.Origin = o
					}
				}
				played = append(played, t)
			}
		}
		var freq []library.Track
		since := time.Now().AddDate(0, 0, -90)
		if top, err := d.UserData.Top(ctx, "track", since, 20); err == nil {
			for _, it := range top {
				if t, err := d.Library.GetTrack(ctx, it.ID); err == nil {
					freq = append(freq, t)
				}
			}
		}
		if played != nil {
			d.decorateTracks(ctx, played)
			out["recently_played_tracks"] = played
			out["recently_played"] = albumOf(played, 12)
		}
		if freq != nil {
			d.decorateTracks(ctx, freq)
			out["frequent_tracks"] = freq
			out["frequent"] = albumOf(freq, 12)
		}
		if top, err := d.UserData.Top(ctx, "artist", since, 12); err == nil && len(top) > 0 {
			var as []library.Artist
			for _, it := range top {
				if a, err := d.Library.GetArtist(ctx, it.ID); err == nil {
					as = append(as, a)
				}
			}
			d.decorateArtists(ctx, as)
			if as != nil {
				out["top_artists"] = as
			}
		}
	}
	if as, ok := out["top_artists"].([]library.Artist); ok && len(as) == 0 {
		if pg, err := d.Library.ListArtists(ctx, library.ListParams{Limit: 12, Origin: library.OriginLocal}); err == nil {
			d.decorateArtists(ctx, pg.Items)
			out["top_artists"] = pg.Items
		}
	}
	if d.Playlists != nil {
		if l, err := d.Playlists.ListPlaylists(ctx); err == nil {
			out["playlists"] = l
		}
	}
	if d.onlineFor(ctx) != nil {
		var os []string
		for _, id := range d.onlineFor(ctx).Reg.EnabledIDs() {
			if st, err := d.onlineFor(ctx).Reg.Status(ctx, id); err == nil && hasCap(st.Manifest.Capabilities, online.CapCharts) {
				os = append(os, id)
			}
		}
		if len(os) > 0 {
			out["charts"] = map[string]any{"title": "排行", "origins": os}
		}
	}
	return out
}


// albumsWithCover returns up to max albums that have a Cover (prefer Cover != nil).
func albumsWithCover(as []library.Album, max int) []library.Album {
	out := make([]library.Album, 0, max)
	for _, a := range as {
		if a.Cover != nil && len(out) < max {
			out = append(out, a)
		}
	}
	if len(out) > 0 {
		return out
	}
	// Fall back to HasCover when Cover ref was not decorated yet.
	for _, a := range as {
		if a.HasCover && len(out) < max {
			out = append(out, a)
		}
	}
	return out
}

func hasCap(cs []online.Capability, c online.Capability) bool {
	for _, x := range cs {
		if x == c {
			return true
		}
	}
	return false
}
