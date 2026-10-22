package gateway

import (
	"errors"
	"fmt"
	"github.com/my-name-is-alan/yimeng/core/internal/system"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/backup"
)

// adminOnly wraps handlers for system/admin APIs.
func (d Deps) adminOnly(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if dev, ok := DeviceFrom(r.Context()); !ok || !dev.Admin {
			writeErr(w, http.StatusForbidden, "forbidden", "admin device required")
			return
		}
		h(w, r)
	}
}

func (d Deps) listBackups(w http.ResponseWriter, r *http.Request) {
	list, err := d.Backup.List()
	if err != nil {
		d.internal(w, "list backups", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list, "dir_configured": true})
}

func (d Deps) createBackup(w http.ResponseWriter, r *http.Request) {
	in, err := d.Backup.Create(r.Context(), "manual")
	if err != nil {
		d.internal(w, "create backup", err)
		return
	}
	writeJSON(w, http.StatusCreated, in)
}

func (d Deps) downloadBackup(w http.ResponseWriter, r *http.Request) {
	p, err := d.Backup.Path(r.PathValue("name"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "not_found", "backup not found")
		return
	}
	f, err := os.Open(p)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not_found", "backup not found")
		return
	}
	defer f.Close()
	st, _ := f.Stat()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+r.PathValue("name")+`"`)
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, "", st.ModTime(), f)
}

func (d Deps) deleteBackup(w http.ResponseWriter, r *http.Request) {
	if err := d.Backup.Delete(r.PathValue("name")); err != nil {
		writeErr(w, http.StatusNotFound, "not_found", "backup not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// exportBackup streams a fresh snapshot. include_key=true adds the vault master
// key (the archive then decrypts stored credentials: keep it safe).
func (d Deps) exportBackup(w http.ResponseWriter, r *http.Request) {
	withKey, _ := strconv.ParseBool(r.URL.Query().Get("include_key"))
	name := "yimeng-export-" + time.Now().UTC().Format("20060102-150405") + backup.Ext
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	if _, err := d.Backup.Snapshot(r.Context(), w, "export", withKey); err != nil {
		d.Log.Error("export backup", "err", err)
		panic(http.ErrAbortHandler) // body already started
	}
}

func (d Deps) restoreBackup(w http.ResponseWriter, r *http.Request) {
	m, err := d.Backup.Stage(r.Context(), r.Body, d.RestoreMaxBytes, d.SchemaVersion)
	if errors.Is(err, backup.ErrInvalid) {
		writeErr(w, http.StatusBadRequest, "invalid_backup", err.Error())
		return
	}
	if err != nil {
		d.internal(w, "stage restore", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"restart_required": true, "manifest": m,
		"message": fmt.Sprintf("restore staged (schema %d); restart Core to apply", m.SchemaVersion)})
}

func (d Deps) getSystem(w http.ResponseWriter, r *http.Request) {
	if d.System == nil {
		writeErr(w, http.StatusNotImplemented, "not_configured", "system info unavailable")
		return
	}
	in, err := d.System.Collect(r.Context())
	if err != nil {
		d.internal(w, "system", err)
		return
	}
	if me, _ := DeviceFrom(r.Context()); !me.Admin {
		redactSystem(&in) // D49: no host paths/ops detail for non-admins
	}
	writeJSON(w, http.StatusOK, in)
}

// redactSystem strips host paths, errors and operational detail.
func redactSystem(in *system.Info) {
	for i := range in.Roots {
		in.Roots[i].Path, in.Roots[i].Error, in.Roots[i].FreeBytes = "", "", 0
	}
	in.Scan = nil
	in.Diagnostics = []system.Diagnostic{}
	in.Database.SizeBytes, in.Database.WALBytes, in.Database.FreeBytes = 0, 0, 0
	in.Backups.Count, in.Backups.Last = 0, nil
	in.Devices.Paired, in.Devices.Active = 0, 0
	in.GoVersion = ""
}
