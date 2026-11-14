package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// External vaults for environment secrets: a variable can point to a value
// in HashiCorp Vault / OpenBao (KV v1 or v2) or 1Password Connect instead of
// storing it. Values are fetched when a request runs and cached briefly.
// Bitwarden/Vaultwarden is not supported: its vault is encrypted end to end
// and can only be read with the account's master password.

const (
	settingVaultKind  = "vault.kind" // hashicorp | onepassword
	settingVaultURL   = "vault.url"
	settingVaultToken = "vault.token" // sealed
	vaultCacheFor     = time.Minute
)

var (
	hashicorpRef = regexp.MustCompile(`^[A-Za-z0-9_./-]{1,300}#[A-Za-z0-9_.-]{1,100}$`)
	onePassRef   = regexp.MustCompile(`^op://([^/]{1,100})/([^/]{1,200})/([^/]{1,100})$`)
)

type vaultCache struct {
	mu   sync.Mutex
	vals map[string]vaultEntry
}

type vaultEntry struct {
	value string
	at    time.Time
}

// VaultConfig is the instance's vault connection as admins see it.
type VaultConfig struct {
	Kind      string `json:"kind"` // "" = none
	URL       string `json:"url"`
	Connected bool   `json:"connected"` // a token is stored
}

func (s *Service) vaultConfig(ctx context.Context) (VaultConfig, string) {
	kind, _ := s.DB.Setting(store.System(ctx), settingVaultKind, "")
	u, _ := s.DB.Setting(store.System(ctx), settingVaultURL, "")
	sealed, _ := s.DB.Setting(store.System(ctx), settingVaultToken, "")
	token := ""
	if sealed != "" && s.Box != nil {
		if b, err := s.Box.Open(sealed, settingVaultToken); err == nil {
			token = string(b)
		}
	}
	return VaultConfig{Kind: kind, URL: u, Connected: token != ""}, token
}

// saveVaultConfig stores the connection (instance admins, via UpdateSettings).
func (s *Service) saveVaultConfig(ctx context.Context, a Actor, kind, rawURL *string, token *string) error {
	if kind != nil {
		if *kind != "" && *kind != "hashicorp" && *kind != "onepassword" {
			return invalid("validation.vault_kind", nil, "vault must be hashicorp or onepassword")
		}
		if err := s.DB.SetSetting(ctx, settingVaultKind, *kind, a.String()); err != nil {
			return err
		}
	}
	if rawURL != nil {
		v := strings.TrimRight(strings.TrimSpace(*rawURL), "/")
		if v != "" {
			u, err := url.Parse(v)
			if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
				return invalid("validation.vault_url", nil, "the vault needs an http(s) address")
			}
		}
		if err := s.DB.SetSetting(ctx, settingVaultURL, v, a.String()); err != nil {
			return err
		}
	}
	if token != nil {
		sealed := ""
		if t := strings.TrimSpace(*token); t != "" {
			if s.Box == nil {
				return invalid("validation.vault_key", nil, "the vault token needs the master key")
			}
			var err error
			if sealed, err = s.Box.Seal([]byte(t), settingVaultToken); err != nil {
				return err
			}
		}
		if err := s.DB.SetSetting(ctx, settingVaultToken, sealed, a.String()); err != nil {
			return err
		}
	}
	s.vaults.mu.Lock()
	s.vaults.vals = nil
	s.vaults.mu.Unlock()
	return nil
}

// validVaultRef checks a reference against the configured vault.
func (s *Service) validVaultRef(ctx context.Context, ref string) error {
	cfg, _ := s.vaultConfig(ctx)
	switch {
	case cfg.Kind == "":
		return invalid("validation.vault_none", nil, "no vault is connected to this instance")
	case cfg.Kind == "hashicorp" && !hashicorpRef.MatchString(ref):
		return invalid("validation.vault_ref", map[string]any{"example": "secret/data/nas#password"}, "reference like secret/data/nas#password")
	case cfg.Kind == "onepassword" && !onePassRef.MatchString(ref):
		return invalid("validation.vault_ref", map[string]any{"example": "op://Infra/NAS/password"}, "reference like op://Infra/NAS/password")
	}
	return nil
}

// SetVaultVariable makes a variable a secret that lives in the vault (owners).
func (s *Service) SetVaultVariable(ctx context.Context, a Actor, ref, name, vaultRef string) error {
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return err
	}
	if a.Via == "mcp" {
		return ErrForbidden
	}
	if !envName.MatchString(name) {
		return invalid("validation.env_name", nil, "names may contain letters, digits and _ and must not start with a digit")
	}
	vaultRef = strings.TrimSpace(vaultRef)
	if err := s.validVaultRef(ctx, vaultRef); err != nil {
		return err
	}
	if err := s.DB.SetEnv(ctx, p.ID, store.EnvVar{Name: name, Secret: true, VaultRef: vaultRef, UpdatedBy: a.User.ID}); err != nil {
		return err
	}
	s.event(ctx, a, "env.set", p.ID, map[string]any{"name": name, "secret": true, "vault": true})
	return nil
}

// vaultValue fetches a referenced value (cached for a minute).
func (s *Service) vaultValue(ctx context.Context, ref string) (string, error) {
	s.vaults.mu.Lock()
	if e, ok := s.vaults.vals[ref]; ok && time.Since(e.at) < vaultCacheFor {
		s.vaults.mu.Unlock()
		return e.value, nil
	}
	s.vaults.mu.Unlock()
	cfg, token := s.vaultConfig(ctx)
	if cfg.Kind == "" || cfg.URL == "" || token == "" {
		return "", errors.New("no vault connected")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var v string
	var err error
	switch cfg.Kind {
	case "hashicorp":
		v, err = s.hashicorpValue(ctx, cfg.URL, token, ref)
	case "onepassword":
		v, err = s.onePasswordValue(ctx, cfg.URL, token, ref)
	default:
		err = errors.New("unknown vault")
	}
	if err != nil {
		return "", err
	}
	s.vaults.mu.Lock()
	if s.vaults.vals == nil {
		s.vaults.vals = map[string]vaultEntry{}
	}
	s.vaults.vals[ref] = vaultEntry{value: v, at: time.Now()}
	s.vaults.mu.Unlock()
	return v, nil
}

func (s *Service) vaultGet(ctx context.Context, endpoint string, header http.Header, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header = header
	res, err := s.RequestPolicy.Client().Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("vault answered %s", res.Status)
	}
	return json.NewDecoder(res.Body).Decode(out)
}

// hashicorpValue reads path#field from Vault/OpenBao (KV v2 or v1).
func (s *Service) hashicorpValue(ctx context.Context, base, token, ref string) (string, error) {
	path, field, _ := strings.Cut(ref, "#")
	var out struct {
		Data map[string]any `json:"data"`
	}
	if err := s.vaultGet(ctx, base+"/v1/"+strings.TrimLeft(path, "/"), http.Header{"X-Vault-Token": {token}}, &out); err != nil {
		return "", err
	}
	data := out.Data
	if inner, ok := data["data"].(map[string]any); ok { // KV v2
		data = inner
	}
	v, ok := data[field]
	if !ok {
		return "", fmt.Errorf("field %s not found", field)
	}
	return fmt.Sprint(v), nil
}

// onePasswordValue reads op://vault/item/field through 1Password Connect.
func (s *Service) onePasswordValue(ctx context.Context, base, token, ref string) (string, error) {
	m := onePassRef.FindStringSubmatch(ref)
	if m == nil {
		return "", errors.New("bad reference")
	}
	h := http.Header{"Authorization": {"Bearer " + token}}
	var vaults []struct {
		ID string `json:"id"`
	}
	if err := s.vaultGet(ctx, base+"/v1/vaults?filter="+url.QueryEscape(`name eq "`+m[1]+`"`), h, &vaults); err != nil || len(vaults) == 0 {
		return "", fmt.Errorf("vault %s not found: %v", m[1], err)
	}
	var items []struct {
		ID string `json:"id"`
	}
	if err := s.vaultGet(ctx, base+"/v1/vaults/"+vaults[0].ID+"/items?filter="+url.QueryEscape(`title eq "`+m[2]+`"`), h, &items); err != nil || len(items) == 0 {
		return "", fmt.Errorf("item %s not found: %v", m[2], err)
	}
	var item struct {
		Fields []struct {
			Label string `json:"label"`
			Value string `json:"value"`
		} `json:"fields"`
	}
	if err := s.vaultGet(ctx, base+"/v1/vaults/"+vaults[0].ID+"/items/"+items[0].ID, h, &item); err != nil {
		return "", err
	}
	for _, f := range item.Fields {
		if strings.EqualFold(f.Label, m[3]) {
			return f.Value, nil
		}
	}
	return "", fmt.Errorf("field %s not found", m[3])
}
