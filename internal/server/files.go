package server

import (
	"encoding/base64"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
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
// ?version=n serves an older version, ?preview=1 the preview image.
func (a *api) serveFile(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	version, _ := strconv.Atoi(q.Get("version"))
	fc, err := a.core.OpenFile(r.Context(), actorOf(r), chi.URLParam(r, "id"), version, q.Get("preview") != "")
	if err != nil {
		writeErr(w, err)
		return
	}
	a.writeFile(w, r, fc, version)
}

// writeFile sends file content with the safe headers.
func (a *api) writeFile(w http.ResponseWriter, r *http.Request, fc core.FileContent, version int) {
	q := r.URL.Query()
	defer fc.Reader.Close()
	ctype := fc.Mime
	if ctype == "" { // preview images: JPEG or PNG
		head := make([]byte, 512)
		n, _ := io.ReadFull(fc.Reader, head)
		ctype = http.DetectContentType(head[:n])
		if _, err := fc.Reader.Seek(0, io.SeekStart); err != nil {
			writeErr(w, err)
			return
		}
	}
	base, _, _ := mime.ParseMediaType(ctype)
	h := w.Header()
	h.Set("Content-Type", ctype)
	h.Set("X-Content-Type-Options", "nosniff")
	if base != "application/pdf" { // browsers refuse to show PDFs in a sandbox
		h.Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'; media-src 'self'; sandbox")
	}
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	h.Set("Cache-Control", "private, max-age=31536000, immutable")
	h.Set("ETag", `"`+fc.SHA256+`"`)
	disp := "attachment"
	if inlineTypes[base] && q.Get("download") == "" {
		disp = "inline"
	}
	name := fc.File.Name
	if version > 0 && version != fc.File.Version {
		ext := path.Ext(name)
		name = strings.TrimSuffix(name, ext) + " (v" + strconv.Itoa(version) + ")" + ext
	}
	h.Set("Content-Disposition", disp+"; filename*=UTF-8''"+strings.ReplaceAll(url.PathEscape(name), "'", "%27"))
	http.ServeContent(w, r, "", fc.File.CreatedAt.Time, fc.Reader)
}

func (a *api) newFileVersion(w http.ResponseWriter, r *http.Request) {
	f, err := a.core.NewFileVersion(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), r.Body)
	respondStatus(w, http.StatusCreated, f, err)
}

func (a *api) fileVersions(w http.ResponseWriter, r *http.Request) {
	vs, err := a.core.FileVersions(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"))
	respond(w, vs, err)
}

func (a *api) restoreFileVersion(w http.ResponseWriter, r *http.Request) {
	n, _ := strconv.Atoi(chi.URLParam(r, "n"))
	f, err := a.core.RestoreFileVersion(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), n)
	respond(w, f, err)
}

func (a *api) storage(w http.ResponseWriter, r *http.Request) {
	s, err := a.core.ProjectStorage(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, s, err)
}

func (a *api) setStorage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		QuotaMB int `json:"quotaMb"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	s, err := a.core.SetProjectQuota(r.Context(), actorOf(r), chi.URLParam(r, "p"), in.QuotaMB)
	respond(w, s, err)
}

// Resumable uploads follow the tus 1.0 core protocol with the creation and
// termination extensions (https://tus.io/protocols/resumable-upload).

func (a *api) tusHeaders(w http.ResponseWriter) {
	w.Header().Set("Tus-Resumable", "1.0.0")
	w.Header().Set("Cache-Control", "no-store")
}

func (a *api) tusOptions(w http.ResponseWriter, _ *http.Request) {
	a.tusHeaders(w)
	w.Header().Set("Tus-Version", "1.0.0")
	w.Header().Set("Tus-Extension", "creation,termination")
	w.Header().Set("Tus-Max-Size", strconv.FormatInt(int64(a.cfg.MaxUploadMB)<<20, 10))
	w.WriteHeader(http.StatusNoContent)
}

// parseTusMeta reads "key base64,key2 base64".
func parseTusMeta(h string) map[string]string {
	out := map[string]string{}
	for _, pair := range strings.Split(h, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(pair), " ")
		if k == "" {
			continue
		}
		b, err := base64.StdEncoding.DecodeString(v)
		if err == nil {
			out[k] = string(b)
		}
	}
	return out
}

func (a *api) tusCreate(w http.ResponseWriter, r *http.Request) {
	a.tusHeaders(w)
	length, err := strconv.ParseInt(r.Header.Get("Upload-Length"), 10, 64)
	if err != nil {
		writeErr(w, &core.InvalidError{Key: "validation.json", Msg: "Upload-Length header missing"})
		return
	}
	m := parseTusMeta(r.Header.Get("Upload-Metadata"))
	u, err := a.core.StartUpload(r.Context(), actorOf(r), chi.URLParam(r, "p"), length,
		core.UploadMeta{Name: m["name"], Document: m["document"], Entity: m["entity"], File: m["file"]})
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Location", "/api/v1/uploads/"+u.ID)
	w.Header().Set("Upload-Offset", "0")
	w.WriteHeader(http.StatusCreated)
}

func (a *api) tusHead(w http.ResponseWriter, r *http.Request) {
	a.tusHeaders(w)
	u, err := a.core.Upload(r.Context(), actorOf(r), chi.URLParam(r, "id"))
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Upload-Offset", strconv.FormatInt(u.Received, 10))
	w.Header().Set("Upload-Length", strconv.FormatInt(u.Length, 10))
	w.WriteHeader(http.StatusOK)
}

func (a *api) tusPatch(w http.ResponseWriter, r *http.Request) {
	a.tusHeaders(w)
	if r.Header.Get("Content-Type") != "application/offset+octet-stream" {
		w.WriteHeader(http.StatusUnsupportedMediaType)
		return
	}
	offset, err := strconv.ParseInt(r.Header.Get("Upload-Offset"), 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	u, f, err := a.core.AppendUpload(r.Context(), actorOf(r), chi.URLParam(r, "id"), offset, r.Body)
	w.Header().Set("Upload-Offset", strconv.FormatInt(u.Received, 10))
	if err != nil {
		writeErr(w, err)
		return
	}
	if f != nil {
		w.Header().Set("Phoedox-File-Id", f.ID)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) tusDelete(w http.ResponseWriter, r *http.Request) {
	a.tusHeaders(w)
	if err := a.core.CancelUpload(r.Context(), actorOf(r), chi.URLParam(r, "id")); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// planImage turns a PDF into a floor plan background image.
func (a *api) planImage(w http.ResponseWriter, r *http.Request) {
	f, err := a.core.PlanImage(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"))
	respond(w, f, err)
}

// fileText returns the text recognised in a file (PDF text, OCR).
func (a *api) fileText(w http.ResponseWriter, r *http.Request) {
	t, err := a.core.FileText(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"))
	respond(w, t, err)
}
