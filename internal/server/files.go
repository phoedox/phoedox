package server

import (
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/core"
)

// inlineTypes may be shown in the browser; everything else is downloaded.
var inlineTypes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true, "image/avif": true, "image/svg+xml": true,
	"application/pdf": true, "video/mp4": true, "video/webm": true, "audio/mpeg": true, "audio/ogg": true, "audio/wav": true, "text/plain": true,
}

func (a *api) uploadFile(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f, err := a.core.UploadFile(r.Context(), actorOf(r), chi.URLParam(r, "p"),
		core.FileInput{Name: q.Get("name"), Document: q.Get("document"), Entity: q.Get("entity")}, r.Body)
	respondStatus(w, http.StatusCreated, f, err)
}

func (a *api) listFiles(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	fs, err := a.core.Files(r.Context(), actorOf(r), chi.URLParam(r, "p"), core.FileFilter{Document: q.Get("document"), Entity: q.Get("entity")})
	respond(w, fs, err)
}

func (a *api) renameFile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	f, err := a.core.RenameFile(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), in.Name)
	respond(w, f, err)
}

func (a *api) deleteFile(w http.ResponseWriter, r *http.Request) {
	err := a.core.DeleteFile(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"))
	respond(w, map[string]bool{"ok": err == nil}, err)
}

// serveFile streams a file with headers that keep uploaded content from
// running in the app's origin: no sniffing, a sandboxing CSP, and
// attachment for anything that is not a plain media type.
func (a *api) serveFile(w http.ResponseWriter, r *http.Request) {
	f, rc, err := a.core.OpenFile(r.Context(), actorOf(r), chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rc.Close()
	base, _, _ := mime.ParseMediaType(f.Mime)
	h := w.Header()
	h.Set("Content-Type", f.Mime)
	h.Set("X-Content-Type-Options", "nosniff")
	if base != "application/pdf" { // browsers refuse to show PDFs in a sandbox
		h.Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'; media-src 'self'; sandbox")
	}
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	h.Set("Cache-Control", "private, max-age=31536000, immutable")
	h.Set("ETag", `"`+f.SHA256+`"`)
	disp := "attachment"
	if inlineTypes[base] && r.URL.Query().Get("download") == "" {
		disp = "inline"
	}
	h.Set("Content-Disposition", disp+"; filename*=UTF-8''"+strings.ReplaceAll(url.PathEscape(f.Name), "'", "%27"))
	http.ServeContent(w, r, "", f.CreatedAt.Time, rc)
}
