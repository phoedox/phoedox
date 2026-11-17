package core

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/ai"
	"github.com/phoedox/phoedox/internal/store"
)

// fakeEmbeddings maps texts to 3-dimensional "meanings": network, kitchen, other.
func fakeEmbeddings(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &in)
		if in.Model != "embed-small" {
			w.WriteHeader(404)
			_, _ = io.WriteString(w, `{"error":{"message":"model not found"}}`)
			return
		}
		type item struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		}
		var data []item
		for i, s := range in.Input {
			s = strings.ToLower(s)
			v := []float32{0, 0, 0.1}
			for _, w := range []string{"router", "wlan", "internet", "network", "fritz"} {
				if strings.Contains(s, w) {
					v[0] = 1
				}
			}
			for _, w := range []string{"oven", "kitchen", "fridge", "herd"} {
				if strings.Contains(s, w) {
					v[1] = 1
				}
			}
			data = append(data, item{Index: i, Embedding: v})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
}

func TestSemanticSearch(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	admin := Actor{User: store.User{ID: owner.User.ID, Name: "Admin", IsAdmin: true}, Via: "web"}
	srv := fakeEmbeddings(t)
	defer srv.Close()
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus"})
	wlan, _ := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: store.DocMarkdown, Title: "WLAN im Haus", Markdown: "Die Fritz!Box im Flur funkt im ganzen Haus."})
	_, _ = s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: store.DocMarkdown, Title: "Küche", Markdown: "Der Herd und die Kühlschrank-Tür."})

	// Without semantic search, a query by meaning finds nothing.
	if hits, _ := s.Search(ctx, owner, "internet", p.Slug, 10); len(hits) != 0 {
		t.Fatalf("plain: %+v", hits)
	}
	prov, err := s.SaveAIProvider(ctx, admin, "", "", AIProviderInput{Name: "Local", Kind: ai.KindOpenAI, BaseURL: srv.URL, Models: []store.AIModel{{ID: "chat"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetAdminEmbeddings(ctx, owner, EmbeddingSettings{Enabled: true, ProviderID: prov.ID, Model: "embed-small"}); err == nil {
		t.Fatal("only system admins")
	}
	if _, err := s.SetAdminEmbeddings(ctx, admin, EmbeddingSettings{Enabled: true, ProviderID: prov.ID, Model: "nope"}); invalidKey(err) != "validation.embeddings_test" {
		t.Fatalf("test call: %v", err)
	}
	st, err := s.SetAdminEmbeddings(ctx, admin, EmbeddingSettings{Enabled: true, ProviderID: prov.ID, Model: "embed-small"})
	if err != nil || len(st.Providers) != 1 || st.Have != 0 || st.Total < 2 {
		t.Fatalf("settings: %v %+v", err, st)
	}
	s.RunEmbeddings(ctx)
	if st, _ = s.AdminEmbeddings(ctx, admin); st.Have != st.Total || st.LastError != "" {
		t.Fatalf("embedded: %+v", st)
	}
	hits, err := s.Search(ctx, owner, "internet", p.Slug, 10)
	if err != nil || len(hits) != 1 || hits[0].ID != wlan.Document.ID {
		t.Fatalf("semantic: %v %+v", err, hits)
	}
	// A changed page loses its embedding until the job runs again.
	newMD := "Jetzt steht hier etwas über den Herd."
	if _, err := s.UpdateDocument(ctx, owner, p.Slug, wlan.Document.Slug, DocumentPatch{Markdown: &newMD}); err != nil {
		t.Fatal(err)
	}
	if st, _ = s.AdminEmbeddings(ctx, admin); st.Have != st.Total-1 {
		t.Fatalf("changed: %+v", st)
	}
	// Switched off: search works as before.
	if _, err := s.SetAdminEmbeddings(ctx, admin, EmbeddingSettings{Enabled: false, ProviderID: prov.ID, Model: "embed-small"}); err != nil {
		t.Fatal(err)
	}
	if hits, _ := s.Search(ctx, owner, "internet", p.Slug, 10); len(hits) != 0 {
		t.Fatalf("off: %+v", hits)
	}
}
