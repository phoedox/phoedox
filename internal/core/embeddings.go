package core

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/phoedox/phoedox/internal/ai"
	"github.com/phoedox/phoedox/internal/store"
)

// Semantic search (ADR-0081): the system admin picks an embedding model of
// one of the instance's OpenAI-compatible AI providers, or switches it off.
// A background job embeds every search index entry; search then ranks the
// hits of the full text (with typo correction) and of the vectors together
// (reciprocal rank fusion). Without it, search works as before.

const settingEmbeddings = "search.embeddings"

// EmbeddingSettings is the configuration of semantic search.
type EmbeddingSettings struct {
	Enabled    bool   `json:"enabled"`
	ProviderID string `json:"providerId"`
	Model      string `json:"model"`
}

// EmbeddingStatus is what the administration shows.
type EmbeddingStatus struct {
	EmbeddingSettings
	// Providers are the instance's providers that offer embeddings.
	Providers []EmbeddingProvider `json:"providers"`
	// Have of Total index entries have an embedding of the model.
	Have      int    `json:"have"`
	Total     int    `json:"total"`
	LastError string `json:"lastError,omitempty"`
}

// EmbeddingProvider is a provider to choose from.
type EmbeddingProvider struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// queryCache keeps the embeddings of recent queries (typing in the palette
// repeats them).
var queryCache struct {
	sync.Mutex
	m map[string][]float32
}

var embedState struct {
	sync.Mutex
	lastError string
	failedAt  time.Time
}

func (s *Service) embeddingSettings(ctx context.Context) EmbeddingSettings {
	var c EmbeddingSettings
	if raw, _ := s.DB.Setting(store.WithTenant(ctx, store.DefaultTenant), settingEmbeddings, ""); raw != "" {
		_ = json.Unmarshal([]byte(raw), &c)
	}
	return c
}

// embedder returns the configured embedder and model (ok false when off).
func (s *Service) embedder(ctx context.Context) (ai.Embedder, string, bool) {
	c := s.embeddingSettings(ctx)
	if !c.Enabled || c.Model == "" || !s.AIEnabled(ctx) {
		return nil, "", false
	}
	p, err := s.DB.GetAIProvider(store.WithTenant(ctx, store.DefaultTenant), "", c.ProviderID)
	if err != nil || !p.Enabled || p.Kind != ai.KindOpenAI {
		return nil, "", false
	}
	client, err := s.aiClient(p)
	if err != nil {
		return nil, "", false
	}
	e, ok := client.(ai.Embedder)
	return e, c.Model, ok
}

func (s *Service) embeddingStatus(ctx context.Context) EmbeddingStatus {
	out := EmbeddingStatus{EmbeddingSettings: s.embeddingSettings(ctx), Providers: []EmbeddingProvider{}}
	ps, _ := s.DB.AIProviders(store.WithTenant(ctx, store.DefaultTenant), "")
	for _, p := range ps {
		if p.Kind == ai.KindOpenAI {
			out.Providers = append(out.Providers, EmbeddingProvider{ID: p.ID, Name: p.Name})
		}
	}
	if out.Model != "" {
		out.Have, out.Total, _ = s.DB.VectorCounts(store.System(ctx), out.Model)
	}
	embedState.Lock()
	out.LastError = embedState.lastError
	embedState.Unlock()
	return out
}

// AdminEmbeddings shows the semantic search settings (system admins).
func (s *Service) AdminEmbeddings(ctx context.Context, a Actor) (EmbeddingStatus, error) {
	if err := s.adminOnly(a); err != nil {
		return EmbeddingStatus{}, err
	}
	return s.embeddingStatus(ctx), nil
}

// SetAdminEmbeddings changes them; switching on checks the model with a
// test call, a new model drops the embeddings of the old one.
func (s *Service) SetAdminEmbeddings(ctx context.Context, a Actor, in EmbeddingSettings) (EmbeddingStatus, error) {
	if err := s.adminOnly(a); err != nil {
		return EmbeddingStatus{}, err
	}
	in.Model = strings.TrimSpace(in.Model)
	old := s.embeddingSettings(ctx)
	if in.Enabled {
		p, err := s.DB.GetAIProvider(store.WithTenant(ctx, store.DefaultTenant), "", in.ProviderID)
		if err != nil || p.Kind != ai.KindOpenAI {
			return EmbeddingStatus{}, invalid("validation.embeddings_provider", nil, "choose an OpenAI-compatible provider of the instance")
		}
		if in.Model == "" || len(in.Model) > 200 {
			return EmbeddingStatus{}, invalid("validation.embeddings_model", nil, "enter the embedding model")
		}
		client, err := s.aiClient(p)
		if err != nil {
			return EmbeddingStatus{}, err
		}
		e, ok := client.(ai.Embedder)
		if !ok {
			return EmbeddingStatus{}, invalid("validation.embeddings_provider", nil, "this provider offers no embeddings")
		}
		tctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if _, err := e.Embed(tctx, in.Model, []string{"Phoedox"}); err != nil {
			return EmbeddingStatus{}, invalid("validation.embeddings_test", map[string]any{"msg": err.Error()}, "the model answered: %s", err.Error())
		}
	}
	raw, _ := json.Marshal(in)
	if err := s.DB.SetSetting(store.WithTenant(ctx, store.DefaultTenant), settingEmbeddings, string(raw), a.User.ID); err != nil {
		return EmbeddingStatus{}, err
	}
	if in.Model != "" && in.Model != old.Model {
		_ = s.DB.DropVectors(store.System(ctx), in.Model)
	}
	embedState.Lock()
	embedState.lastError = ""
	embedState.Unlock()
	s.event(ctx, a, "search.embeddings", "instance", map[string]any{"enabled": in.Enabled, "provider": in.ProviderID, "model": in.Model})
	return s.embeddingStatus(ctx), nil
}

// RunEmbeddings embeds index entries that have no embedding yet (a batch
// per call; the scheduler calls it every minute). After an error it waits
// ten minutes.
func (s *Service) RunEmbeddings(ctx context.Context) {
	e, model, ok := s.embedder(ctx)
	if !ok {
		return
	}
	embedState.Lock()
	wait := time.Since(embedState.failedAt) < 10*time.Minute
	embedState.Unlock()
	if wait {
		return
	}
	sys := store.System(ctx)
	for round := 0; round < 10; round++ {
		jobs, err := s.DB.VectorsMissing(sys, model, 32)
		if err != nil || len(jobs) == 0 {
			return
		}
		texts := make([]string, len(jobs))
		for i, j := range jobs {
			texts[i] = excerpt(j.Title+"\n\n"+j.Body, 4000)
		}
		cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		vecs, err := e.Embed(cctx, model, texts)
		cancel()
		if err != nil {
			embedState.Lock()
			embedState.lastError, embedState.failedAt = err.Error(), time.Now()
			embedState.Unlock()
			slog.Warn("embeddings failed", "model", model, "err", err)
			return
		}
		for i, j := range jobs {
			if err := s.DB.PutVector(sys, j, model, vecs[i]); err != nil {
				slog.Warn("store embedding", "ref", j.RefID, "err", err)
			}
		}
		embedState.Lock()
		embedState.lastError = ""
		embedState.Unlock()
	}
}

// semanticHits finds index entries close in meaning to the query.
func (s *Service) semanticHits(ctx context.Context, projectIDs []string, query string, limit int) []store.Hit {
	e, model, ok := s.embedder(ctx)
	if !ok || strings.TrimSpace(query) == "" {
		return nil
	}
	key := model + "\x00" + strings.ToLower(strings.TrimSpace(query))
	queryCache.Lock()
	qv, cached := queryCache.m[key]
	queryCache.Unlock()
	if !cached {
		cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		vecs, err := e.Embed(cctx, model, []string{query})
		cancel()
		if err != nil {
			slog.Warn("embed query", "err", err)
			return nil
		}
		qv = store.Normalize(vecs[0])
		queryCache.Lock()
		if queryCache.m == nil || len(queryCache.m) > 500 {
			queryCache.m = map[string][]float32{}
		}
		queryCache.m[key] = qv
		queryCache.Unlock()
	}
	matches, err := s.DB.NearestVectors(ctx, projectIDs, model, qv, 0.3, limit)
	if err != nil || len(matches) == 0 {
		return nil
	}
	ids := make([]string, len(matches))
	for i, m := range matches {
		ids[i] = m.RefID
	}
	hits, err := s.DB.IndexHits(ctx, ids, strings.Fields(strings.ToLower(query)))
	if err != nil {
		return nil
	}
	order := map[string]int{}
	for i, id := range ids {
		order[id] = i
	}
	sorted := make([]store.Hit, len(ids))
	n := 0
	for _, h := range hits {
		ref := h.ID
		if h.Locale != "" {
			ref = store.TranslationRef(h.ID, h.Locale)
		}
		if i, ok := order[ref]; ok {
			sorted[i] = h
			n++
		}
	}
	out := sorted[:0]
	for _, h := range sorted {
		if h.ID != "" {
			out = append(out, h)
		}
	}
	return out
}

// fuseHits merges the full-text and the semantic ranking (reciprocal rank
// fusion, k = 60); a hit found by both rises.
func fuseHits(text, sem []store.Hit, limit int) []store.Hit {
	if len(sem) == 0 {
		return text
	}
	type entry struct {
		h     store.Hit
		score float64
	}
	byKey := map[string]*entry{}
	var keys []string
	add := func(list []store.Hit) {
		for i, h := range list {
			k := h.Type + "/" + h.ID
			e, ok := byKey[k]
			if !ok {
				e = &entry{h: h}
				byKey[k] = e
				keys = append(keys, k)
			} else if e.h.Snippet == "" {
				e.h.Snippet = h.Snippet
			}
			e.score += 1 / float64(60+i+1)
		}
	}
	add(text)
	add(sem)
	out := make([]store.Hit, 0, len(keys))
	for _, k := range keys {
		e := byKey[k]
		e.h.Score = e.score
		out = append(out, e.h)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Score > out[j-1].Score; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}
