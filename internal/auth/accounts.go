package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/graph"
	phxmail "github.com/phoedox/phoedox/internal/mail"
	"github.com/phoedox/phoedox/internal/store"
	"github.com/phoedox/phoedox/resources"
)

// Link lifetimes.
const (
	InviteTTL     = 7 * 24 * time.Hour
	ResetTTL      = time.Hour      // requested by the user via email
	AdminResetTTL = 24 * time.Hour // created by an admin and passed on by hand
)

// MinPasswordLen is the minimum password length.
const MinPasswordLen = 10

// NewPasswordHash checks the length rule and hashes a new password.
func NewPasswordHash(pw string) (string, error) {
	if len([]rune(pw)) < MinPasswordLen {
		return "", core.Invalid("validation.password_short", map[string]any{"min": MinPasswordLen}, "password must have at least %d characters", MinPasswordLen)
	}
	return HashPassword(pw)
}

// requireAdmin allows system admins via web, API token or CLI – not AI clients.
func requireAdmin(a core.Actor) error {
	if !a.User.IsAdmin || a.Via == "mcp" || a.Restricted() {
		return core.ErrForbidden
	}
	return nil
}

func normalizeEmail(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Address != s || addr.Name != "" {
		return "", core.Invalid("validation.email", nil, "invalid email address")
	}
	return s, nil
}

func validLocale(l string) error {
	switch l {
	case "", "de", "en":
		return nil
	}
	return core.Invalid("validation.locale", nil, "locale must be de, en or empty")
}

// LinkResult is a created invitation or reset link. Link is shown to admins
// so they can pass it on when no email is configured.
type LinkResult struct {
	User      store.User `json:"user"`
	Purpose   string     `json:"purpose"`
	Link      string     `json:"link"`
	Mailed    bool       `json:"mailed"`
	ExpiresAt store.Time `json:"expiresAt"`
}

// Users lists all accounts (admins only).
func (a *Auth) Users(ctx context.Context, actor core.Actor) ([]store.User, error) {
	if err := requireAdmin(actor); err != nil {
		return nil, err
	}
	return a.DB.ListUsers(ctx)
}

// InviteInput creates an account without password.
type InviteInput struct {
	Email  string `json:"email"`
	Name   string `json:"name"`
	Admin  bool   `json:"admin"`
	Locale string `json:"locale"`
}

// Invite creates an account and an invitation link (emailed if possible).
func (a *Auth) Invite(ctx context.Context, actor core.Actor, in InviteInput, base string) (LinkResult, error) {
	if err := requireAdmin(actor); err != nil {
		return LinkResult{}, err
	}
	email, err := normalizeEmail(in.Email)
	if err != nil {
		return LinkResult{}, err
	}
	if err := validLocale(in.Locale); err != nil {
		return LinkResult{}, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name, _, _ = strings.Cut(email, "@")
	}
	u, err := a.DB.CreateUser(ctx, store.User{ID: string(graph.NewID()), Email: email, Name: name, IsAdmin: in.Admin, Locale: in.Locale})
	if errors.Is(err, store.ErrConflict) {
		return LinkResult{}, core.Invalid("validation.email_taken", nil, "a user with this email exists")
	}
	if err != nil {
		return LinkResult{}, err
	}
	_ = a.DB.AddEvent(ctx, actor.String(), "user.invite", u.ID, map[string]any{"admin": in.Admin})
	return a.issueLink(ctx, actor, u, store.LinkInvite, InviteTTL, base)
}

// SignupInput is a self sign-up (ADR-0057).
type SignupInput struct {
	Email  string `json:"email"`
	Name   string `json:"name"`
	Locale string `json:"locale"`
	// Accept confirms the terms and privacy policy when the instance names them.
	Accept bool `json:"accept"`
}

// SignupTTL is how long the confirmation link of a sign-up stays valid.
const SignupTTL = 48 * time.Hour

// Signup registers an account with a confirmation link sent by mail. The
// account has no password until the link is used, so an address cannot be
// taken over by someone who does not receive its mail. The answer is the
// same whether or not the address is known: a known address gets a reset
// link (or, without a password yet, a new confirmation link) instead.
// requireTerms is set when the instance names terms or a privacy policy.
func (a *Auth) Signup(ctx context.Context, in SignupInput, base string, requireTerms bool) error {
	email, err := normalizeEmail(in.Email)
	if err != nil {
		return err
	}
	if err := validLocale(in.Locale); err != nil {
		return err
	}
	if requireTerms && !in.Accept {
		return core.Invalid("validation.terms_required", nil, "accept the terms and the privacy policy")
	}
	name := strings.TrimSpace(in.Name)
	if len([]rune(name)) > 100 {
		return core.Invalid("validation.name_too_long", nil, "the name is too long")
	}
	if name == "" {
		name, _, _ = strings.Cut(email, "@")
	}
	if u, err := a.DB.UserByEmail(ctx, email); err == nil {
		return a.signupKnown(ctx, u, base)
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	u, err := a.DB.CreateUser(ctx, store.User{ID: string(graph.NewID()), Email: email, Name: name, Locale: in.Locale})
	if errors.Is(err, store.ErrConflict) {
		if u, err := a.DB.UserByEmail(ctx, email); err == nil {
			return a.signupKnown(ctx, u, base)
		}
		return nil
	}
	if err != nil {
		return err
	}
	self := core.Actor{User: u, Via: "web"}
	_ = a.DB.AddEvent(ctx, self.String(), "user.signup", u.ID, map[string]any{"acceptedTerms": in.Accept})
	_, err = a.issueLinkMail(ctx, self, u, store.LinkInvite, "signup", SignupTTL, base)
	return err
}

// signupKnown answers a sign-up for an address that has an account.
func (a *Auth) signupKnown(ctx context.Context, u store.User, base string) error {
	if u.Disabled || u.Deleted {
		return nil
	}
	self := core.Actor{User: u, Via: "web"}
	if u.HasPassword {
		_, err := a.issueLink(ctx, self, u, store.LinkReset, ResetTTL, base)
		return err
	}
	_, err := a.issueLinkMail(ctx, self, u, store.LinkInvite, "signup", SignupTTL, base)
	return err
}

// UserPatch changes an account; nil fields stay.
type UserPatch struct {
	Name     *string `json:"name,omitempty"`
	Admin    *bool   `json:"admin,omitempty"`
	Disabled *bool   `json:"disabled,omitempty"`
	Locale   *string `json:"locale,omitempty"`
}

// UpdateUser changes another account (admins only). The last active admin
// cannot be demoted or disabled, and admins cannot lock themselves out.
func (a *Auth) UpdateUser(ctx context.Context, actor core.Actor, id string, p UserPatch) (store.User, error) {
	if err := requireAdmin(actor); err != nil {
		return store.User{}, err
	}
	u, err := a.DB.UserByID(ctx, id)
	if err != nil {
		return u, err
	}
	wasActiveAdmin := u.IsAdmin && !u.Disabled
	if p.Name != nil {
		if u.Name = strings.TrimSpace(*p.Name); u.Name == "" {
			return u, core.Invalid("validation.name_required", nil, "name is required")
		}
	}
	if p.Locale != nil {
		if err := validLocale(*p.Locale); err != nil {
			return u, err
		}
		u.Locale = *p.Locale
	}
	if p.Admin != nil {
		u.IsAdmin = *p.Admin
	}
	if p.Disabled != nil {
		u.Disabled = *p.Disabled
	}
	if u.ID == actor.User.ID && (!u.IsAdmin || u.Disabled) {
		return u, core.Invalid("validation.self_lockout", nil, "you cannot remove your own admin rights or disable yourself")
	}
	if wasActiveAdmin && (!u.IsAdmin || u.Disabled) {
		if n, err := a.DB.CountActiveAdmins(ctx); err != nil {
			return u, err
		} else if n <= 1 {
			return u, core.Invalid("validation.last_admin", nil, "the last admin cannot be removed")
		}
	}
	if err := a.DB.UpdateUser(ctx, u); err != nil {
		return u, err
	}
	_ = a.DB.AddEvent(ctx, actor.String(), "user.update", u.ID, p)
	return u, nil
}

// NewAccountLink creates a fresh invitation (no password yet) or reset link
// for a user (admins only). Older unused links of that kind stop working.
func (a *Auth) NewAccountLink(ctx context.Context, actor core.Actor, id, base string) (LinkResult, error) {
	if err := requireAdmin(actor); err != nil {
		return LinkResult{}, err
	}
	u, err := a.DB.UserByID(ctx, id)
	if err != nil {
		return LinkResult{}, err
	}
	if u.Disabled {
		return LinkResult{}, core.Invalid("validation.user_disabled", nil, "the user is disabled")
	}
	if !u.HasPassword {
		return a.issueLink(ctx, actor, u, store.LinkInvite, InviteTTL, base)
	}
	return a.issueLink(ctx, actor, u, store.LinkReset, AdminResetTTL, base)
}

// RequestReset emails a reset link if the address belongs to an active user
// with a password. It never reveals whether the address exists.
func (a *Auth) RequestReset(ctx context.Context, email, base string) error {
	if a.Mail == nil || !a.Mail.Enabled() {
		return nil
	}
	u, err := a.DB.UserByEmail(ctx, email)
	if err != nil || !u.HasPassword {
		return nil
	}
	_, err = a.issueLink(ctx, core.Actor{User: u, Via: "web"}, u, store.LinkReset, ResetTTL, base)
	return err
}

func (a *Auth) issueLink(ctx context.Context, actor core.Actor, u store.User, purpose string, ttl time.Duration, base string) (LinkResult, error) {
	return a.issueLinkMail(ctx, actor, u, purpose, purpose, ttl, base)
}

// issueLinkMail creates a link and mails it with the given template.
func (a *Auth) issueLinkMail(ctx context.Context, actor core.Actor, u store.User, purpose, template string, ttl time.Duration, base string) (LinkResult, error) {
	plain, hash := NewSecret("")
	exp := store.Now()
	exp.Time = exp.Time.Add(ttl)
	if _, err := a.DB.CreateLink(ctx, store.AccountLink{ID: hash, UserID: u.ID, Purpose: purpose, CreatedBy: actor.String(), ExpiresAt: exp}); err != nil {
		return LinkResult{}, err
	}
	res := LinkResult{User: u, Purpose: purpose, Link: base + "/" + purpose + "/" + plain, ExpiresAt: exp}
	if a.Mail != nil && a.Mail.Enabled() {
		locale := u.Locale
		if locale == "" {
			locale = a.DefaultLocale
		}
		inviter := actor.User.Name
		subject, body, err := resources.Mail(template, locale, map[string]any{
			"Name": u.Name, "Inviter": inviter, "Instance": hostOf(base), "Link": res.Link, "Hours": int(ttl.Hours()),
		})
		if err == nil {
			err = a.Mail.Send(ctx, phxmail.Message{To: u.Email, Subject: subject, Body: body})
		}
		if err != nil {
			slog.Warn("sending account mail failed", "purpose", purpose, "user", u.ID, "err", err)
		} else {
			res.Mailed = true
		}
	}
	_ = a.DB.AddEvent(ctx, actor.String(), "user.link", u.ID, map[string]any{"purpose": purpose, "template": template, "mailed": res.Mailed})
	return res, nil
}

func hostOf(base string) string {
	if u, err := url.Parse(base); err == nil && u.Host != "" {
		return u.Host
	}
	return base
}

// LinkInfo is what the set-password page shows about a link.
type LinkInfo struct {
	Purpose   string     `json:"purpose"`
	Email     string     `json:"email"`
	Name      string     `json:"name"`
	ExpiresAt store.Time `json:"expiresAt"`
}

func linkInvalid() error {
	return core.Invalid("validation.link_invalid", nil, "this link is invalid, expired or was already used")
}

// Link returns information about a valid link.
func (a *Auth) Link(ctx context.Context, token string) (LinkInfo, error) {
	l, err := a.DB.ValidLink(ctx, HashSecret(token))
	if errors.Is(err, store.ErrNotFound) {
		return LinkInfo{}, linkInvalid()
	}
	if err != nil {
		return LinkInfo{}, err
	}
	u, err := a.DB.UserByID(ctx, l.UserID)
	if err != nil {
		return LinkInfo{}, err
	}
	return LinkInfo{Purpose: l.Purpose, Email: u.Email, Name: u.Name, ExpiresAt: l.ExpiresAt}, nil
}

// CompleteLink sets the password from an invitation or reset link, ends all
// other sessions and signs the user in.
func (a *Auth) CompleteLink(ctx context.Context, token, password string) (*http.Cookie, store.User, error) {
	hash, err := NewPasswordHash(password)
	if err != nil {
		return nil, store.User{}, err
	}
	l, err := a.DB.UseLink(ctx, HashSecret(token), hash)
	if errors.Is(err, store.ErrNotFound) {
		return nil, store.User{}, linkInvalid()
	}
	if err != nil {
		return nil, store.User{}, err
	}
	u, err := a.DB.UserByID(ctx, l.UserID)
	if err != nil {
		return nil, u, err
	}
	_ = a.DB.AddEvent(ctx, "user:"+u.ID, "auth.password_set", u.ID, map[string]string{"via": l.Purpose})
	c, err := a.startSession(ctx, u)
	return c, u, err
}

// ProfilePatch changes the own account.
type ProfilePatch struct {
	Name   *string `json:"name,omitempty"`
	Locale *string `json:"locale,omitempty"`
}

// UpdateProfile changes name and language of the actor.
func (a *Auth) UpdateProfile(ctx context.Context, actor core.Actor, p ProfilePatch) (store.User, error) {
	u, err := a.DB.UserByID(ctx, actor.User.ID)
	if err != nil {
		return u, err
	}
	if p.Name != nil {
		if u.Name = strings.TrimSpace(*p.Name); u.Name == "" {
			return u, core.Invalid("validation.name_required", nil, "name is required")
		}
	}
	if p.Locale != nil {
		if err := validLocale(*p.Locale); err != nil {
			return u, err
		}
		u.Locale = *p.Locale
	}
	if err := a.DB.UpdateUser(ctx, u); err != nil {
		return u, err
	}
	_ = a.DB.AddEvent(ctx, actor.String(), "user.profile", u.ID, p)
	return u, nil
}

// ChangePassword sets a new password after checking the current one and
// ends all other sessions (keepSession is the hash of the current one).
func (a *Auth) ChangePassword(ctx context.Context, actor core.Actor, current, next, keepSession string) error {
	u, err := a.DB.UserByID(ctx, actor.User.ID)
	if err != nil {
		return err
	}
	if u.HasPassword && !VerifyPassword(u.PasswordHash, current) {
		return core.Invalid("validation.password_wrong", nil, "the current password is wrong")
	}
	hash, err := NewPasswordHash(next)
	if err != nil {
		return err
	}
	if err := a.DB.SetPassword(ctx, u.ID, hash); err != nil {
		return err
	}
	_ = a.DB.DeleteUserSessions(ctx, u.ID, keepSession)
	_ = a.DB.AddEvent(ctx, actor.String(), "auth.password_change", u.ID, nil)
	return nil
}

// SessionID returns the stored hash of the request's session cookie.
func SessionID(r *http.Request) string {
	if c, err := r.Cookie(CookieName); err == nil {
		return HashSecret(c.Value)
	}
	return ""
}

// DeletionNeeds is the confirmation deleting an account needs.
type DeletionNeeds struct {
	NeedsPassword    bool `json:"needsPassword"`
	NeedsCode        bool `json:"needsCode"`
	NeedsRecentLogin bool `json:"needsRecentLogin"`
}

// recentLogin is how fresh a session must be to delete an account without
// password or authenticator (OIDC, passkey or Home Assistant sign-in).
const recentLogin = 10 * time.Minute

// DeletionNeeds returns how the person confirms deleting their account.
func (a *Auth) DeletionNeeds(ctx context.Context, userID string) (DeletionNeeds, error) {
	u, err := a.DB.UserByID(ctx, userID)
	if err != nil {
		return DeletionNeeds{}, err
	}
	n := DeletionNeeds{NeedsPassword: u.HasPassword, NeedsCode: a.totpActive(ctx, u.ID)}
	n.NeedsRecentLogin = !n.NeedsPassword && !n.NeedsCode
	return n, nil
}

// DeleteInput confirms deleting one's own account.
type DeleteInput struct {
	Password string `json:"password"`
	Code     string `json:"code"`
	Email    string `json:"email"`
}

// ConfirmDeletion checks that the signed-in person typed their e-mail
// address and confirmed with password and/or authenticator code (or,
// without both, signed in within the last minutes).
func (a *Auth) ConfirmDeletion(ctx context.Context, actor core.Actor, session string, in DeleteInput) error {
	if actor.Via != "web" && actor.Via != "" {
		return core.ErrForbidden
	}
	u, err := a.DB.UserByID(ctx, actor.User.ID)
	if err != nil {
		return err
	}
	needs, err := a.DeletionNeeds(ctx, u.ID)
	if err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(in.Email), u.Email) {
		return core.Invalid("validation.email_mismatch", nil, "type your e-mail address to confirm")
	}
	if needs.NeedsPassword && !VerifyPassword(u.PasswordHash, in.Password) {
		return core.Invalid("validation.password_wrong", nil, "the current password is wrong")
	}
	if needs.NeedsCode {
		if err := a.checkTOTP(ctx, u.ID, in.Code, false); err != nil {
			return err
		}
	}
	if needs.NeedsRecentLogin {
		started, err := a.DB.SessionCreated(ctx, session)
		if err != nil || time.Since(started.Time) > recentLogin {
			return core.Invalid("validation.reauth_needed", nil, "sign in again, then delete the account within 10 minutes")
		}
	}
	return nil
}
