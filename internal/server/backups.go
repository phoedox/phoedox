package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/backup"
	"github.com/phoedox/phoedox/internal/core"
)

// sysAdmin allows only system admins with full rights (no tokens with a
// narrower scope, no AI) and only when backups exist on this instance.
func (a *api) sysAdmin(w http.ResponseWriter, r *http.Request) bool {
	act := actorOf(r)
	if a.backups == nil {
		writeErr(w, core.ErrNotFound)
		return false
	}
	if !act.User.IsAdmin || act.Via == "mcp" || act.Restricted() {
		writeErr(w, core.ErrForbidden)
		return false
	}
	return true
}

func backupErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, backup.ErrBusy):
		writeErr(w, core.Invalid("validation.backup_busy", nil, "a backup or restore is running"))
	case errors.Is(err, backup.ErrNewer):
		writeErr(w, core.Invalid("validation.backup_newer", nil, "the backup was made by a newer Phoedox version"))
	case errors.Is(err, backup.ErrNotFound):
		writeErr(w, core.ErrNotFound)
	default:
		writeErr(w, core.Invalid("validation.backup_failed", map[string]any{"reason": err.Error()}, "backup failed: %v", err))
	}
}

func (a *api) listBackups(w http.ResponseWriter, r *http.Request) {
	if !a.sysAdmin(w, r) {
		return
	}
	st, err := a.backups.Status(r.Context())
	if err != nil {
		backupErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (a *api) createBackup(w http.ResponseWriter, r *http.Request) {
	if !a.sysAdmin(w, r) {
		return
	}
	// Large instances take a while: run in the background, the page polls.
	done := make(chan error, 1)
	go func() {
		_, err := a.backups.Create(context.WithoutCancel(r.Context()))
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			backupErr(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]bool{"done": true})
	case <-r.Context().Done():
	case <-timeAfter():
		writeJSON(w, http.StatusAccepted, map[string]bool{"done": false})
	}
}

func (a *api) downloadBackup(w http.ResponseWriter, r *http.Request) {
	if !a.sysAdmin(w, r) {
		return
	}
	name := chi.URLParam(r, "name")
	rc, size, err := a.backups.Target.Open(r.Context(), name)
	if err != nil {
		backupErr(w, err)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = copyBuffer(w, rc)
}

func (a *api) deleteBackup(w http.ResponseWriter, r *http.Request) {
	if !a.sysAdmin(w, r) {
		return
	}
	if err := a.backups.Target.Delete(r.Context(), chi.URLParam(r, "name")); err != nil {
		backupErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *api) uploadBackup(w http.ResponseWriter, r *http.Request) {
	if !a.sysAdmin(w, r) {
		return
	}
	info, err := a.backups.Upload(r.Context(), r.Body)
	if err != nil {
		backupErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, info)
}

func (a *api) restoreBackup(w http.ResponseWriter, r *http.Request) {
	if !a.sysAdmin(w, r) {
		return
	}
	var in struct {
		Confirm string `json:"confirm"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	name := chi.URLParam(r, "name")
	if in.Confirm != name {
		writeErr(w, core.Invalid("validation.backup_confirm", nil, "type the backup name to confirm"))
		return
	}
	man, err := a.backups.Restore(context.WithoutCancel(r.Context()), name)
	if err != nil {
		backupErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, man)
}

func timeAfter() <-chan time.Time { return time.After(3 * time.Second) }

func copyBuffer(w http.ResponseWriter, r io.Reader) (int64, error) {
	return io.CopyBuffer(w, r, make([]byte, 256<<10))
}
