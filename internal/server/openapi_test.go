package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/auth"
	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/store"
)

// Every API route must be documented (and nothing documented may be gone).
func TestOpenAPI(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, store.SQLite, filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	h := New(Deps{Core: core.New(db), Auth: &auth.Auth{DB: db}})
	seen := map[string]bool{}
	_ = chi.Walk(h.(chi.Routes), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.HasPrefix(route, "/api/v1/") {
			k := normRoute(method, route)
			seen[k] = true
			if d, ok := apiDocs[k]; !ok || d.Summary == "" || d.Tag == "" {
				t.Errorf("undocumented route: %s", k)
			}
		}
		return nil
	})
	for k := range apiDocs {
		if !seen[k] {
			t.Errorf("documented route does not exist: %s", k)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/openapi.json", nil))
	var spec map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil || rec.Code != 200 || spec["openapi"] != "3.1.0" {
		t.Fatalf("spec: %d %v", rec.Code, err)
	}
	paths := spec["paths"].(map[string]any)
	doc := paths["/projects/{p}/documents"].(map[string]any)["post"].(map[string]any)
	body, _ := json.Marshal(doc["requestBody"])
	if !strings.Contains(string(body), `"markdown"`) || !strings.Contains(string(body), `"adr"`) {
		t.Fatalf("request schema from Go types: %s", body)
	}
}
