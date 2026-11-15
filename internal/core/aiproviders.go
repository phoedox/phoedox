package core

import (
	"context"
	"net/url"
	"slices"
	"strings"

	"github.com/phoedox/phoedox/internal/ai"
	"github.com/phoedox/phoedox/internal/ai/anthropic"
	"github.com/phoedox/phoedox/internal/ai/openai"
	"github.com/phoedox/phoedox/internal/requests"
	"github.com/phoedox/phoedox/internal/store"
)

// Model providers of the AI chat (ADR-0067). System admins set up providers
// for the instance, organization admins for their organization (own keys).
// A provider is Anthropic or any OpenAI-compatible API (OpenAI, Mistral,
// Kimi, Ollama, gateways); each lists its models with prices. The provider
// from PHOEDOX_AI_* appears as the built-in provider "env".

// EnvProviderID is the provider configured with PHOEDOX_AI_* variables.
const EnvProviderID = "env"

// ChatModel is a model people can choose in the chat.
type ChatModel struct {
	// Ref is "provider/model".
	Ref        string `json:"ref"`
	ProviderID string `json:"providerId"`
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	Label      string `json:"label"`
	// Own: the organization's own provider (its key pays).
	Own    bool     `json:"own,omitempty"`
	price  ai.Price `json:"-"`
	source store.AIProvider
}

func (m ChatModel) name() string {
	if m.Label != "" {
		return m.Label
	}
	return m.Model
}

// envProvider describes the built-in provider for lists.
func (s *Service) envProvider() (store.AIProvider, bool) {
	if s.AI == nil {
		return store.AIProvider{}, false
	}
	model := s.AIEnvModel
	if model == "" {
		model = anthropic.DefaultModel
	}
	price := ai.PriceOf(model, s.AIPrice)
	return store.AIProvider{ID: EnvProviderID, Name: "PHOEDOX_AI_*", Kind: ai.KindAnthropic, HasKey: true, Enabled: true, BuiltIn: true,
		Models: []store.AIModel{{ID: model, Input: price.Input, Output: price.Output, CacheRead: price.CacheRead, CacheWrite: price.CacheWrite}}}, true
}

func modelsOf(ps []store.AIProvider, own bool) []ChatModel {
	var out []ChatModel
	for _, p := range ps {
		if !p.Enabled {
			continue
		}
		for _, m := range p.Models {
			cm := ChatModel{Ref: p.ID + "/" + m.ID, ProviderID: p.ID, Provider: p.Name, Model: m.ID, Label: m.Label, Own: own,
				price: ai.Price{Input: m.Input, Output: m.Output, CacheRead: m.CacheRead, CacheWrite: m.CacheWrite}, source: p}
			cm.Label = cm.name()
			out = append(out, cm)
		}
	}
	return out
}

// instanceModels are the models of the instance's providers.
func (s *Service) instanceModels(ctx context.Context) []ChatModel {
	ps, _ := s.DB.AIProviders(ctx, "")
	if env, ok := s.envProvider(); ok {
		ps = append([]store.AIProvider{env}, ps...)
	}
	return modelsOf(ps, false)
}

// orgModels are the models of an organization's own providers.
func (s *Service) orgModels(ctx context.Context, orgID string) []ChatModel {
	if orgID == "" {
		return nil
	}
	ps, _ := s.DB.AIProviders(ctx, orgID)
	return modelsOf(ps, true)
}

// projectModels are all models available in a project: the organization's
// own first, then the instance's.
func (s *Service) projectModels(ctx context.Context, p store.Project) []ChatModel {
	return append(s.orgModels(ctx, p.OrgID), s.instanceModels(ctx)...)
}

func hasModel(ms []ChatModel, ref string) bool {
	return slices.ContainsFunc(ms, func(m ChatModel) bool { return m.Ref == ref })
}

// ChatModels lists the models of a project and the default one.
type ChatModels struct {
	Models  []ChatModel `json:"models"`
	Default string      `json:"default"`
}

func (s *Service) defaultModel(ctx context.Context, p store.Project, ms []ChatModel) string {
	var prefs []string
	if p.OrgID != "" {
		if o, err := s.DB.OrgFor(ctx, p.OrgID, ""); err == nil {
			prefs = append(prefs, o.AIChatModel)
		}
	}
	inst, _ := s.DB.Setting(ctx, settingAIChatModel, "")
	prefs = append(prefs, inst)
	for _, ref := range prefs {
		if ref != "" && hasModel(ms, ref) {
			return ref
		}
	}
	if len(ms) > 0 {
		return ms[0].Ref
	}
	return ""
}

// ProjectChatModels returns the models a person can choose in a project.
func (s *Service) ProjectChatModels(ctx context.Context, a Actor, ref string) (ChatModels, error) {
	if err := s.fullUser(a); err != nil {
		return ChatModels{}, err
	}
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return ChatModels{}, err
	}
	ms := s.projectModels(ctx, p)
	if ms == nil {
		ms = []ChatModel{}
	}
	return ChatModels{Models: ms, Default: s.defaultModel(ctx, p, ms)}, nil
}

// ResolveChatModel finds the model to use ("" = the default) and its client.
func (s *Service) ResolveChatModel(ctx context.Context, a Actor, ref, want string) (ChatModel, ai.Provider, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return ChatModel{}, nil, err
	}
	ms := s.projectModels(ctx, p)
	if want == "" || !hasModel(ms, want) {
		want = s.defaultModel(ctx, p, ms)
	}
	for _, m := range ms {
		if m.Ref == want {
			c, err := s.aiClient(m.source)
			return m, c, err
		}
	}
	return ChatModel{}, nil, invalid("ai_chat.unavailable.not_configured", nil, "no model is set up")
}

// aiClient builds the API client of a provider. Organizations' providers
// connect like request blocks (no private networks on hosted instances);
// the instance's providers are trusted.
func (s *Service) aiClient(p store.AIProvider) (ai.Provider, error) {
	if p.ID == EnvProviderID {
		if s.AI == nil {
			return nil, invalid("ai_chat.unavailable.not_configured", nil, "no provider configured")
		}
		return s.AI, nil
	}
	key := ""
	if p.APIKey != "" {
		b, err := s.Box.Open(p.APIKey, "aiprovider:"+p.ID)
		if err != nil {
			return nil, err
		}
		key = string(b)
	}
	policy := s.RequestPolicy
	if p.OrgID == "" {
		policy.AllowPrivate = true
	}
	client := policy.StreamClient()
	switch p.Kind {
	case ai.KindAnthropic:
		return anthropic.New(key, p.BaseURL, p.Effort, client), nil
	case ai.KindOpenAI:
		return openai.New(p.BaseURL, key, client), nil
	}
	return nil, invalid("validation.ai_provider_kind", nil, "unknown provider kind %q", p.Kind)
}

// AIProviderInput creates or changes a provider. APIKey nil keeps the
// stored key, "" removes it.
type AIProviderInput struct {
	Name    string          `json:"name"`
	Kind    string          `json:"kind"`
	BaseURL string          `json:"baseUrl"`
	APIKey  *string         `json:"apiKey,omitempty"`
	Effort  string          `json:"effort"`
	Models  []store.AIModel `json:"models"`
	Enabled *bool           `json:"enabled,omitempty"`
}

// providerOwner checks who may manage providers: system admins for the
// instance (org ""), organization admins for their organization.
func (s *Service) providerOwner(ctx context.Context, a Actor, orgRef string) (string, error) {
	if err := s.fullUser(a); err != nil {
		return "", err
	}
	if orgRef == "" {
		if !a.User.IsAdmin {
			return "", ErrForbidden
		}
		return "", nil
	}
	o, err := s.org(ctx, a, orgRef, store.OrgAdmin)
	if err != nil {
		return "", err
	}
	return o.ID, nil
}

// AIProviders lists the providers of the instance or an organization (with
// the built-in provider for the instance).
func (s *Service) AIProviders(ctx context.Context, a Actor, orgRef string) ([]store.AIProvider, error) {
	orgID, err := s.providerOwner(ctx, a, orgRef)
	if err != nil {
		return nil, err
	}
	ps, err := s.DB.AIProviders(ctx, orgID)
	if err != nil {
		return nil, err
	}
	if env, ok := s.envProvider(); ok && orgID == "" {
		ps = append([]store.AIProvider{env}, ps...)
	}
	return ps, nil
}

func validProvider(in AIProviderInput) error {
	if n := strings.TrimSpace(in.Name); n == "" || len(n) > 80 {
		return invalid("validation.ai_provider_name", nil, "name is required (at most 80 characters)")
	}
	if in.Kind != ai.KindAnthropic && in.Kind != ai.KindOpenAI {
		return invalid("validation.ai_provider_kind", nil, "kind must be anthropic or openai")
	}
	if in.BaseURL != "" || in.Kind == ai.KindOpenAI {
		u, err := url.Parse(in.BaseURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
			return invalid("validation.ai_provider_url", nil, "the address must be an http(s) URL")
		}
	}
	switch in.Effort {
	case "", "low", "medium", "high", "xhigh", "max":
	default:
		return invalid("validation.ai_provider_effort", nil, "effort must be low, medium, high, xhigh or max")
	}
	seen := map[string]bool{}
	for _, m := range in.Models {
		id := strings.TrimSpace(m.ID)
		if id == "" || len(id) > 200 || seen[id] {
			return invalid("validation.ai_model_id", nil, "every model needs its own id")
		}
		seen[id] = true
		for _, p := range []float64{m.Input, m.Output, m.CacheRead, m.CacheWrite} {
			if p < 0 || p > 10_000 {
				return invalid("validation.ai_model_price", nil, "prices must be between 0 and 10000 dollars per million tokens")
			}
		}
	}
	if len(in.Models) > 100 {
		return invalid("validation.ai_model_id", nil, "at most 100 models")
	}
	return nil
}

// SaveAIProvider creates (id "") or changes a provider.
func (s *Service) SaveAIProvider(ctx context.Context, a Actor, orgRef, id string, in AIProviderInput) (store.AIProvider, error) {
	orgID, err := s.providerOwner(ctx, a, orgRef)
	if err != nil {
		return store.AIProvider{}, err
	}
	if id == EnvProviderID {
		return store.AIProvider{}, invalid("validation.ai_provider_builtin", nil, "the built-in provider is set with PHOEDOX_AI_* variables")
	}
	in.Name, in.BaseURL = strings.TrimSpace(in.Name), strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	for i := range in.Models {
		in.Models[i].ID, in.Models[i].Label = strings.TrimSpace(in.Models[i].ID), strings.TrimSpace(in.Models[i].Label)
	}
	if err := validProvider(in); err != nil {
		return store.AIProvider{}, err
	}
	now := store.Now()
	p := store.AIProvider{ID: newID(), OrgID: orgID, Enabled: true, CreatedBy: a.String(), CreatedAt: now}
	if id != "" {
		if p, err = s.DB.GetAIProvider(ctx, orgID, id); err != nil {
			return p, err
		}
	}
	p.Name, p.Kind, p.BaseURL, p.Effort, p.UpdatedAt = in.Name, in.Kind, in.BaseURL, in.Effort, now
	p.Models = in.Models
	if p.Models == nil {
		p.Models = []store.AIModel{}
	}
	if in.Enabled != nil {
		p.Enabled = *in.Enabled
	}
	if in.APIKey != nil {
		p.APIKey = ""
		if k := strings.TrimSpace(*in.APIKey); k != "" {
			if p.APIKey, err = s.Box.Seal([]byte(k), "aiprovider:"+p.ID); err != nil {
				return p, err
			}
		}
	}
	if err := s.DB.SaveAIProvider(ctx, p); err != nil {
		return p, err
	}
	s.event(ctx, a, "ai.provider_save", p.ID, map[string]any{"org": orgID, "name": p.Name, "kind": p.Kind, "baseUrl": p.BaseURL, "models": len(p.Models)})
	p.HasKey = p.APIKey != ""
	return p, nil
}

// DeleteAIProvider removes a provider.
func (s *Service) DeleteAIProvider(ctx context.Context, a Actor, orgRef, id string) error {
	orgID, err := s.providerOwner(ctx, a, orgRef)
	if err != nil {
		return err
	}
	if err := s.DB.DeleteAIProvider(ctx, orgID, id); err != nil {
		return err
	}
	s.event(ctx, a, "ai.provider_delete", id, map[string]any{"org": orgID})
	return nil
}

// DiscoverAIModels asks a saved provider which models it offers.
func (s *Service) DiscoverAIModels(ctx context.Context, a Actor, orgRef, id string) ([]string, error) {
	orgID, err := s.providerOwner(ctx, a, orgRef)
	if err != nil {
		return nil, err
	}
	var p store.AIProvider
	if id == EnvProviderID && orgID == "" {
		var ok bool
		if p, ok = s.envProvider(); !ok {
			return nil, store.ErrNotFound
		}
	} else if p, err = s.DB.GetAIProvider(ctx, orgID, id); err != nil {
		return nil, err
	}
	c, err := s.aiClient(p)
	if err != nil {
		return nil, err
	}
	ms, err := c.Models(ctx)
	if err != nil {
		detail := trimErr(err)
		return nil, invalid("validation.ai_provider_unreachable", map[string]any{"detail": detail}, "the provider did not answer: %s", detail)
	}
	slices.Sort(ms)
	return ms, nil
}

// trimErr keeps an upstream error short for the message.
func trimErr(err error) string {
	m := err.Error()
	if errorsIsBlocked(err) {
		return "address not allowed"
	}
	if len(m) > 200 {
		m = m[:200] + "…"
	}
	return m
}

func errorsIsBlocked(err error) bool {
	return err != nil && strings.Contains(err.Error(), requests.ErrBlocked.Error())
}
