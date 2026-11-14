package auth

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/secret"
	"github.com/phoedox/phoedox/internal/store"
)

func TestTwoFactorLogin(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(ctx, store.SQLite, filepath.Join(dir, "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	box, err := secret.Load("", filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	a := &Auth{DB: db, Box: box}
	u, _ := a.CreateUser(ctx, "jan@example.org", "Jan", "sehr-geheimes-passwort", true)
	me := core.Actor{User: u, Via: "web"}

	// set up: unconfirmed setup does not change the login
	setup, err := a.SetupTOTP(ctx, me, "Phoedox (test)")
	if err != nil || setup.Secret == "" || setup.QR == "" {
		t.Fatalf("setup: %v", err)
	}
	if step, err := a.Login(ctx, "jan@example.org", "sehr-geheimes-passwort"); err != nil || step.Cookie == nil {
		t.Fatalf("login before confirm must be one step: %v %+v", err, step)
	}
	seed, _ := b32.DecodeString(setup.Secret)
	// Run inside one 30-second step: near its end, wait for the next one, so
	// "the same code again" is not a fresh code by then.
	if sec := time.Now().Unix() % 30; sec > 24 {
		time.Sleep(time.Duration(31-sec) * time.Second)
	}
	step0 := time.Now().Unix() / 30
	code := func(offset int64) string { return totpCode(seed, step0+offset) }
	if _, err := a.ConfirmTOTP(ctx, me, "000000"); keyOf(err) != "validation.totp_code" && code(0) != "000000" {
		t.Fatalf("wrong confirm code: %v", err)
	}
	codes, err := a.ConfirmTOTP(ctx, me, code(0))
	if err != nil || len(codes) != 10 {
		t.Fatalf("confirm: %v %d", err, len(codes))
	}
	if _, err := a.SetupTOTP(ctx, me, "x"); keyOf(err) != "validation.totp_active" {
		t.Fatalf("second setup: %v", err)
	}
	if sec, _ := a.Security(ctx, me); !sec.TOTP || sec.RecoveryCodes != 10 {
		t.Fatalf("security: %+v", sec)
	}
	if us, _ := a.Users(ctx, me); !us[0].SecondFactor {
		t.Fatal("user list must show the second factor")
	}

	// password alone is not enough any more
	step, err := a.Login(ctx, "jan@example.org", "sehr-geheimes-passwort")
	if err != nil || step.Cookie != nil || step.Challenge == "" || len(step.Methods) != 2 {
		t.Fatalf("login needs a second step: %v %+v", err, step)
	}
	// the code from the confirmation cannot be replayed
	if _, _, err := a.LoginTOTP(ctx, step.Challenge, code(0)); keyOf(err) != "validation.totp_code" {
		t.Fatalf("replayed code: %v", err)
	}
	if c, _, err := a.LoginTOTP(ctx, step.Challenge, code(1)); err != nil || c == nil {
		t.Fatalf("next code: %v", err)
	}
	if _, _, err := a.LoginTOTP(ctx, step.Challenge, code(1)); keyOf(err) != "validation.challenge_invalid" {
		t.Fatalf("challenge must be single use: %v", err)
	}

	// attempt limit
	step, _ = a.Login(ctx, "jan@example.org", "sehr-geheimes-passwort")
	for range maxFactorAttempts {
		_, _, _ = a.LoginTOTP(ctx, step.Challenge, "999999")
	}
	if _, _, err := a.LoginRecovery(ctx, step.Challenge, codes[0]); keyOf(err) != "validation.challenge_invalid" {
		t.Fatalf("challenge must end after %d attempts: %v", maxFactorAttempts, err)
	}

	// recovery codes work once, with any formatting
	step, _ = a.Login(ctx, "jan@example.org", "sehr-geheimes-passwort")
	if c, _, err := a.LoginRecovery(ctx, step.Challenge, " "+codes[0][:3]+" "+codes[0][3:]+" "); err != nil || c == nil {
		t.Fatalf("recovery: %v", err)
	}
	step, _ = a.Login(ctx, "jan@example.org", "sehr-geheimes-passwort")
	if _, _, err := a.LoginRecovery(ctx, step.Challenge, codes[0]); keyOf(err) != "validation.recovery_code" {
		t.Fatalf("used recovery code: %v", err)
	}

	// new codes need the password; disabling too
	if _, err := a.RegenerateRecoveryCodes(ctx, me, "falsch", ""); keyOf(err) != "validation.password_wrong" {
		t.Fatalf("regenerate without password: %v", err)
	}
	fresh, err := a.RegenerateRecoveryCodes(ctx, me, "sehr-geheimes-passwort", "")
	if err != nil || len(fresh) != 10 || fresh[0] == codes[1] {
		t.Fatalf("regenerate: %v", err)
	}
	if err := a.DisableTOTP(ctx, me, "sehr-geheimes-passwort", ""); err != nil {
		t.Fatal(err)
	}
	if step, _ := a.Login(ctx, "jan@example.org", "sehr-geheimes-passwort"); step.Cookie == nil {
		t.Fatal("login after disable must be one step")
	}

	// admin reset
	_, _ = a.SetupTOTP(ctx, me, "x")
	s2, _ := a.DB.GetTOTP(ctx, u.ID)
	seed2, _ := a.totpSecret(s2)
	if _, err := a.ConfirmTOTP(ctx, me, totpCode(seed2, time.Now().Unix()/30)); err != nil {
		t.Fatal(err)
	}
	other, _ := a.CreateUser(ctx, "other@example.org", "Other", "sehr-geheimes-passwort", false)
	if err := a.ResetSecondFactors(ctx, core.Actor{User: other, Via: "web"}, u.ID); err != core.ErrForbidden {
		t.Fatalf("non-admin reset: %v", err)
	}
	if err := a.ResetSecondFactors(ctx, me, u.ID); err != nil {
		t.Fatal(err)
	}
	if step, _ := a.Login(ctx, "jan@example.org", "sehr-geheimes-passwort"); step.Cookie == nil {
		t.Fatal("login after admin reset must be one step")
	}
	// the seed is encrypted at rest
	if s2.SecretEnc == "" || s2.SecretEnc[:3] != "v1:" {
		t.Fatalf("seed not encrypted: %q", s2.SecretEnc)
	}
}
