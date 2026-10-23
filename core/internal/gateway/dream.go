package gateway

import (
	"net/http"
	"strings"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
)

// GET /api/v1/tracks/{id}/dream — reduced (D47): no audio analysis at all.
//
//	{colors[3]|null, tags[], bpm: null|number, source: {...}}
//
// colors come from the cover; tags from the file's genre tags (and platform
// tags when a platform provides them); bpm only if a tag/platform has it.
func (d Deps) trackDream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	out := map[string]any{"track_id": id, "colors": nil, "blurhash": nil, "tags": []string{}, "bpm": nil}
	if t, err := d.Library.GetTrack(r.Context(), id); err == nil {
		if t.Genre != "" {
			var tags []string
			for _, g := range strings.FieldsFunc(t.Genre, func(r rune) bool { return r == ';' || r == '/' || r == ',' || r == '、' }) {
				if g = strings.TrimSpace(g); g != "" {
					tags = append(tags, g)
				}
			}
			out["tags"] = tags
		}
		if t.CoverPath != "" && d.Covers != nil {
			src := t.CoverPath
			if strings.HasPrefix(src, "http") {
				if lp, err := d.Covers.Remote(r.Context(), src); err == nil {
					src = lp
				} else {
					src = ""
				}
			}
			if src != "" {
				if m, err := d.Covers.Meta(src); err == nil {
					out["colors"], out["blurhash"] = m.Colors, m.Blurhash
				}
			}
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	if d.onlineFor(r.Context()) != nil && library.IsExternalID(id) {
		t, err := d.onlineFor(r.Context()).Track(r.Context(), id)
		if err != nil {
			d.platformErr(w, "track", err)
			return
		}
		if t.CoverURL != "" && d.Covers != nil {
			if lp, err := d.Covers.Remote(r.Context(), t.CoverURL); err == nil {
				if m, err := d.Covers.Meta(lp); err == nil {
					out["colors"], out["blurhash"] = m.Colors, m.Blurhash
				}
			}
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	writeErr(w, http.StatusNotFound, "not_found", "track not found")
}
