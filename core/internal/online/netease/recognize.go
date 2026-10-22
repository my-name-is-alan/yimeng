package netease

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/online"
)

// Match is one 听歌识曲 hit from NetEase.
type Match struct {
	SongID   int64
	Title    string
	Artist   string
	Album    string
	CoverURL string
	OffsetMs int
	Score    float64
}

// MatchFingerprint sends a shazam_v2 fingerprint (raw ciphertext, not base64)
// to NetEase /api/music/audio/match. The official extension posts a plain
// form to interface.music.163.com (not eapi) with credentials:include.
func (p *Plugin) MatchFingerprint(ctx context.Context, fp []byte, durationSec float64) ([]Match, error) {
	if _, err := p.Account(ctx); err != nil {
		return nil, err
	}
	if !onlineOK(durationSec) {
		return nil, fmt.Errorf("%w: duration", online.ErrUnavailable)
	}
	form := url.Values{
		"sessionId":     {fmt.Sprintf("%d", time.Now().UnixNano())},
		"algorithmCode": {"shazam_v2"},
		"duration":      {fmt.Sprintf("%g", durationSec)},
		"rawdata":       {base64.StdEncoding.EncodeToString(fp)},
		"times":         {"1"},
		"decrypt":       {"1"},
	}
	body, err := p.api.post(ctx,
		p.api.opts.EapiDomain+"/api/music/audio/match",
		form,
		p.api.c.CookieHeader(nil),
		uaWeb,
		p.api.opts.Domain,
	)
	if err != nil {
		return nil, err
	}
	var r struct {
		Code int `json:"code"`
		Data struct {
			Result []struct {
				Song struct {
					ID      int64  `json:"id"`
					Name    string `json:"name"`
					Artists []struct {
						Name string `json:"name"`
					} `json:"artists"`
					Album struct {
						Name   string `json:"name"`
						PicURL string `json:"picUrl"`
					} `json:"album"`
				} `json:"song"`
				Offset float64 `json:"offset"` // seconds
				Score  float64 `json:"score"`
			} `json:"result"`
		} `json:"data"`
		// Some responses nest result at top level.
		Result []struct {
			Song struct {
				ID      int64  `json:"id"`
				Name    string `json:"name"`
				Artists []struct {
					Name string `json:"name"`
				} `json:"artists"`
				Album struct {
					Name   string `json:"name"`
					PicURL string `json:"picUrl"`
				} `json:"album"`
			} `json:"song"`
			Offset float64 `json:"offset"`
			Score  float64 `json:"score"`
		} `json:"result"`
	}
	if err := decode(body, &r); err != nil {
		return nil, fmt.Errorf("match decode: %w (body %s)", err, trimBody(body))
	}
	if r.Code != 200 {
		return nil, fmt.Errorf("netease match code %d (%s)", r.Code, trimBody(body))
	}
	rows := r.Data.Result
	if len(rows) == 0 {
		rows = r.Result
	}
	out := make([]Match, 0, len(rows))
	for _, it := range rows {
		if it.Song.ID == 0 {
			continue
		}
		artists := ""
		for i, a := range it.Song.Artists {
			if i > 0 {
				artists += " / "
			}
			artists += a.Name
		}
		out = append(out, Match{
			SongID: it.Song.ID, Title: it.Song.Name, Artist: artists,
			Album: it.Song.Album.Name, CoverURL: it.Song.Album.PicURL,
			OffsetMs: int(it.Offset * 1000), Score: it.Score,
		})
	}
	return out, nil
}

func onlineOK(d float64) bool { return d > 0 && d <= 60 }

func trimBody(b []byte) string {
	if len(b) > 300 {
		return string(b[:300]) + "…"
	}
	return string(b)
}
