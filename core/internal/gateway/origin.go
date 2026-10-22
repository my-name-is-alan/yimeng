package gateway

import (
	"errors"
	"net/http"
	"sort"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
)

// Metadata editing, matching, cover replace and lyrics binding apply only to
// local library items (origin "local"). Items from online StreamSources are
// read-only here; see docs/decisions.md.

const codeNotLocal = "not_local_source"

// nonLocal splits ids into missing and non-local (id -> origin).
func (d Deps) nonLocal(r *http.Request, kind string, ids []string) (missing []string, foreign map[string]string, err error) {
	org, err := d.Admin.Origins(r.Context(), kind, ids)
	if err != nil {
		return nil, nil, err
	}
	foreign = map[string]string{}
	for _, id := range ids {
		o, ok := org[id]
		switch {
		case !ok:
			missing = append(missing, id)
		case o != library.OriginLocal:
			foreign[id] = o
		}
	}
	return missing, foreign, nil
}

// requireLocal writes 404/409 and returns false unless every id is a local item.
func (d Deps) requireLocal(w http.ResponseWriter, r *http.Request, kind string, ids ...string) bool {
	missing, foreign, err := d.nonLocal(r, kind, ids)
	if err != nil {
		if errors.Is(err, library.ErrBadInput) {
			writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		} else {
			d.internal(w, "origin lookup", err)
		}
		return false
	}
	if len(foreign) > 0 {
		ids := make([]string, 0, len(foreign))
		for id := range foreign {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":   codeNotLocal,
			"message": "only local library items can be edited or matched; " + kind + " comes from an online source",
			"type":    kind, "ids": ids, "origins": foreign,
		})
		return false
	}
	if len(missing) > 0 {
		writeErr(w, http.StatusNotFound, "not_found", kind+" not found")
		return false
	}
	return true
}

// localOnly guards a handler whose {id} path value is of kind. An empty kind
// takes it from the {type} path value (overrides).
func (d Deps) localOnly(kind string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		k := kind
		if k == "" {
			k = r.PathValue("type")
		}
		if d.requireLocal(w, r, k, r.PathValue("id")) {
			h(w, r)
		}
	}
}
