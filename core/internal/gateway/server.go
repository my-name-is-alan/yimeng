package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
)

// LibraryUseCases is implemented by app.Library.
type LibraryUseCases interface {
	StartScan(ctx context.Context, req library.StartRequest) (library.Job, error)
	ScanStatus(ctx context.Context, id string) (library.Job, error)
	LatestScan(ctx context.Context) (library.Job, error)
}

type Deps struct {
	Library LibraryUseCases
}

func NewRouter(d Deps) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "yimeng-core"})
	})
	if d.Library != nil {
		mux.HandleFunc("POST /api/library/scan", d.startScan)
		mux.HandleFunc("GET /api/library/scan", d.latestScan)
		mux.HandleFunc("GET /api/library/scan/{id}", d.scanStatus)
	}
	return mux
}

type startScanBody struct {
	Root string `json:"root,omitempty"`
	Full bool   `json:"full,omitempty"`
}

func (d Deps) startScan(w http.ResponseWriter, r *http.Request) {
	var body startScanBody
	if r.Body != nil {
		dec := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil && !errors.Is(err, io.EOF) {
			writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
	}
	job, err := d.Library.StartScan(r.Context(), library.StartRequest{Root: body.Root, Full: body.Full})
	switch {
	case errors.Is(err, library.ErrScanRunning):
		writeJSON(w, http.StatusConflict, map[string]any{"error": "scan_running", "job": job})
	case errors.Is(err, library.ErrUnknownRoot):
		writeErr(w, http.StatusBadRequest, "unknown_root", "root must be inside a configured library root")
	case err != nil:
		writeErr(w, http.StatusInternalServerError, "internal", "failed to start scan")
	default:
		w.Header().Set("Location", "/api/library/scan/"+job.ID)
		writeJSON(w, http.StatusAccepted, job)
	}
}

func (d Deps) scanStatus(w http.ResponseWriter, r *http.Request) {
	d.writeJob(w, func() (library.Job, error) { return d.Library.ScanStatus(r.Context(), r.PathValue("id")) })
}

func (d Deps) latestScan(w http.ResponseWriter, r *http.Request) {
	d.writeJob(w, func() (library.Job, error) { return d.Library.LatestScan(r.Context()) })
}

func (d Deps) writeJob(w http.ResponseWriter, f func() (library.Job, error)) {
	job, err := f()
	switch {
	case errors.Is(err, library.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not_found", "scan job not found")
	case err != nil:
		writeErr(w, http.StatusInternalServerError, "internal", "failed to load scan job")
	default:
		writeJSON(w, http.StatusOK, job)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, kind, msg string) {
	writeJSON(w, code, map[string]string{"error": kind, "message": msg})
}
