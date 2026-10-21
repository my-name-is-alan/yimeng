package gateway

import (
	"errors"
	"net/http"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
)

type startScanBody struct {
	Root string `json:"root,omitempty"`
	Full bool   `json:"full,omitempty"`
}

func (d Deps) startScan(w http.ResponseWriter, r *http.Request) {
	var body startScanBody
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	job, err := d.Library.StartScan(r.Context(), library.StartRequest{Root: body.Root, Full: body.Full})
	switch {
	case errors.Is(err, library.ErrScanRunning):
		writeJSON(w, http.StatusConflict, map[string]any{"error": "scan_running", "job": job})
	case errors.Is(err, library.ErrUnknownRoot):
		writeErr(w, http.StatusBadRequest, "unknown_root", "root must be inside a configured library root")
	case err != nil:
		d.internal(w, "start scan", err)
	default:
		w.Header().Set("Location", "/api/library/scan/"+job.ID)
		writeJSON(w, http.StatusAccepted, job)
	}
}

func (d Deps) scanStatus(w http.ResponseWriter, r *http.Request) {
	d.writeOne(w, "scan job", func() (any, error) { return d.Library.ScanStatus(r.Context(), r.PathValue("id")) })
}

func (d Deps) latestScan(w http.ResponseWriter, r *http.Request) {
	d.writeOne(w, "scan job", func() (any, error) { return d.Library.LatestScan(r.Context()) })
}

func (d Deps) writeOne(w http.ResponseWriter, what string, f func() (any, error)) {
	v, err := f()
	switch {
	case errors.Is(err, library.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not_found", what+" not found")
	case errors.Is(err, library.ErrBadSort):
		writeErr(w, http.StatusBadRequest, "invalid_sort", err.Error())
	case err != nil:
		d.internal(w, "load "+what, err)
	default:
		writeJSON(w, http.StatusOK, v)
	}
}
