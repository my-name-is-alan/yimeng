package library

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"
)

const UnknownArtist = "Unknown Artist"

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

var (
	featRe = regexp.MustCompile(`(?i)\s*[\(\[（]?\s*\b(?:feat\.?|ft\.?|featuring)\s+`)
	sepRe  = regexp.MustCompile(`\s*(?:;|；|、|／|\s/\s|/)\s*`)
	// Names that contain a separator character but are a single artist.
	unsplittable = map[string]bool{"ac/dc": true, "his name is alive/": true}
)

// SplitArtists splits a tag value into individual artists. Separators: ; ； 、 / ／
// and "feat." / "ft." / "featuring" (the latter mark featured artists).
// "&" and "," are NOT split (too many real names contain them).
func SplitArtists(s string) []ArtistCredit {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	main, feat := s, ""
	if loc := featRe.FindStringIndex(s); loc != nil && loc[0] > 0 {
		main, feat = s[:loc[0]], s[loc[1]:]
		feat = strings.TrimRight(strings.TrimSpace(feat), ")]）")
	}
	var out []ArtistCredit
	seen := map[string]bool{}
	add := func(part, role string) {
		for _, n := range splitSep(part) {
			k := normKey(n)
			if k == "" || seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, ArtistCredit{Name: n, Role: role})
		}
	}
	add(main, "primary")
	add(feat, "feat")
	return out
}

func splitSep(s string) []string {
	s = strings.TrimSpace(s)
	if unsplittable[strings.ToLower(s)] {
		return []string{s}
	}
	var out []string
	for _, p := range sepRe.Split(s, -1) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
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
	id, title, artist, album, albumArtist, genre, isrc, mbRec, mbRel, cover string
	trackNo, discNo, year                                                   int
	dur, created                                                            int64
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
	rows, err := s.db.QueryContext(ctx, `SELECT id, COALESCE(title,''), COALESCE(artist,''), COALESCE(album,''),
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
		if err := rows.Scan(&r.id, &r.title, &r.artist, &r.album, &r.albumArtist, &r.genre, &r.isrc, &r.mbRec,
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

	for _, r := range src {
		credits := SplitArtists(r.artist)
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
			if r.mbRel != "" {
				key = "mb:" + strings.ToLower(r.mbRel)
			}
			id := hid("al_", key)
			al = albums[id]
			if al == nil {
				al = &mAlbum{id: id, title: strings.TrimSpace(r.album), artistName: aaName, year: r.year,
					mbRel: r.mbRel, genre: r.genre, created: r.created, artists: SplitArtists(aaName)}
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
		exec(`INSERT INTO artists(id, name, sort_name, created_at, updated_at) VALUES (?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET name=excluded.name, updated_at=excluded.updated_at WHERE name<>excluded.name`,
			id, name, normKey(name), now, now)
	}
	for _, a := range albums {
		exec(`INSERT INTO albums(id, title, year, cover_path, mb_release_id, artist_name, genre, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET title=excluded.title, year=excluded.year, cover_path=excluded.cover_path,
			mb_release_id=excluded.mb_release_id, artist_name=excluded.artist_name, genre=excluded.genre,
			updated_at=excluded.updated_at`,
			a.id, a.title, nzInt(a.year), nz(a.cover), nz(a.mbRel), a.artistName, nz(a.genre), a.created, now)
		for i, c := range a.artists {
			exec(`INSERT OR IGNORE INTO album_artists(album_id, artist_id, role, position) VALUES (?,?,?,?)`,
				a.id, artistID(c.Name), c.Role, i)
		}
	}
	for _, id := range trackOrder {
		t := tracks[id]
		exec(`INSERT INTO tracks(id, title, album_id, disc_no, track_no, duration_ms, isrc, mb_recording_id,
			artist_name, genre, year, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET title=excluded.title, album_id=excluded.album_id, disc_no=excluded.disc_no,
			track_no=excluded.track_no, duration_ms=excluded.duration_ms, isrc=excluded.isrc,
			mb_recording_id=excluded.mb_recording_id, artist_name=excluded.artist_name, genre=excluded.genre,
			year=excluded.year, updated_at=excluded.updated_at`,
			t.id, t.title, nz(t.albumID), nzInt(t.discNo), nzInt(t.trackNo), nzInt64(t.dur), nz(t.isrc), nz(t.mbRec),
			t.artistName, nz(t.genre), nzInt(t.year), t.created, now)
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
	return tx.Commit()
}

func nzInt(n int) any     { return ni(n) }
func nzInt64(n int64) any { return ni64(n) }
