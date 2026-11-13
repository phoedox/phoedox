package auth

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/store"
)

// Second factors: TOTP (with recovery codes) is the switch that makes a
// password login need a second step. Passkeys can complete that step and
// can also sign in on their own (with user verification on the device).

const (
	challengeTTL      = 5 * time.Minute
	maxFactorAttempts = 5
	recoveryCodeCount = 10

	kindSecondFactor = "second_factor"
	kindPasskey2FA   = "webauthn_2fa"
	kindPasskeyLogin = "webauthn_login"
	kindPasskeyReg   = "webauthn_register"
)

// LoginStep is the result of a password login: either a session or a
// pending second factor (Challenge + Methods).
type LoginStep struct {
	Cookie    *http.Cookie `json:"-"`
	User      *store.User  `json:"user,omitempty"`
	Challenge string       `json:"challenge,omitempty"`
	Methods   []string     `json:"methods,omitempty"` // totp, passkey, recovery
}

func codeInvalid() error {
	return core.Invalid("validation.totp_code", nil, "the code is wrong")
}

func challengeInvalid() error {
	return core.Invalid("validation.challenge_invalid", nil, "the sign-in expired, please start again")
}

func (a *Auth) newChallenge(ctx context.Context, userID, kind, data string) (string, error) {
	plain, hash := NewSecret("")
	exp := store.Now()
	exp.Time = exp.Time.Add(challengeTTL)
	return plain, a.DB.CreateChallenge(ctx, store.Challenge{ID: hash, UserID: userID, Kind: kind, Data: data}, exp)
}

// takeChallenge loads a challenge and enforces the attempt limit.
func (a *Auth) takeChallenge(ctx context.Context, token, kind string) (store.Challenge, error) {
	c, err := a.DB.GetChallenge(ctx, HashSecret(token), kind)
	if errors.Is(err, store.ErrNotFound) {
		return c, challengeInvalid()
	}
	if err != nil {
		return c, err
	}
	if c.Attempts >= maxFactorAttempts {
		_ = a.DB.DeleteChallenge(ctx, c.ID)
		return c, challengeInvalid()
	}
	return c, nil
}

func (a *Auth) totpActive(ctx context.Context, userID string) bool {
	t, err := a.DB.GetTOTP(ctx, userID)
	return err == nil && t.Confirmed
}

// passwordStep turns a verified password into a session or a 2FA challenge.
func (a *Auth) passwordStep(ctx context.Context, u store.User) (LoginStep, error) {
	if !a.totpActive(ctx, u.ID) {
		c, err := a.startSession(ctx, u)
		if err == nil {
			_ = a.DB.AddEvent(ctx, "user:"+u.ID, "auth.login", u.ID, map[string]string{"method": "password"})
		}
		return LoginStep{Cookie: c, User: &u}, err
	}
	token, err := a.newChallenge(ctx, u.ID, kindSecondFactor, "")
	if err != nil {
		return LoginStep{}, err
	}
	methods := []string{"totp"}
	if pks, _ := a.DB.Passkeys(ctx, u.ID); len(pks) > 0 {
		methods = append(methods, "passkey")
	}
	return LoginStep{Challenge: token, Methods: append(methods, "recovery")}, nil
}

func (a *Auth) finishSecondFactor(ctx context.Context, c store.Challenge, method string) (*http.Cookie, store.User, error) {
	_ = a.DB.DeleteChallenge(ctx, c.ID)
	u, err := a.DB.UserByID(ctx, c.UserID)
	if err != nil || u.Disabled {
		return nil, u, challengeInvalid()
	}
	cookie, err := a.startSession(ctx, u)
	if err == nil {
		_ = a.DB.AddEvent(ctx, "user:"+u.ID, "auth.login", u.ID, map[string]string{"method": "password+" + method})
	}
	return cookie, u, err
}

func (a *Auth) totpSecret(t store.TOTP) ([]byte, error) {
	if a.Box == nil {
		return nil, errors.New("auth: no master key configured")
	}
	return a.Box.Open(t.SecretEnc, "totp:"+t.UserID)
}

func (a *Auth) checkTOTP(ctx context.Context, userID, code string, confirm bool) error {
	t, err := a.DB.GetTOTP(ctx, userID)
	if errors.Is(err, store.ErrNotFound) {
		return core.Invalid("validation.totp_not_setup", nil, "two-factor authentication is not set up")
	}
	if err != nil {
		return err
	}
	secret, err := a.totpSecret(t)
	if err != nil {
		return err
	}
	step, ok := totpVerify(secret, code, time.Now(), t.LastStep)
	if !ok {
		return codeInvalid()
	}
	if err := a.DB.UseTOTPStep(ctx, userID, step, confirm); errors.Is(err, store.ErrConflict) {
		return codeInvalid() // the same code was just used
	} else if err != nil {
		return err
	}
	return nil
}

// LoginTOTP completes a password login with an authenticator code.
func (a *Auth) LoginTOTP(ctx context.Context, challenge, code string) (*http.Cookie, store.User, error) {
	c, err := a.takeChallenge(ctx, challenge, kindSecondFactor)
	if err != nil {
		return nil, store.User{}, err
	}
	if err := a.checkTOTP(ctx, c.UserID, code, false); err != nil {
		return nil, store.User{}, err
	}
	return a.finishSecondFactor(ctx, c, "totp")
}

func normalizeRecovery(code string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '2' && r <= '7':
			return r
		case r >= 'A' && r <= 'Z':
			return r + 32
		}
		return -1
	}, code)
}

// LoginRecovery completes a password login with a one-time recovery code.
func (a *Auth) LoginRecovery(ctx context.Context, challenge, code string) (*http.Cookie, store.User, error) {
	c, err := a.takeChallenge(ctx, challenge, kindSecondFactor)
	if err != nil {
		return nil, store.User{}, err
	}
	if err := a.DB.UseRecoveryCode(ctx, c.UserID, HashSecret(normalizeRecovery(code))); errors.Is(err, store.ErrNotFound) {
		return nil, store.User{}, core.Invalid("validation.recovery_code", nil, "the recovery code is wrong or was used")
	} else if err != nil {
		return nil, store.User{}, err
	}
	cookie, u, err := a.finishSecondFactor(ctx, c, "recovery")
	if err == nil {
		_ = a.DB.AddEvent(ctx, "user:"+u.ID, "auth.recovery_code_used", u.ID, nil)
	}
	return cookie, u, err
}

// TOTPSetup is shown once while setting up an authenticator app.
type TOTPSetup struct {
	Secret string `json:"secret"` // base32 for manual entry
	URI    string `json:"uri"`
	QR     string `json:"qr"` // SVG
}

// SetupTOTP creates a new, unconfirmed seed. An active setup must be
// disabled first.
func (a *Auth) SetupTOTP(ctx context.Context, actor core.Actor, issuer string) (TOTPSetup, error) {
	if a.Box == nil {
		return TOTPSetup{}, errors.New("auth: no master key configured")
	}
	if a.totpActive(ctx, actor.User.ID) {
		return TOTPSetup{}, core.Invalid("validation.totp_active", nil, "two-factor authentication is already active")
	}
	secret := newTOTPSecret()
	enc, err := a.Box.Seal(secret, "totp:"+actor.User.ID)
	if err != nil {
		return TOTPSetup{}, err
	}
	if err := a.DB.SetTOTP(ctx, actor.User.ID, enc); err != nil {
		return TOTPSetup{}, err
	}
	uri := totpURI(secret, issuer, actor.User.Email)
	qrsvg, err := qrSVG(uri)
	if err != nil {
		return TOTPSetup{}, err
	}
	return TOTPSetup{Secret: b32.EncodeToString(secret), URI: uri, QR: qrsvg}, nil
}

// ConfirmTOTP activates the setup with a first code and returns fresh
// recovery codes (shown once).
func (a *Auth) ConfirmTOTP(ctx context.Context, actor core.Actor, code string) ([]string, error) {
	if a.totpActive(ctx, actor.User.ID) {
		return nil, core.Invalid("validation.totp_active", nil, "two-factor authentication is already active")
	}
	if err := a.checkTOTP(ctx, actor.User.ID, code, true); err != nil {
		return nil, err
	}
	codes, err := a.newRecoveryCodes(ctx, actor.User.ID)
	if err == nil {
		_ = a.DB.AddEvent(ctx, actor.String(), "auth.totp_enable", actor.User.ID, nil)
	}
	return codes, err
}

func (a *Auth) newRecoveryCodes(ctx context.Context, userID string) ([]string, error) {
	enc := base32.StdEncoding.WithPadding(base32.NoPadding)
	codes := make([]string, recoveryCodeCount)
	hashes := make([]string, recoveryCodeCount)
	for i := range codes {
		b := make([]byte, 7)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		s := strings.ToLower(enc.EncodeToString(b))[:10]
		codes[i] = s[:5] + "-" + s[5:]
		hashes[i] = HashSecret(s)
	}
	return codes, a.DB.ReplaceRecoveryCodes(ctx, userID, hashes)
}

// confirmIdentity checks the password for sensitive changes. Accounts
// without a password (OIDC) confirm with a current authenticator code.
func (a *Auth) confirmIdentity(ctx context.Context, actor core.Actor, password, code string) error {
	u, err := a.DB.UserByID(ctx, actor.User.ID)
	if err != nil {
		return err
	}
	if u.HasPassword {
		if !VerifyPassword(u.PasswordHash, password) {
			return core.Invalid("validation.password_wrong", nil, "the current password is wrong")
		}
		return nil
	}
	return a.checkTOTP(ctx, u.ID, code, false)
}

// DisableTOTP turns two-factor authentication off and drops recovery codes.
func (a *Auth) DisableTOTP(ctx context.Context, actor core.Actor, password, code string) error {
	if err := a.confirmIdentity(ctx, actor, password, code); err != nil {
		return err
	}
	if err := a.DB.DeleteTOTP(ctx, actor.User.ID); err != nil {
		return err
	}
	if err := a.DB.ReplaceRecoveryCodes(ctx, actor.User.ID, nil); err != nil {
		return err
	}
	_ = a.DB.AddEvent(ctx, actor.String(), "auth.totp_disable", actor.User.ID, nil)
	return nil
}

// RegenerateRecoveryCodes replaces all recovery codes.
func (a *Auth) RegenerateRecoveryCodes(ctx context.Context, actor core.Actor, password, code string) ([]string, error) {
	if !a.totpActive(ctx, actor.User.ID) {
		return nil, core.Invalid("validation.totp_not_setup", nil, "two-factor authentication is not set up")
	}
	if err := a.confirmIdentity(ctx, actor, password, code); err != nil {
		return nil, err
	}
	codes, err := a.newRecoveryCodes(ctx, actor.User.ID)
	if err == nil {
		_ = a.DB.AddEvent(ctx, actor.String(), "auth.recovery_codes_new", actor.User.ID, nil)
	}
	return codes, err
}

// Security summarizes the second factors of the actor.
type Security struct {
	TOTP          bool            `json:"totp"`
	RecoveryCodes int             `json:"recoveryCodes"`
	Passkeys      []store.Passkey `json:"passkeys"`
}

func (a *Auth) Security(ctx context.Context, actor core.Actor) (Security, error) {
	var s Security
	s.TOTP = a.totpActive(ctx, actor.User.ID)
	var err error
	if s.RecoveryCodes, err = a.DB.CountRecoveryCodes(ctx, actor.User.ID); err != nil {
		return s, err
	}
	s.Passkeys, err = a.DB.Passkeys(ctx, actor.User.ID)
	return s, err
}

// ResetSecondFactors lets an admin remove TOTP, recovery codes and passkeys
// of a locked-out user.
func (a *Auth) ResetSecondFactors(ctx context.Context, actor core.Actor, userID string) error {
	if err := requireAdmin(actor); err != nil {
		return err
	}
	if _, err := a.DB.UserByID(ctx, userID); err != nil {
		return err
	}
	if err := a.DB.ResetSecondFactors(ctx, userID); err != nil {
		return err
	}
	_ = a.DB.AddEvent(ctx, actor.String(), "auth.second_factors_reset", userID, nil)
	return nil
}

// ---- Passkeys (WebAuthn) ----

// waUser adapts a user and its passkeys to the library.
type waUser struct {
	u     store.User
	creds []webauthn.Credential
}

func (w *waUser) WebAuthnID() []byte                         { return []byte(w.u.ID) }
func (w *waUser) WebAuthnName() string                       { return w.u.Email }
func (w *waUser) WebAuthnDisplayName() string                { return w.u.Name }
func (w *waUser) WebAuthnCredentials() []webauthn.Credential { return w.creds }

func (a *Auth) loadWAUser(ctx context.Context, u store.User) (*waUser, error) {
	pks, err := a.DB.Passkeys(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	w := &waUser{u: u}
	for _, p := range pks {
		var c webauthn.Credential
		if err := json.Unmarshal([]byte(p.Credential), &c); err == nil {
			w.creds = append(w.creds, c)
		}
	}
	return w, nil
}

// relyingParty configures WebAuthn for the origin the browser uses.
func relyingParty(base string) (*webauthn.WebAuthn, error) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return nil, errors.New("auth: invalid base URL for passkeys")
	}
	return webauthn.New(&webauthn.Config{RPID: u.Hostname(), RPDisplayName: "Phoedox", RPOrigins: []string{u.Scheme + "://" + u.Host}})
}

func passkeyFailed(err error) error {
	return core.Invalid("validation.passkey", nil, "the passkey could not be verified: %v", err)
}

// BeginPasskeyRegistration returns WebAuthn creation options for the actor.
func (a *Auth) BeginPasskeyRegistration(ctx context.Context, actor core.Actor, base string) (any, string, error) {
	rp, err := relyingParty(base)
	if err != nil {
		return nil, "", err
	}
	u, err := a.DB.UserByID(ctx, actor.User.ID)
	if err != nil {
		return nil, "", err
	}
	w, err := a.loadWAUser(ctx, u)
	if err != nil {
		return nil, "", err
	}
	exclude := make([]protocol.CredentialDescriptor, len(w.creds))
	for i, c := range w.creds {
		exclude[i] = c.Descriptor()
	}
	opts, session, err := rp.BeginRegistration(w,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{ResidentKey: protocol.ResidentKeyRequirementRequired, UserVerification: protocol.VerificationPreferred}),
		webauthn.WithExclusions(exclude))
	if err != nil {
		return nil, "", err
	}
	data, _ := json.Marshal(session)
	token, err := a.newChallenge(ctx, u.ID, kindPasskeyReg, string(data))
	return opts, token, err
}

// FinishPasskeyRegistration verifies the browser's response (request body)
// and stores the passkey.
func (a *Auth) FinishPasskeyRegistration(ctx context.Context, actor core.Actor, token, name, base string, r *http.Request) (store.Passkey, error) {
	c, err := a.takeChallenge(ctx, token, kindPasskeyReg)
	if err != nil {
		return store.Passkey{}, err
	}
	_ = a.DB.DeleteChallenge(ctx, c.ID)
	if c.UserID != actor.User.ID {
		return store.Passkey{}, challengeInvalid()
	}
	rp, err := relyingParty(base)
	if err != nil {
		return store.Passkey{}, err
	}
	var session webauthn.SessionData
	if err := json.Unmarshal([]byte(c.Data), &session); err != nil {
		return store.Passkey{}, err
	}
	u, err := a.DB.UserByID(ctx, actor.User.ID)
	if err != nil {
		return store.Passkey{}, err
	}
	w, err := a.loadWAUser(ctx, u)
	if err != nil {
		return store.Passkey{}, err
	}
	cred, err := rp.FinishRegistration(w, session, r)
	if err != nil {
		return store.Passkey{}, passkeyFailed(err)
	}
	if name = strings.TrimSpace(name); name == "" {
		name = "Passkey"
	}
	raw, _ := json.Marshal(cred)
	p, err := a.DB.AddPasskey(ctx, store.Passkey{ID: base64.RawURLEncoding.EncodeToString(cred.ID), UserID: u.ID, Name: name, Credential: string(raw)})
	if err == nil {
		_ = a.DB.AddEvent(ctx, actor.String(), "auth.passkey_add", u.ID, map[string]string{"name": name})
	}
	return p, err
}

// RenamePasskey and DeletePasskey change the actor's own passkeys.
func (a *Auth) RenamePasskey(ctx context.Context, actor core.Actor, id, name string) error {
	if name = strings.TrimSpace(name); name == "" {
		return core.Invalid("validation.name_required", nil, "name is required")
	}
	return a.DB.RenamePasskey(ctx, actor.User.ID, id, name)
}

func (a *Auth) DeletePasskey(ctx context.Context, actor core.Actor, id string) error {
	err := a.DB.DeletePasskey(ctx, actor.User.ID, id)
	if err == nil {
		_ = a.DB.AddEvent(ctx, actor.String(), "auth.passkey_delete", actor.User.ID, map[string]string{"id": id})
	}
	return err
}

// BeginPasskeyLogin starts a sign-in with any passkey of this site
// (discoverable credential, user verification required).
func (a *Auth) BeginPasskeyLogin(ctx context.Context, base string) (any, string, error) {
	rp, err := relyingParty(base)
	if err != nil {
		return nil, "", err
	}
	opts, session, err := rp.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return nil, "", err
	}
	data, _ := json.Marshal(session)
	token, err := a.newChallenge(ctx, "", kindPasskeyLogin, string(data))
	return opts, token, err
}

// FinishPasskeyLogin verifies the assertion and signs the owner in.
func (a *Auth) FinishPasskeyLogin(ctx context.Context, token, base string, r *http.Request) (*http.Cookie, store.User, error) {
	c, err := a.takeChallenge(ctx, token, kindPasskeyLogin)
	if err != nil {
		return nil, store.User{}, err
	}
	_ = a.DB.DeleteChallenge(ctx, c.ID)
	rp, err := relyingParty(base)
	if err != nil {
		return nil, store.User{}, err
	}
	var session webauthn.SessionData
	if err := json.Unmarshal([]byte(c.Data), &session); err != nil {
		return nil, store.User{}, err
	}
	var owner store.User
	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		p, err := a.DB.PasskeyByID(ctx, base64.RawURLEncoding.EncodeToString(rawID))
		if err != nil || p.UserID != string(userHandle) {
			return nil, errors.New("unknown passkey")
		}
		u, err := a.DB.UserByID(ctx, p.UserID)
		if err != nil || u.Disabled {
			return nil, errors.New("unknown passkey")
		}
		owner = u
		return a.loadWAUser(ctx, u)
	}
	cred, err := rp.FinishDiscoverableLogin(handler, session, r)
	if err != nil {
		return nil, store.User{}, passkeyFailed(err)
	}
	if !cred.Flags.UserVerified {
		return nil, store.User{}, passkeyFailed(errors.New("user verification missing"))
	}
	a.savePasskeyUse(ctx, cred)
	cookie, err := a.startSession(ctx, owner)
	if err == nil {
		_ = a.DB.AddEvent(ctx, "user:"+owner.ID, "auth.login", owner.ID, map[string]string{"method": "passkey"})
	}
	return cookie, owner, err
}

func (a *Auth) savePasskeyUse(ctx context.Context, cred *webauthn.Credential) {
	raw, _ := json.Marshal(cred)
	_ = a.DB.UpdatePasskeyUse(ctx, base64.RawURLEncoding.EncodeToString(cred.ID), string(raw))
}

// BeginPasskeySecondFactor offers the user's passkeys for the second step.
func (a *Auth) BeginPasskeySecondFactor(ctx context.Context, challenge, base string) (any, string, error) {
	c, err := a.takeChallenge(ctx, challenge, kindSecondFactor)
	if err != nil {
		return nil, "", err
	}
	rp, err := relyingParty(base)
	if err != nil {
		return nil, "", err
	}
	u, err := a.DB.UserByID(ctx, c.UserID)
	if err != nil {
		return nil, "", err
	}
	w, err := a.loadWAUser(ctx, u)
	if err != nil {
		return nil, "", err
	}
	if len(w.creds) == 0 {
		return nil, "", passkeyFailed(errors.New("no passkeys"))
	}
	opts, session, err := rp.BeginLogin(w)
	if err != nil {
		return nil, "", err
	}
	data, _ := json.Marshal(struct {
		Parent  string               `json:"parent"`
		Session webauthn.SessionData `json:"session"`
	}{c.ID, *session})
	token, err := a.newChallenge(ctx, u.ID, kindPasskey2FA, string(data))
	return opts, token, err
}

// FinishPasskeySecondFactor completes the password login with a passkey.
func (a *Auth) FinishPasskeySecondFactor(ctx context.Context, token, base string, r *http.Request) (*http.Cookie, store.User, error) {
	c, err := a.takeChallenge(ctx, token, kindPasskey2FA)
	if err != nil {
		return nil, store.User{}, err
	}
	_ = a.DB.DeleteChallenge(ctx, c.ID)
	var data struct {
		Parent  string               `json:"parent"`
		Session webauthn.SessionData `json:"session"`
	}
	if err := json.Unmarshal([]byte(c.Data), &data); err != nil {
		return nil, store.User{}, err
	}
	rp, err := relyingParty(base)
	if err != nil {
		return nil, store.User{}, err
	}
	u, err := a.DB.UserByID(ctx, c.UserID)
	if err != nil {
		return nil, store.User{}, err
	}
	w, err := a.loadWAUser(ctx, u)
	if err != nil {
		return nil, store.User{}, err
	}
	cred, err := rp.FinishLogin(w, data.Session, r)
	if err != nil {
		return nil, store.User{}, passkeyFailed(err)
	}
	a.savePasskeyUse(ctx, cred)
	parent := store.Challenge{ID: data.Parent, UserID: u.ID}
	return a.finishSecondFactor(ctx, parent, "passkey")
}
