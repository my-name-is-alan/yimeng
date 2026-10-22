package userdata

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/owner"
)

// Listening report (D55): computed only from the caller's own plays.

type ReportEntry struct {
	ID         string `json:"id,omitempty"`
	Name       string `json:"name"`
	ArtistName string `json:"artist_name,omitempty"`
	Plays      int    `json:"plays"`
	ListenedMs int64  `json:"listened_ms"`
}

type Report struct {
	Period     string        `json:"period"` // month | year
	Year       int           `json:"year"`
	Month      int           `json:"month,omitempty"`
	TZ         string        `json:"tz"`
	From       time.Time     `json:"from"`
	To         time.Time     `json:"to"`
	Plays      int           `json:"plays"`
	ListenedMs int64         `json:"listened_ms"`
	Tracks     int           `json:"unique_tracks"`
	Artists    int           `json:"unique_artists"`
	Albums     int           `json:"unique_albums"`
	ActiveDays int           `json:"active_days"`
	TopTracks  []ReportEntry `json:"top_tracks"`
	TopArtists []ReportEntry `json:"top_artists"`
	TopAlbums  []ReportEntry `json:"top_albums"`
	ByHour     [24]int       `json:"by_hour"`
	ByWeekday  [7]int        `json:"by_weekday"` // 0 = Monday
	ByDay      []int         `json:"by_day"`     // month: per day of month; year: per month
}

// ListeningReport aggregates the caller's plays for one month (month 1..12)
// or one year (month 0) in loc.
func (s *Store) ListeningReport(ctx context.Context, year, month int, loc *time.Location, top int) (Report, error) {
	if loc == nil {
		loc = time.UTC
	}
	if year < 1970 || year > 9999 || month < 0 || month > 12 {
		return Report{}, fmt.Errorf("%w: invalid year/month", ErrBad)
	}
	if top <= 0 || top > 50 {
		top = 10
	}
	r := Report{Period: "year", Year: year, Month: month, TZ: loc.String()}
	from := time.Date(year, 1, 1, 0, 0, 0, 0, loc)
	to := from.AddDate(1, 0, 0)
	r.ByDay = make([]int, 12)
	if month > 0 {
		r.Period = "month"
		from = time.Date(year, time.Month(month), 1, 0, 0, 0, 0, loc)
		to = from.AddDate(0, 1, 0)
		r.ByDay = make([]int, to.AddDate(0, 0, -1).Day())
	}
	r.From, r.To = from.UTC(), to.UTC()
	rows, err := s.DB.QueryContext(ctx, `SELECT p.played_at, COALESCE(p.duration_ms, 0), p.track_id, COALESCE(t.title,''), COALESCE(t.artist_name,''),
		COALESCE(t.album_id,''), COALESCE(a.title,''), COALESCE(a.artist_name,'')
		FROM plays p LEFT JOIN tracks t ON t.id=p.track_id LEFT JOIN albums a ON a.id=t.album_id
		WHERE p.user_id=? AND p.played_at>=? AND p.played_at<?`, owner.From(ctx).ID, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return r, err
	}
	defer rows.Close()
	tracks, artists, albums := map[string]*ReportEntry{}, map[string]*ReportEntry{}, map[string]*ReportEntry{}
	days := map[string]bool{}
	bump := func(m map[string]*ReportEntry, key string, e ReportEntry, ms int64) {
		if key == "" {
			return
		}
		x := m[key]
		if x == nil {
			x = &e
			m[key] = x
		}
		x.Plays++
		x.ListenedMs += ms
	}
	for rows.Next() {
		var at, ms int64
		var tid, title, artist, aid, atitle, aartist string
		if err := rows.Scan(&at, &ms, &tid, &title, &artist, &aid, &atitle, &aartist); err != nil {
			return r, err
		}
		t := time.UnixMilli(at).In(loc)
		r.Plays++
		r.ListenedMs += ms
		r.ByHour[t.Hour()]++
		r.ByWeekday[(int(t.Weekday())+6)%7]++
		if month > 0 {
			r.ByDay[t.Day()-1]++
		} else {
			r.ByDay[int(t.Month())-1]++
		}
		days[t.Format("2006-01-02")] = true
		bump(tracks, tid, ReportEntry{ID: tid, Name: title, ArtistName: artist}, ms)
		bump(artists, artist, ReportEntry{Name: artist}, ms)
		if aid != "" {
			if aartist == "" {
				aartist = artist
			}
			bump(albums, aid, ReportEntry{ID: aid, Name: atitle, ArtistName: aartist}, ms)
		}
	}
	if err := rows.Err(); err != nil {
		return r, err
	}
	r.Tracks, r.Artists, r.Albums, r.ActiveDays = len(tracks), len(artists), len(albums), len(days)
	r.TopTracks, r.TopArtists, r.TopAlbums = topN(tracks, top), topN(artists, top), topN(albums, top)
	return r, nil
}

func topN(m map[string]*ReportEntry, n int) []ReportEntry {
	out := make([]ReportEntry, 0, len(m))
	for _, e := range m {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Plays != out[j].Plays {
			return out[i].Plays > out[j].Plays
		}
		if out[i].ListenedMs != out[j].ListenedMs {
			return out[i].ListenedMs > out[j].ListenedMs
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}
