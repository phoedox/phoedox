package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/phoedox/phoedox/internal/botcheck"
	"github.com/phoedox/phoedox/internal/mail"
	"github.com/phoedox/phoedox/resources"
)

// Bot protection (internal/botcheck) wired to Phoedox: the e-mail code goes
// out with the instance's mailer, spikes of blocked requests notify system
// admins, and system admins change the configuration.

// NewBotCheck creates the bot protection for this service.
func (s *Service) NewBotCheck() *botcheck.Service {
	b := botcheck.New(s.DB, s.Box, s.sendBotCode)
	if s.Mail == nil || !s.Mail.Enabled() {
		b.Mail = nil
	}
	b.Alert = s.botAlert
	return b
}

func (s *Service) sendBotCode(ctx context.Context, to, code, locale string) error {
	if locale != "de" && locale != "en" {
		locale = s.DefaultLocale
	}
	host := s.PublicURL
	if u, err := url.Parse(s.PublicURL); err == nil && u.Host != "" {
		host = u.Host
	}
	subject, body, err := resources.Mail("botcode", locale, map[string]any{"Code": code, "Instance": host})
	if err != nil {
		return err
	}
	return s.Mail.Send(ctx, mail.Message{To: to, Subject: subject, Body: body})
}

func (s *Service) botAlert(ctx context.Context, blocked int, place string) {
	users, err := s.DB.ListUsers(ctx)
	if err != nil {
		return
	}
	slog.Warn("bot check: many blocked requests", "lastHour", blocked, "place", place)
	for _, u := range users {
		if u.IsAdmin {
			s.notify(ctx, u, Note{Kind: "bot.spike", Title: fmt.Sprintf("%d · %s", blocked, place), Link: "/admin/bot-protection"})
		}
	}
}

// BotSettings is the configuration with the last 7 days (system admins).
type BotSettings struct {
	Config botcheck.Config `json:"config"`
	Stats  botcheck.Stats  `json:"stats"`
	Mail   bool            `json:"mail"`
}

func (s *Service) botAdmin(a Actor) error {
	if !a.User.IsAdmin || a.Via == "mcp" {
		return ErrForbidden
	}
	if s.Bot == nil {
		return invalid("validation.bot_unavailable", nil, "bot protection is not available")
	}
	return nil
}

// BotSettingsOf reads the bot protection settings (system admins).
func (s *Service) BotSettingsOf(ctx context.Context, a Actor) (BotSettings, error) {
	if err := s.botAdmin(a); err != nil {
		return BotSettings{}, err
	}
	st, err := s.Bot.Stats(ctx)
	if err != nil {
		return BotSettings{}, err
	}
	return BotSettings{Config: botcheck.Public(s.Bot.Config(ctx)), Stats: st, Mail: s.Bot.Mail != nil}, nil
}

// SaveBotSettings stores the bot protection configuration (system admins).
func (s *Service) SaveBotSettings(ctx context.Context, a Actor, c botcheck.Config) (BotSettings, error) {
	if err := s.botAdmin(a); err != nil {
		return BotSettings{}, err
	}
	if _, err := s.Bot.SaveConfig(ctx, c, a.String()); err != nil {
		var ve *botcheck.ValidationError
		if errors.As(err, &ve) {
			return BotSettings{}, invalid(ve.Key, ve.Params, "%s", ve.Key)
		}
		return BotSettings{}, err
	}
	s.event(ctx, a, "admin.bot_protection", "", botcheck.Public(c))
	return s.BotSettingsOf(ctx, a)
}
