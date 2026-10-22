package library

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/search"

	"golang.org/x/text/unicode/norm"
)

const (
	UnknownArtist  = "Unknown Artist"
	VariousArtists = "Various Artists"
)

// Link confidences recorded in track_links.
const (
	ConfMBID  = 1.0  // MusicBrainz recording id (+ same album)
	ConfISRC  = 0.95 // ISRC (+ same album)
	ConfFuzzy = 0.8  // same album, normalized title + primary artist, duration ±2s
	ConfSelf  = 1.0  // the only source of its canonical track
)

type ArtistCredit struct {
	Name string
	Role string // primary | feat
}

// normKey normalizes for grouping: NFKC (fullwidth→ASCII), lower-case, collapsed spaces.
func normKey(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(norm.NFKC.String(s))), " ")
}

func hid(prefix, key string) string {
	sum := sha1.Sum([]byte(key))
	return prefix + hex.EncodeToString(sum[:10])
}

type srcRow struct {
	id, path, title, artist, album, albumArtist, genre, isrc, mbRec, mbRel, cover string
	trackNo, discNo, year                                                         int
	dur, created                                                                  int64
}

type mTrack struct {
	id, albumID, title, artistName, genre, isrc, mbRec string
	trackNo, discNo, year                              int
	dur, created                                       int64
	artists                                            []ArtistCredit
	links                                              map[string]float64
}

type mAlbum struct {
	id, title, artistName, cover, mbRel, genre string
	year                                       int
	created                                    int64
	artists                                    []ArtistCredit
}

// Merge rebuilds the canonical layer (artists / albums / tracks and their
// joins + track_links) from source_tracks. IDs are derived deterministically
// from grouping keys, so re-running is idempotent and canonical IDs are
// stable across rescans; entities with no remaining sources are deleted.
//
// Grouping:
//   - album: MusicBrainz release id if present, else
//     normalized(album artist) + normalized(album) + year;
//     album artist = ALBUMARTIST tag, else the track's first primary artist.
//   - track: within an album, MB recording id → ISRC → normalized title +
//     primary artist with duration within ±2s; otherwise one track per file.
func (s *Scanner) Merge(ctx context.Context) error {
	s.mergeMu.Lock()
	defer s.mergeMu.Unlock()
	sp := s.splitter()
	rows, err := s.db.QueryContext(ctx, `SELECT id, COALESCE(path,''), COALESCE(title,''), COALESCE(artist,''), COALESCE(album,''),
		COALESCE(album_artist,''), COALESCE(genre,''), COALESCE(isrc,''), COALESCE(mb_recording_id,''),
		COALESCE(mb_release_id,''), COALESCE(cover_path,''), COALESCE(track_no,0), COALESCE(disc_no,0),
		COALESCE(year,0), COALESCE(duration_ms,0), created_at
		FROM source_tracks ORDER BY CASE format WHEN 'flac' THEN 0 WHEN 'wav' THEN 1 WHEN 'm4a' THEN 2 ELSE 3 END, path, id`)
	if err != nil {
		return err
	}
	var src []srcRow
	for rows.Next() {
		var r srcRow
		if err := rows.Scan(&r.id, &r.path, &r.title, &r.artist, &r.album, &r.albumArtist, &r.genre, &r.isrc, &r.mbRec,
			&r.mbRel, &r.cover, &r.trackNo, &r.discNo, &r.year, &r.dur, &r.created); err != nil {
			rows.Close()
			return err
		}
		src = append(src, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	artists := map[string]string{} // id -> display name
	albums := map[string]*mAlbum{}
	tracks := map[string]*mTrack{}
	var trackOrder []string
	fuzzy := map[string][]*mTrack{}

	artistID := func(name string) string {
		id := hid("ar_", normKey(name))
		if _, ok := artists[id]; !ok {
			artists[id] = name
		}
		return id
	}

	// Compilations: tracks without ALBUMARTIST that share a folder + album
	// name but have different primary artists form one "Various Artists" album.
	compArtists := map[string]map[string]bool{}
	compKey := func(r srcRow) string { return filepath.Dir(r.path) + "\x00" + normKey(r.album) }
	for _, r := range src {
		if strings.TrimSpace(r.albumArtist) == "" && strings.TrimSpace(r.album) != "" && r.mbRel == "" {
			k := compKey(r)
			if compArtists[k] == nil {
				compArtists[k] = map[string]bool{}
			}
			p := UnknownArtist
			if c := sp.Split(r.artist); len(c) > 0 {
				p = c[0].Name
			}
			compArtists[k][normKey(p)] = true
		}
	}

	for _, r := range src {
		credits := sp.Split(r.artist)
		if len(credits) == 0 {
			credits = []ArtistCredit{{Name: UnknownArtist, Role: "primary"}}
		}
		primary := credits[0].Name
		var al *mAlbum
		if strings.TrimSpace(r.album) != "" {
			aaName := strings.TrimSpace(r.albumArtist)
			if aaName == "" {
				aaName = primary
			}
			key := fmt.Sprintf("t:%s\x00%s\x00%d", normKey(aaName), normKey(r.album), r.year)
			if strings.TrimSpace(r.albumArtist) == "" && r.mbRel == "" && len(compArtists[compKey(r)]) > 1 {
				aaName = VariousArtists
				key = "va:" + compKey(r) // folder-scoped; year ignored (tracks often differ)
			}
			if r.mbRel != "" {
				key = "mb:" + strings.ToLower(r.mbRel)
			}
			id := hid("al_", key)
			al = albums[id]
			if al == nil {
				al = &mAlbum{id: id, title: strings.TrimSpace(r.album), artistName: aaName, year: r.year,
					mbRel: r.mbRel, genre: r.genre, created: r.created, artists: sp.Split(aaName)}
				albums[id] = al
			}
			if al.cover == "" {
				al.cover = r.cover
			}
			if r.created < al.created {
				al.created = r.created
			}
		}
		albumKey := ""
		if al != nil {
			albumKey = al.id
		}

		var t *mTrack
		var conf float64
		switch {
		case r.mbRec != "":
			t, conf = tracks[hid("tr_", "mb:"+strings.ToLower(r.mbRec)+"\x00"+albumKey)], ConfMBID
			if t == nil {
				t = &mTrack{id: hid("tr_", "mb:"+strings.ToLower(r.mbRec)+"\x00"+albumKey)}
			}
		case r.isrc != "":
			t, conf = tracks[hid("tr_", "isrc:"+strings.ToUpper(r.isrc)+"\x00"+albumKey)], ConfISRC
			if t == nil {
				t = &mTrack{id: hid("tr_", "isrc:"+strings.ToUpper(r.isrc)+"\x00"+albumKey)}
			}
		default:
			fk := albumKey + "\x00" + normKey(r.title) + "\x00" + normKey(primary)
			if albumKey != "" {
				for _, c := range fuzzy[fk] {
					if d := c.dur - r.dur; d >= -2000 && d <= 2000 {
						t, conf = c, ConfFuzzy
						break
					}
				}
			}
			if t == nil {
				t, conf = &mTrack{id: hid("tr_", "src:"+r.id)}, ConfSelf
				fuzzy[fk] = append(fuzzy[fk], t)
			}
		}
		if _, ok := tracks[t.id]; !ok {
			t.albumID, t.title, t.artistName = albumKey, r.title, strings.TrimSpace(r.artist)
			if t.artistName == "" {
				t.artistName = UnknownArtist
			}
			t.genre, t.isrc, t.mbRec = r.genre, r.isrc, r.mbRec
			t.trackNo, t.discNo, t.year, t.dur, t.created = r.trackNo, r.discNo, r.year, r.dur, r.created
			t.artists = credits
			t.links = map[string]float64{}
			tracks[t.id] = t
			trackOrder = append(trackOrder, t.id)
			conf = ConfSelf
		}
		t.links[r.id] = conf
		if r.created < t.created {
			t.created = r.created
		}
	}

	// Register every artist up front so joins never reference a missing row.
	for _, a := range albums {
		for _, c := range a.artists {
			artistID(c.Name)
		}
	}
	for _, t := range tracks {
		for _, c := range t.artists {
			artistID(c.Name)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UnixMilli()
	for _, q := range []string{`DELETE FROM track_links`, `DELETE FROM track_artists`, `DELETE FROM album_artists`} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	exec := func(q string, args ...any) {
		if err == nil {
			_, err = tx.ExecContext(ctx, q, args...)
		}
	}
	for id, name := range artists {
		exec(`INSERT INTO artists(id, name, sort_name, search_text, created_at, updated_at) VALUES (?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET name=excluded.name, search_text=excluded.search_text, updated_at=excluded.updated_at WHERE name<>excluded.name OR search_text<>excluded.search_text`,
			id, name, normKey(name), search.Text(name), now, now)
	}
	for _, a := range albums {
		exec(`INSERT INTO albums(id, title, year, cover_path, mb_release_id, artist_name, genre, search_text, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET title=excluded.title, year=excluded.year,
			cover_path=COALESCE(excluded.cover_path, CASE WHEN albums.cover_source='remote' THEN albums.cover_path END),
			cover_source=CASE WHEN excluded.cover_path IS NULL AND albums.cover_source='remote' THEN 'remote' ELSE 'local' END,
			mb_release_id=COALESCE(excluded.mb_release_id, albums.mb_release_id), artist_name=excluded.artist_name, genre=excluded.genre,
			search_text=excluded.search_text, updated_at=excluded.updated_at`,
			a.id, a.title, nzInt(a.year), nz(a.cover), nz(a.mbRel), a.artistName, nz(normGenre(a.genre)), search.Text(a.title, a.artistName), a.created, now)
		for i, c := range a.artists {
			exec(`INSERT OR IGNORE INTO album_artists(album_id, artist_id, role, position) VALUES (?,?,?,?)`,
				a.id, artistID(c.Name), c.Role, i)
		}
	}
	for _, id := range trackOrder {
		t := tracks[id]
		exec(`INSERT INTO tracks(id, title, album_id, disc_no, track_no, duration_ms, isrc, mb_recording_id,
			artist_name, genre, year, search_text, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET title=excluded.title, album_id=excluded.album_id, disc_no=excluded.disc_no,
			track_no=excluded.track_no, duration_ms=excluded.duration_ms, isrc=excluded.isrc,
			mb_recording_id=COALESCE(excluded.mb_recording_id, tracks.mb_recording_id), artist_name=excluded.artist_name, genre=excluded.genre,
			year=excluded.year, search_text=excluded.search_text, updated_at=excluded.updated_at`,
			t.id, t.title, nz(t.albumID), nzInt(t.discNo), nzInt(t.trackNo), nzInt64(t.dur), nz(t.isrc), nz(t.mbRec),
			t.artistName, nz(normGenre(t.genre)), nzInt(t.year), search.Text(t.title, t.artistName), t.created, now)
		for i, c := range t.artists {
			exec(`INSERT OR IGNORE INTO track_artists(track_id, artist_id, role, position) VALUES (?,?,?,?)`,
				t.id, artistID(c.Name), c.Role, i)
		}
		for sid, conf := range t.links {
			exec(`INSERT INTO track_links(track_id, source_track_id, confidence) VALUES (?,?,?)`, t.id, sid, conf)
		}
	}
	for _, q := range []string{
		`DELETE FROM tracks WHERE id NOT IN (SELECT track_id FROM track_links)`,
		`DELETE FROM albums WHERE id NOT IN (SELECT album_id FROM tracks WHERE album_id IS NOT NULL)`,
		`DELETE FROM artists WHERE id NOT IN (SELECT artist_id FROM track_artists UNION SELECT artist_id FROM album_artists)`,
		`UPDATE albums SET track_count=(SELECT count(*) FROM tracks WHERE album_id=albums.id),
			duration_ms=(SELECT COALESCE(sum(duration_ms),0) FROM tracks WHERE album_id=albums.id)`,
		`UPDATE artists SET track_count=(SELECT count(DISTINCT track_id) FROM track_artists WHERE artist_id=artists.id),
			album_count=(SELECT count(DISTINCT album_id) FROM (
				SELECT album_id FROM album_artists WHERE artist_id=artists.id
				UNION SELECT t.album_id FROM track_artists ta JOIN tracks t ON t.id=ta.track_id
				WHERE ta.artist_id=artists.id AND t.album_id IS NOT NULL))`,
	} {
		exec(q)
	}
	if err != nil {
		return err
	}
	if err := s.applyOverrides(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func nzInt(n int) any     { return ni(n) }
func nzInt64(n int64) any { return ni64(n) }
