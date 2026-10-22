package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
	"github.com/my-name-is-alan/yimeng/core/internal/search"
)

// List envelope (D45). Every library list (artists/albums/tracks, …) returns
//
//	{items, total, limit, offset, cursor, next_cursor, updated_at}
//
// with a strong ETag (If-None-Match → 304). cursor is opaque; pass
// ?cursor=<next_cursor> to continue (offset still works).

type cursorState struct {
	O int    `json:"o"`
	F string `json:"f"` // fingerprint of the query, so a cursor can't cross lists
}

func listFingerprint(r *http.Request) string {
	q := r.URL.Query()
	q.Del("cursor")
	q.Del("offset")
	q.Del("limit")
	h := sha256.Sum256([]byte(r.URL.Path + "?" + q.Encode()))
	return hex.EncodeToString(h[:4])
}

func encodeCursor(o int, fp string) string {
	b, _ := json.Marshal(cursorState{O: o, F: fp})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(c, fp string) (int, bool) {
	b, err := base64.RawURLEncoding.DecodeString(c)
	var st cursorState
	if err != nil || json.Unmarshal(b, &st) != nil || st.O < 0 || st.F != fp {
		return 0, false
	}
	return st.O, true
}

// writeList encodes a library page as the list envelope with ETag.
func (d Deps) writeList(w http.ResponseWriter, r *http.Request, page any, limit, offset, total int) {
	b, err := json.Marshal(page)
	if err != nil {
		d.internal(w, "encode list", err)
		return
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(b, &m) != nil {
		writeJSON(w, http.StatusOK, page)
		return
	}
	fp := listFingerprint(r)
	m["cursor"], _ = json.Marshal(encodeCursor(offset, fp))
	if offset+limit < total {
		m["next_cursor"], _ = json.Marshal(encodeCursor(offset+limit, fp))
	} else {
		m["next_cursor"] = json.RawMessage("null")
	}
	if t := d.libraryUpdatedAt(r.Context()); t.IsZero() {
		m["updated_at"] = json.RawMessage("null")
	} else {
		m["updated_at"], _ = json.Marshal(t)
	}
	out, _ := json.Marshal(m)
	writeETagged(w, r, out)
}

// writeETagged writes JSON with a strong ETag derived from the body.
func writeETagged(w http.ResponseWriter, r *http.Request, body []byte) {
	h := sha256.Sum256(body)
	et := `"` + hex.EncodeToString(h[:12]) + `"`
	w.Header().Set("ETag", et)
	w.Header().Set("Cache-Control", "private, no-cache")
	for _, v := range strings.Split(r.Header.Get("If-None-Match"), ",") {
		if strings.TrimSpace(v) == et {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(body)
}

// libraryUpdatedAt is the last catalog change (scan or edit).
func (d Deps) libraryUpdatedAt(ctx context.Context) time.Time {
	if d.Library == nil {
		return time.Time{}
	}
	if u, ok := d.Library.(interface {
		UpdatedAt(context.Context) time.Time
	}); ok {
		return u.UpdatedAt(ctx)
	}
	return time.Time{}
}

func applyCursor(r *http.Request, p *library.ListParams) bool {
	c := r.URL.Query().Get("cursor")
	if c == "" {
		return true
	}
	o, ok := decodeCursor(c, listFingerprint(r))
	p.Offset = o
	return ok
}

func (d Deps) decorateAlbums(ctx context.Context, as []library.Album) {
	dev, _ := DeviceFrom(ctx)
	for i := range as {
		a := &as[i]
		a.SortLetter = search.SortLetter(a.Title)
		a.Name = a.Title
		if len(a.Artists) > 0 {
			ar := a.Artists[0]
			a.Artist = &ar
		} else if a.ArtistName != "" {
			a.Artist = &library.ArtistRef{Name: a.ArtistName}
		}
		if a.HasCover {
			if src, err := d.Library.AlbumCover(ctx, a.ID); err == nil {
				a.Cover = d.coverRef(dev.ID, a.ID, src)
			}
		}
	}
}

func (d Deps) decorateArtists(ctx context.Context, as []library.Artist) {
	dev, _ := DeviceFrom(ctx)
	for i := range as {
		a := &as[i]
		a.SortLetter = search.SortLetter(a.Name)
		if d.Auth != nil {
			p := "/api/v1/artists/" + a.ID + "/image"
			if a.ImageUpdatedAt > 0 {
				p += "?v=" + strconv.FormatInt(a.ImageUpdatedAt, 10)
			}
			a.ImageURL = d.Auth.SignURL(p, dev.ID, 0)
		}
		if a.ImageUpdatedAt > 0 {
			a.Cover = &library.CoverRef{URL: a.ImageURL}
		} else if pg, err := d.Library.ListAlbums(ctx, library.ListParams{ArtistID: a.ID, Limit: 1, Sort: "year", Desc: true}); err == nil && len(pg.Items) > 0 && pg.Items[0].HasCover {
			if src, err := d.Library.AlbumCover(ctx, pg.Items[0].ID); err == nil {
				a.Cover = d.coverRef(dev.ID, pg.Items[0].ID, src)
			}
		}
	}
}

// pageSlice pages an in-memory list with ?limit= (default all, max 500) and
// ?cursor= / ?offset=. It returns the page, total, offset and next cursor.
func pageSlice[T any](r *http.Request, all []T) (page []T, total, offset int, next any) {
	q := r.URL.Query()
	total = len(all)
	limit := total
	if v, err := strconv.Atoi(q.Get("limit")); err == nil && v > 0 {
		limit = min(v, 500)
	}
	fp := listFingerprint(r)
	if c := q.Get("cursor"); c != "" {
		offset, _ = decodeCursor(c, fp)
	} else if v, err := strconv.Atoi(q.Get("offset")); err == nil && v > 0 {
		offset = v
	}
	offset = min(offset, total)
	end := min(offset+limit, total)
	page = all[offset:end]
	if end < total {
		next = encodeCursor(end, fp)
	}
	return page, total, offset, next
}
