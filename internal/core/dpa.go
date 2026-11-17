package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/phoedox/phoedox/internal/pdftext"
	"github.com/phoedox/phoedox/internal/store"
	"github.com/phoedox/phoedox/resources"
)

// Data processing agreement (DPA, German AVV): the system admin publishes
// versioned texts (English, German translation); admins of an organization
// accept the current text electronically on its behalf. Each acceptance
// records who, when, which version and a checksum of the exact text, and
// can be downloaded as a PDF. Until a text is published, a placeholder from
// resources/legal applies.

// placeholderDPA is the id of the built-in placeholder text.
const placeholderDPA = "placeholder"

// DPAText is a version of the agreement in one language.
type DPAText struct {
	ID          string     `json:"id"`
	Version     string     `json:"version"`
	Placeholder bool       `json:"placeholder"`
	PublishedAt store.Time `json:"publishedAt"`
	Lang        string     `json:"lang"`
	Body        string     `json:"body"`
}

// DPAInfo is what an organization's settings show.
type DPAInfo struct {
	Current DPAText `json:"current"`
	// Accepted is the acceptance of the current version, if any.
	Accepted *store.DPAAcceptance  `json:"accepted,omitempty"`
	History  []store.DPAAcceptance `json:"history"`
	// CanAccept: the caller is an admin of the organization.
	CanAccept bool   `json:"canAccept"`
	Company   string `json:"company"`
}

// DPAAcceptInput is an acceptance.
type DPAAcceptInput struct {
	VersionID  string `json:"versionId"`
	Lang       string `json:"lang"`
	SignerName string `json:"signerName"`
	SignerRole string `json:"signerRole"`
	Company    string `json:"company"`
	// Confirm: the person confirms acting on behalf of the organization.
	Confirm bool `json:"confirm"`
}

func dpaLang(l string) string {
	if strings.HasPrefix(strings.ToLower(l), "de") {
		return "de"
	}
	return "en"
}

// currentDPA is the newest published text, or the placeholder.
func (s *Service) currentDPA(ctx context.Context, lang string) (DPAText, error) {
	lang = dpaLang(lang)
	v, err := s.DB.CurrentDPAVersion(ctx)
	if err == store.ErrNotFound {
		body, err := resources.Legal("dpa", lang, nil)
		return DPAText{ID: placeholderDPA, Version: "0", Placeholder: true, Lang: lang, Body: body}, err
	}
	if err != nil {
		return DPAText{}, err
	}
	body := v.BodyEN
	if lang == "de" && strings.TrimSpace(v.BodyDE) != "" {
		body = v.BodyDE
	} else {
		lang = "en"
	}
	return DPAText{ID: v.ID, Version: v.Version, PublishedAt: v.PublishedAt, Lang: lang, Body: body}, nil
}

// OrgDPA shows an organization's agreement: the current text, whether it is
// accepted and the history (members of the organization).
func (s *Service) OrgDPA(ctx context.Context, a Actor, ref, lang string) (DPAInfo, error) {
	o, err := s.org(ctx, a, ref, "")
	if err != nil {
		return DPAInfo{}, err
	}
	cur, err := s.currentDPA(ctx, lang)
	if err != nil {
		return DPAInfo{}, err
	}
	hist, err := s.DB.DPAAcceptances(ctx, o.ID)
	if err != nil {
		return DPAInfo{}, err
	}
	out := DPAInfo{Current: cur, History: hist, Company: o.Name}
	for i := range hist {
		if hist[i].VersionID == cur.ID {
			out.Accepted = &hist[i]
			break
		}
	}
	member, _ := s.DB.OrgFor(ctx, o.ID, a.User.ID)
	out.CanAccept = member.Role == store.OrgAdmin && s.fullUser(a) == nil
	return out, nil
}

// AcceptDPA accepts the current text for the organization (its admins, in
// person – not AI clients or tokens).
func (s *Service) AcceptDPA(ctx context.Context, a Actor, ref string, in DPAAcceptInput) (store.DPAAcceptance, error) {
	if err := s.fullUser(a); err != nil || a.Via == "token" || a.Plugin != "" {
		return store.DPAAcceptance{}, ErrForbidden
	}
	o, err := s.DB.OrgFor(ctx, ref, a.User.ID)
	if err != nil {
		return store.DPAAcceptance{}, err
	}
	if o.Role != store.OrgAdmin {
		return store.DPAAcceptance{}, ErrForbidden
	}
	cur, err := s.currentDPA(ctx, in.Lang)
	if err != nil {
		return store.DPAAcceptance{}, err
	}
	if in.VersionID != cur.ID {
		return store.DPAAcceptance{}, invalid("validation.dpa_outdated", nil, "a newer version of the agreement was published; read it first")
	}
	in.SignerName, in.SignerRole, in.Company = strings.TrimSpace(in.SignerName), strings.TrimSpace(in.SignerRole), strings.TrimSpace(in.Company)
	if in.Company == "" {
		in.Company = o.Name
	}
	if in.SignerName == "" || utf8.RuneCountInString(in.SignerName) > 120 || utf8.RuneCountInString(in.SignerRole) > 120 || utf8.RuneCountInString(in.Company) > 200 {
		return store.DPAAcceptance{}, invalid("validation.dpa_signer", nil, "enter your name (and optionally your position)")
	}
	if !in.Confirm {
		return store.DPAAcceptance{}, invalid("validation.dpa_confirm", nil, "confirm that you accept on behalf of the organization")
	}
	sum := sha256.Sum256([]byte(cur.Body))
	acc := store.DPAAcceptance{ID: newID(), OrgID: o.ID, VersionID: cur.ID, Version: cur.Version, Lang: cur.Lang, TextSHA256: hex.EncodeToString(sum[:]),
		UserID: a.User.ID, SignerName: in.SignerName, SignerRole: in.SignerRole, Company: in.Company, AcceptedAt: store.Now()}
	if err := s.DB.AddDPAAcceptance(ctx, acc); err != nil {
		return acc, err
	}
	s.event(ctx, a, "org.dpa_accept", o.ID, map[string]any{"version": acc.Version, "versionId": acc.VersionID, "lang": acc.Lang, "sha256": acc.TextSHA256,
		"signer": acc.SignerName, "role": acc.SignerRole, "company": acc.Company})
	return acc, nil
}

// DPAPDF is an accepted agreement as PDF: the exact text and the record of
// the acceptance (members of the organization).
func (s *Service) DPAPDF(ctx context.Context, a Actor, ref, id string) ([]byte, string, error) {
	o, err := s.org(ctx, a, ref, "")
	if err != nil {
		return nil, "", err
	}
	acc, err := s.DB.GetDPAAcceptance(ctx, id)
	if err != nil || acc.OrgID != o.ID {
		return nil, "", ErrNotFound
	}
	body := ""
	if acc.VersionID == placeholderDPA {
		body, err = resources.Legal("dpa", acc.Lang, nil)
	} else {
		var v store.DPAVersion
		if v, err = s.DB.GetDPAVersion(ctx, acc.VersionID); err == nil {
			body = v.BodyEN
			if acc.Lang == "de" && strings.TrimSpace(v.BodyDE) != "" {
				body = v.BodyDE
			}
		}
	}
	if err != nil {
		return nil, "", err
	}
	email := ""
	if u, err := s.DB.UserByID(ctx, acc.UserID); err == nil {
		email = u.Email
	}
	date := acc.AcceptedAt.UTC().Format("2006-01-02 15:04 UTC")
	if acc.Lang == "de" {
		date = acc.AcceptedAt.UTC().Format("02.01.2006, 15:04 UTC")
	}
	record, err := resources.Legal("dpa_acceptance", acc.Lang, map[string]any{"Company": acc.Company, "Signer": acc.SignerName, "Role": acc.SignerRole,
		"Email": email, "Date": date, "Version": acc.Version, "Checksum": acc.TextSHA256, "ID": acc.ID})
	if err != nil {
		return nil, "", err
	}
	// The checksum covers the text as accepted; a changed text no longer matches.
	if sum := sha256.Sum256([]byte(body)); hex.EncodeToString(sum[:]) != acc.TextSHA256 {
		return nil, "", fmt.Errorf("dpa: the stored text does not match the accepted checksum")
	}
	pdf := pdftext.Render(body+"\n\n"+record, "DPA "+acc.Company, acc.Company+" · "+acc.Version+" · "+acc.AcceptedAt.UTC().Format(time.DateOnly))
	return pdf, fmt.Sprintf("dpa-%s-%s.pdf", o.Slug, acc.AcceptedAt.UTC().Format("2006-01-02")), nil
}

// DPAVersionInput publishes a new text (system admins).
type DPAVersionInput struct {
	Version string `json:"version"`
	BodyEN  string `json:"bodyEn"`
	BodyDE  string `json:"bodyDe"`
}

// DPAAdmin is what the administration shows.
type DPAAdmin struct {
	Versions []store.DPAVersion `json:"versions"`
	// Placeholder: no text published yet; organizations see the placeholder.
	Placeholder   bool   `json:"placeholder"`
	PlaceholderEN string `json:"placeholderEn"`
	PlaceholderDE string `json:"placeholderDe"`
}

// AdminDPA lists the published texts (system admins).
func (s *Service) AdminDPA(ctx context.Context, a Actor) (DPAAdmin, error) {
	if err := s.adminOnly(a); err != nil {
		return DPAAdmin{}, err
	}
	vs, err := s.DB.DPAVersions(ctx)
	if err != nil {
		return DPAAdmin{}, err
	}
	en, _ := resources.Legal("dpa", "en", nil)
	de, _ := resources.Legal("dpa", "de", nil)
	return DPAAdmin{Versions: vs, Placeholder: len(vs) == 0, PlaceholderEN: en, PlaceholderDE: de}, nil
}

// PublishDPA publishes a new version; organizations accept it again
// (system admins).
func (s *Service) PublishDPA(ctx context.Context, a Actor, in DPAVersionInput) (store.DPAVersion, error) {
	if err := s.adminOnly(a); err != nil {
		return store.DPAVersion{}, err
	}
	in.Version, in.BodyEN, in.BodyDE = strings.TrimSpace(in.Version), strings.TrimSpace(in.BodyEN), strings.TrimSpace(in.BodyDE)
	if in.Version == "" || utf8.RuneCountInString(in.Version) > 40 {
		return store.DPAVersion{}, invalid("validation.dpa_version", nil, "give the version a short name, e.g. 2026-11")
	}
	if in.BodyEN == "" || len(in.BodyEN) > 400_000 || len(in.BodyDE) > 400_000 {
		return store.DPAVersion{}, invalid("validation.dpa_body", nil, "the English text is required (at most 400 kB per language)")
	}
	vs, _ := s.DB.DPAVersions(ctx)
	for _, v := range vs {
		if v.Version == in.Version {
			return store.DPAVersion{}, invalid("validation.dpa_version_taken", map[string]any{"version": in.Version}, "version %q exists", in.Version)
		}
	}
	v := store.DPAVersion{ID: newID(), Version: in.Version, BodyEN: in.BodyEN, BodyDE: in.BodyDE, PublishedAt: store.Now(), CreatedBy: a.User.ID}
	if err := s.DB.AddDPAVersion(ctx, v); err != nil {
		return v, err
	}
	s.event(ctx, a, "instance.dpa_publish", v.ID, map[string]any{"version": v.Version})
	return v, nil
}
