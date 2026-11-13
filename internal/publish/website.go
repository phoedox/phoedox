package publish

import (
	"html/template"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// Website serves the product website (web/website) on its own domains;
// legal pages come from Markdown files of the operator (impressum.md,
// datenschutz.md in Dir) and are missing until those exist.
type Website struct {
	Domains []string
	Dir     string
	Web     fs.FS
}

var legalPage = template.Must(template.New("legal").Parse(`<!doctype html><html lang="de"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1"><title>{{.Title}} – Phoedox</title><link rel="icon" href="/logo.svg">
<style>body{margin:0 auto;max-width:760px;padding:32px 16px 80px;font:16px/1.65 system-ui,sans-serif;color:#1f1a17;background:#f6f4f2}
a{color:#b84a0c}@media (prefers-color-scheme:dark){body{color:#eee7e1;background:#14110f}a{color:#f26a1b}}</style></head>
<body><p><a href="/">← Phoedox</a></p>{{.Body}}</body></html>`))

// Middleware serves the website for its domains and passes other hosts on.
func (s *Website) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := strings.ToLower(r.Host)
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if len(s.Domains) == 0 || !slices.Contains(s.Domains, host) || r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			next.ServeHTTP(w, r)
			return
		}
		s.serve(w, r)
	})
}

func (s *Website) serve(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; object-src 'none'; base-uri 'none'")
	p := path.Clean(r.URL.Path)
	switch {
	case p == "/" || p == "/index.html":
		s.file(w, r, "website/index.html", "no-cache")
	case p == "/robots.txt":
		h.Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("User-agent: *\nAllow: /\n"))
	case p == "/impressum" || p == "/datenschutz":
		b, err := os.ReadFile(filepath.Join(s.Dir, strings.TrimPrefix(p, "/")+".md"))
		if s.Dir == "" || err != nil {
			http.NotFound(w, r)
			return
		}
		title := map[string]string{"/impressum": "Impressum", "/datenschutz": "Datenschutz"}[p]
		h.Set("Content-Type", "text/html; charset=utf-8")
		_ = legalPage.Execute(w, map[string]any{"Title": title, "Body": template.HTML(Markdown(string(b), Links{}))})
	case strings.HasPrefix(p, "/assets/") || p == "/logo.svg":
		s.file(w, r, strings.TrimPrefix(p, "/"), "public, max-age=31536000, immutable")
	default:
		http.Redirect(w, r, "/", http.StatusFound)
	}
}

func (s *Website) file(w http.ResponseWriter, r *http.Request, name, cache string) {
	b, err := fs.ReadFile(s.Web, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ct := mime.TypeByExtension(path.Ext(name))
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", cache)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(b)
}
