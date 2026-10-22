package gateway

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/backup"
)

// GET /api/v1/security/alerts — admins see all; others their account's.
func (d Deps) listAlerts(w http.ResponseWriter, r *http.Request) {
	me, _ := DeviceFrom(r.Context())
	if !me.Admin && me.UserID == "" {
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{}, "unread": 0})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, unread, err := d.Alerts.List(r.Context(), me.UserID, me.Admin, limit)
	if err != nil {
		d.internal(w, "alerts", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "unread": unread})
}

// POST /api/v1/security/alerts/read {ids?} — empty marks all visible read.
func (d Deps) readAlerts(w http.ResponseWriter, r *http.Request) {
	me, _ := DeviceFrom(r.Context())
	var b struct {
		IDs []int64 `json:"ids"`
	}
	if err := decodeBody(r, &b); err != nil || len(b.IDs) > 500 {
		writeErr(w, http.StatusBadRequest, "invalid_body", "ids: up to 500 alert ids")
		return
	}
	if !me.Admin && me.UserID == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := d.Alerts.MarkRead(r.Context(), me.UserID, me.Admin, b.IDs); err != nil {
		d.internal(w, "alerts", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/v1/system/backups/policy
func (d Deps) getBackupPolicy(w http.ResponseWriter, r *http.Request) {
	p := d.Backup.Policy()
	writeJSON(w, http.StatusOK, map[string]any{"enabled": p.Enabled, "keep": p.Keep,
		"interval": d.BackupInterval.String(), "last_auto": nullTime(d.Backup.LastAuto()), "includes_key": false})
}

// PUT /api/v1/system/backups/policy {enabled?, keep?}
func (d Deps) putBackupPolicy(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Enabled *bool `json:"enabled"`
		Keep    *int  `json:"keep"`
	}
	if err := decodeBody(r, &b); err != nil || (b.Keep != nil && (*b.Keep < 1 || *b.Keep > 60)) {
		writeErr(w, http.StatusBadRequest, "invalid_body", "keep must be 1..60")
		return
	}
	p := d.Backup.Policy()
	if b.Enabled != nil {
		p.Enabled = *b.Enabled
	}
	if b.Keep != nil {
		p.Keep = *b.Keep
	}
	if _, err := d.Backup.SavePolicy(r.Context(), p); err != nil {
		d.internal(w, "backup policy", err)
		return
	}
	d.getBackupPolicy(w, r)
}

// POST /api/v1/system/backups/{name}/restore {confirm: name} — stage a stored
// backup; the caller must repeat the backup name to confirm.
func (d Deps) restoreStored(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var b struct {
		Confirm string `json:"confirm"`
	}
	if err := decodeBody(r, &b); err != nil || b.Confirm != name {
		writeErr(w, http.StatusBadRequest, "confirmation_required", "repeat the backup name in confirm to restore it")
		return
	}
	m, err := d.Backup.StageFile(r.Context(), name, d.RestoreMaxBytes, d.SchemaVersion)
	switch {
	case errors.Is(err, backup.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not_found", "backup not found")
	case errors.Is(err, backup.ErrInvalid):
		writeErr(w, http.StatusBadRequest, "invalid_backup", err.Error())
	case err != nil:
		d.internal(w, "stage restore", err)
	default:
		me, _ := DeviceFrom(r.Context())
		d.Log.Warn("restore staged from stored backup", "name", name, "by", me.ID)
		writeJSON(w, http.StatusAccepted, map[string]any{"restart_required": true, "manifest": m,
			"message": "restore staged; restart Core to apply (the current database is kept as .pre-restore-*)"})
	}
}

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	u := t.UTC()
	return &u
}
