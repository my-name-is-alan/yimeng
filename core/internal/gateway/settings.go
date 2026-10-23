package gateway

import (
	"errors"
	"net/http"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
)

func (d Deps) getSplit(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, d.Library.SplitConfig())
}

// putSplit replaces the artist split policy and re-merges the library.
func (d Deps) putSplit(w http.ResponseWriter, r *http.Request) {
	var c library.SplitConfig
	if err := decodeBody(r, &c); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	got, err := d.Library.SetSplitConfig(r.Context(), c)
	switch {
	case errors.Is(err, library.ErrInvalidSettings):
		writeErr(w, http.StatusBadRequest, "invalid_settings", err.Error())
	case err != nil:
		d.internal(w, "save settings", err)
	default:
		writeJSON(w, http.StatusOK, got)
	}
}
