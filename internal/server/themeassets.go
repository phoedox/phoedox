package server

import (
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/core"
)

// uploadThemeAsset takes a font (woff2) or logo for themes (multipart: file, kind, name).
func (a *api) uploadThemeAsset(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 3<<20)
	if err := r.ParseMultipartForm(3 << 20); err != nil {
		writeErr(w, &core.InvalidError{Key: "validation.theme_asset_size", Params: map[string]any{"kb": 2048}, Msg: "file too large"})
		return
	}
	defer r.MultipartForm.RemoveAll()
	f, _, err := r.FormFile("file")
	if err != nil {
		writeErr(w, &core.InvalidError{Key: "validation.theme_asset_type", Msg: "no file"})
		return
	}
	data, err := io.ReadAll(f)
	f.Close()
	if err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.UploadThemeAsset(r.Context(), actorOf(r), r.FormValue("kind"), r.FormValue("name"), data)
	respondStatus(w, http.StatusCreated, v, err)
}

// themeAsset serves a theme asset; content-addressed, so cached for good.
func (a *api) themeAsset(w http.ResponseWriter, r *http.Request) {
	data, mime, err := a.core.ThemeAsset(r.Context(), chi.URLParam(r, "file"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Access-Control-Allow-Origin", "*") // fonts on publications and own domains
	_, _ = w.Write(data)
}
