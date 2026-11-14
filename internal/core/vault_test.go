package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/requests"
	"github.com/phoedox/phoedox/internal/secret"
)

// TestVault: environment secrets read from HashiCorp Vault and 1Password Connect.
func TestVault(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	s.Box, _ = secret.New(make([]byte, 32))
	s.RequestPolicy = requests.Policy{AllowPrivate: true}
	admin := owner
	admin.User.IsAdmin = true
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus"})

	hv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != "hv-token" {
			w.WriteHeader(403)
			return
		}
		switch r.URL.Path {
		case "/v1/secret/data/nas":
			_, _ = w.Write([]byte(`{"data":{"data":{"password":"geheim-kv2"}}}`))
		case "/v1/kv/router":
			_, _ = w.Write([]byte(`{"data":{"password":"geheim-kv1"}}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer hv.Close()
	if err := s.SetVaultVariable(ctx, owner, p.Slug, "NAS_PW", "secret/data/nas#password"); invalidKey(err) != "validation.vault_none" {
		t.Fatalf("without vault: %v", err)
	}
	kind, u, tok := "hashicorp", hv.URL, "hv-token"
	if _, err := s.UpdateSettings(ctx, admin, SettingsPatch{VaultKind: &kind, VaultURL: &u, VaultToken: &tok}); err != nil {
		t.Fatal(err)
	}
	if set, _ := s.Settings(ctx, admin); !set.Vault.Connected || set.Vault.Kind != "hashicorp" {
		t.Fatalf("settings: %+v", set.Vault)
	}
	if err := s.SetVaultVariable(ctx, owner, p.Slug, "NAS_PW", "op://x/y/z"); invalidKey(err) != "validation.vault_ref" {
		t.Fatalf("bad ref: %v", err)
	}
	for name, ref := range map[string]string{"NAS_PW": "secret/data/nas#password", "ROUTER_PW": "kv/router#password", "GONE": "secret/data/none#x"} {
		if err := s.SetVaultVariable(ctx, owner, p.Slug, name, ref); err != nil {
			t.Fatal(err)
		}
	}
	vals, secrets, _ := s.envValues(ctx, p.ID)
	if vals["NAS_PW"] != "geheim-kv2" || vals["ROUTER_PW"] != "geheim-kv1" || len(secrets) != 2 {
		t.Fatalf("values: %v %v", vals, secrets)
	}
	if _, ok := vals["GONE"]; ok {
		t.Fatal("missing entries stay unknown")
	}
	env, _ := s.Environment(ctx, owner, p.Slug)
	for _, v := range env {
		if v.Value != "" || !v.Secret || v.VaultRef == "" {
			t.Fatalf("listing: %+v", v)
		}
	}

	// 1Password Connect.
	op := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer op-token" {
			w.WriteHeader(401)
			return
		}
		switch {
		case r.URL.Path == "/v1/vaults" && strings.Contains(r.URL.Query().Get("filter"), `"Infra"`):
			_, _ = w.Write([]byte(`[{"id":"v1"}]`))
		case r.URL.Path == "/v1/vaults/v1/items" && strings.Contains(r.URL.Query().Get("filter"), `"NAS"`):
			_, _ = w.Write([]byte(`[{"id":"i1"}]`))
		case r.URL.Path == "/v1/vaults/v1/items/i1":
			_, _ = w.Write([]byte(`{"fields":[{"label":"username","value":"admin"},{"label":"password","value":"geheim-op"}]}`))
		default:
			_, _ = w.Write([]byte(`[]`))
		}
	}))
	defer op.Close()
	kind, u, tok = "onepassword", op.URL, "op-token"
	if _, err := s.UpdateSettings(ctx, admin, SettingsPatch{VaultKind: &kind, VaultURL: &u, VaultToken: &tok}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetVaultVariable(ctx, owner, p.Slug, "OP_PW", "op://Infra/NAS/password"); err != nil {
		t.Fatal(err)
	}
	if v, err := s.vaultValue(ctx, "op://Infra/NAS/password"); err != nil || v != "geheim-op" {
		t.Fatalf("1password: %v %q", err, v)
	}
	if _, err := s.vaultValue(ctx, "op://Infra/Nope/password"); err == nil {
		t.Fatal("unknown item")
	}
}
