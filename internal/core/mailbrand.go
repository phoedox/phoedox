package core

import (
	"context"
	"strings"

	"github.com/phoedox/phoedox/internal/mail"
)

// MailBrand is the look of e-mails: the organization's theme (when the
// message belongs to one), else the instance theme – logo, accent colour
// and name. The mail sender asks for it per message.
func (s *Service) MailBrand(ctx context.Context, orgID string) mail.Brand {
	b := mail.Brand{Name: "Phoedox", Site: s.PublicURL}
	themeID := ""
	if orgID != "" {
		if o, err := s.DB.OrgFor(ctx, orgID, ""); err == nil {
			b.Name = o.Name
			themeID, _, _ = s.DB.OrgTheme(ctx, o.ID)
		}
	}
	if themeID == "" {
		themeID, _ = s.DB.Setting(ctx, settingThemeDefault, "")
	}
	if themeID == "" {
		return b
	}
	t, err := s.DB.GetTheme(ctx, themeID)
	if err != nil {
		return b
	}
	d := themeData(t)
	b.Accent = d.Light["accent"]
	switch {
	case strings.HasPrefix(d.Logo, "https://"):
		b.Logo = d.Logo
	case strings.HasPrefix(d.Logo, "/api/v1/") && strings.HasPrefix(s.PublicURL, "https://"):
		b.Logo = s.PublicURL + d.Logo
	}
	return b
}

// projectOrg is the organization of a project ("" for personal projects).
func (s *Service) projectOrg(ctx context.Context, projectID string) string {
	if projectID == "" {
		return ""
	}
	p, err := s.DB.ProjectFor(ctx, projectID, "")
	if err != nil {
		return ""
	}
	return p.OrgID
}
