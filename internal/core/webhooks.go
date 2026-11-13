package core

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/jobs"
	"github.com/phoedox/phoedox/internal/store"
)

// WebhookVersion is the version of the event payload. Breaking changes get a
// new version; hooks keep receiving the version they were made for.
const WebhookVersion = 1

// JobWebhook delivers one event to one webhook.
const JobWebhook = "webhook.deliver"

// webhookEvents maps internal event kinds to public webhook event types.
var webhookEvents = map[string]string{
	"document.create": "document.created", "document.update": "document.updated", "document.live": "document.updated",
	"document.delete": "document.deleted", "document.restore": "document.restored",
	"entity.upsert": "entity.saved", "entity.delete": "entity.deleted", "entity.restore": "entity.restored",
	"edge.link": "relation.created", "edge.unlink": "relation.deleted",
	"file.upload": "file.uploaded", "file.version": "file.updated", "file.delete": "file.deleted",
	"suggestion.create": "suggestion.created", "suggestion.accept": "suggestion.accepted", "suggestion.reject": "suggestion.rejected",
}

// WebhookEventTypes lists the event types a webhook can subscribe to.
func WebhookEventTypes() []string {
	var out []string
	for _, t := range webhookEvents {
		if !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	slices.Sort(out)
	return out
}

// WebhookEvent is what a webhook receives (JSON body).
type WebhookEvent struct {
	Version   int       `json:"version"`
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"createdAt"`
	Project   struct {
		ID   string `json:"id"`
		Slug string `json:"slug"`
		Name string `json:"name"`
	} `json:"project"`
	Actor   string `json:"actor"`
	Subject string `json:"subject"`
	Data    any    `json:"data,omitempty"`
}

// WebhookInput creates or changes a webhook.
type WebhookInput struct {
	URL    string   `json:"url"`
	Events []string `json:"events"` // ["*"] = all
	Active *bool    `json:"active,omitempty"`
}

// NewWebhook is a webhook with its signing secret (shown once).
type NewWebhook struct {
	store.Webhook
	Secret string `json:"secret"`
}

func (in *WebhookInput) check() error {
	u, err := url.Parse(strings.TrimSpace(in.URL))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || len(in.URL) > 2000 {
		return invalid("validation.webhook_url", nil, "the URL must start with https:// (or http://)")
	}
	in.URL = u.String()
	if len(in.Events) == 0 {
		in.Events = []string{"*"}
	}
	types := WebhookEventTypes()
	for _, e := range in.Events {
		if e != "*" && !slices.Contains(types, e) {
			return invalid("validation.webhook_event", map[string]any{"event": e}, "unknown event %q", e)
		}
	}
	return nil
}

func (s *Service) hookOwner(ctx context.Context, a Actor, ref string) (store.Project, error) {
	if err := s.fullUser(a); err != nil {
		return store.Project{}, err
	}
	if s.Box == nil {
		return store.Project{}, errors.New("core: no master key for webhook secrets")
	}
	return s.project(ctx, a, ref, store.RoleOwner)
}

// Webhooks lists the webhooks of a project (owners).
func (s *Service) Webhooks(ctx context.Context, a Actor, ref string) ([]store.Webhook, error) {
	p, err := s.hookOwner(ctx, a, ref)
	if err != nil {
		return nil, err
	}
	return s.DB.Webhooks(ctx, p.ID)
}

// CreateWebhook adds a webhook; the secret signs every delivery.
func (s *Service) CreateWebhook(ctx context.Context, a Actor, ref string, in WebhookInput) (NewWebhook, error) {
	p, err := s.hookOwner(ctx, a, ref)
	if err != nil {
		return NewWebhook{}, err
	}
	if err := in.check(); err != nil {
		return NewWebhook{}, err
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return NewWebhook{}, err
	}
	secret := "whsec_" + base64.RawURLEncoding.EncodeToString(b)
	h := store.Webhook{ID: newID(), ProjectID: p.ID, URL: in.URL, Events: in.Events, Active: true, CreatedBy: a.User.ID}
	if h.Secret, err = s.Box.Seal([]byte(secret), "webhook:"+h.ID); err != nil {
		return NewWebhook{}, err
	}
	if err := s.DB.SaveWebhook(ctx, h); err != nil {
		return NewWebhook{}, err
	}
	s.event(ctx, a, "webhook.create", h.ID, map[string]any{"url": h.URL, "events": h.Events})
	return NewWebhook{Webhook: h, Secret: secret}, nil
}

func (s *Service) hookIn(ctx context.Context, a Actor, ref, id string) (store.Project, store.Webhook, error) {
	p, err := s.hookOwner(ctx, a, ref)
	if err != nil {
		return p, store.Webhook{}, err
	}
	h, err := s.DB.GetWebhook(ctx, id)
	if err != nil || h.ProjectID != p.ID {
		return p, h, ErrNotFound
	}
	return p, h, nil
}

// UpdateWebhook changes URL, events or turns a webhook on/off (resets failures).
func (s *Service) UpdateWebhook(ctx context.Context, a Actor, ref, id string, in WebhookInput) (store.Webhook, error) {
	_, h, err := s.hookIn(ctx, a, ref, id)
	if err != nil {
		return h, err
	}
	if in.URL == "" {
		in.URL = h.URL
	}
	if in.Events == nil {
		in.Events = h.Events
	}
	if err := in.check(); err != nil {
		return h, err
	}
	h.URL, h.Events = in.URL, in.Events
	if in.Active != nil {
		h.Active = *in.Active
		h.Failures = 0
	}
	if err := s.DB.SaveWebhook(ctx, h); err != nil {
		return h, err
	}
	s.event(ctx, a, "webhook.update", h.ID, in)
	return h, nil
}

// DeleteWebhook removes a webhook.
func (s *Service) DeleteWebhook(ctx context.Context, a Actor, ref, id string) error {
	p, h, err := s.hookIn(ctx, a, ref, id)
	if err != nil {
		return err
	}
	if err := s.DB.DeleteWebhook(ctx, p.ID, h.ID); err != nil {
		return err
	}
	s.event(ctx, a, "webhook.delete", h.ID, nil)
	return nil
}

// WebhookDeliveries lists the latest deliveries of a webhook.
func (s *Service) WebhookDeliveries(ctx context.Context, a Actor, ref, id string) ([]store.Delivery, error) {
	_, h, err := s.hookIn(ctx, a, ref, id)
	if err != nil {
		return nil, err
	}
	return s.DB.Deliveries(ctx, h.ID)
}

// TestWebhook sends a "ping" event now.
func (s *Service) TestWebhook(ctx context.Context, a Actor, ref, id string) (store.Delivery, error) {
	p, h, err := s.hookIn(ctx, a, ref, id)
	if err != nil {
		return store.Delivery{}, err
	}
	x, err := s.enqueueDelivery(ctx, h, s.webhookEvent(p, a, "ping", p.ID, map[string]string{"message": "Phoedox webhook test"}))
	if err != nil {
		return x, err
	}
	s.deliver(ctx, x.ID) // right away, so the owner sees the result
	return s.DB.GetDelivery(ctx, x.ID)
}

func (s *Service) webhookEvent(p store.Project, a Actor, typ, subject string, data any) WebhookEvent {
	e := WebhookEvent{Version: WebhookVersion, ID: newID(), Type: typ, CreatedAt: time.Now().UTC(), Actor: a.String(), Subject: subject, Data: data}
	e.Project.ID, e.Project.Slug, e.Project.Name = p.ID, p.Slug, p.Name
	return e
}

func (s *Service) enqueueDelivery(ctx context.Context, h store.Webhook, e WebhookEvent) (store.Delivery, error) {
	body, err := json.Marshal(e)
	if err != nil {
		return store.Delivery{}, err
	}
	x := store.Delivery{ID: newID(), WebhookID: h.ID, EventType: e.Type, Body: string(body)}
	if err := s.DB.CreateDelivery(ctx, x); err != nil {
		return x, err
	}
	return x, nil
}

// emitWebhooks queues deliveries for the webhooks that want an event.
func (s *Service) emitWebhooks(ctx context.Context, a Actor, kind, subject string, payload any) {
	typ, ok := webhookEvents[kind]
	if !ok || s.Box == nil {
		return
	}
	p, err := s.DB.ProjectOfObject(ctx, subject)
	if err != nil {
		return
	}
	hooks, err := s.DB.Webhooks(ctx, p.ID)
	if err != nil || len(hooks) == 0 {
		return
	}
	e := s.webhookEvent(p, a, typ, subject, payload)
	for _, h := range hooks {
		if !h.Active || (!slices.Contains(h.Events, "*") && !slices.Contains(h.Events, typ)) {
			continue
		}
		x, err := s.enqueueDelivery(ctx, h, e)
		if err != nil {
			slog.Warn("queueing a webhook failed", "hook", h.ID, "err", err)
			continue
		}
		if s.Jobs == nil {
			s.deliver(ctx, x.ID)
		} else if err := s.Jobs.Add(ctx, JobWebhook, map[string]string{"delivery": x.ID}); err != nil {
			slog.Warn("queueing a webhook failed", "hook", h.ID, "err", err)
		}
	}
}

// Sign returns the Phoedox-Signature header: t=<unix seconds>,v1=<hex
// HMAC-SHA256 of "<t>.<body>" with the webhook secret>.
func Sign(secret string, t time.Time, body []byte) string {
	ts := strconv.FormatInt(t.Unix(), 10)
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(ts + "."))
	m.Write(body)
	return "t=" + ts + ",v1=" + hex.EncodeToString(m.Sum(nil))
}

var errDelivery = errors.New("webhook delivery failed")

// deliver sends one delivery; the error makes the job try again later.
func (s *Service) deliver(ctx context.Context, id string) error {
	x, err := s.DB.GetDelivery(ctx, id)
	if err != nil {
		return jobs.Permanent(err)
	}
	h, err := s.DB.GetWebhook(ctx, x.WebhookID)
	if err != nil || !h.Active {
		return nil // deleted or switched off meanwhile
	}
	secret, err := s.Box.Open(h.Secret, "webhook:"+h.ID)
	if err != nil {
		return jobs.Permanent(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.URL, bytes.NewReader([]byte(x.Body)))
	if err != nil {
		return jobs.Permanent(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Phoedox-Webhooks/1")
	req.Header.Set("Phoedox-Event", x.EventType)
	req.Header.Set("Phoedox-Delivery", x.ID)
	req.Header.Set("Phoedox-Signature", Sign(string(secret), time.Now(), []byte(x.Body)))
	client := s.RequestPolicy.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	start := time.Now()
	res, err := client.Do(req)
	x.DurationMs = int(time.Since(start).Milliseconds())
	if err == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
		res.Body.Close()
		x.Response = res.StatusCode
		if res.StatusCode >= 200 && res.StatusCode < 300 {
			x.Status, x.Error, x.DeliveredAt = "ok", "", store.Now()
			_ = s.DB.FinishDelivery(ctx, x)
			_ = s.DB.HookOK(ctx, h.ID)
			return nil
		}
		err = fmt.Errorf("HTTP %d", res.StatusCode)
	}
	x.Status, x.Error = "failed", err.Error()
	_ = s.DB.FinishDelivery(ctx, x)
	_ = s.DB.HookFailed(ctx, h.ID, 20)
	return fmt.Errorf("%w: %v", errDelivery, err)
}

// registerWebhookJobs connects deliveries to the queue.
func (s *Service) registerWebhookJobs(q *jobs.Queue) {
	q.Handle(JobWebhook, func(ctx context.Context, payload json.RawMessage) error {
		var p struct {
			Delivery string `json:"delivery"`
		}
		if err := json.Unmarshal(payload, &p); err != nil {
			return jobs.Permanent(err)
		}
		return s.deliver(ctx, p.Delivery)
	})
}
