package core

import (
	"context"
	"errors"
	"regexp"

	"github.com/phoedox/phoedox/internal/store"
)

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

// Environment lists the project's variables. Secret values are never
// returned (only that they are set).
func (s *Service) Environment(ctx context.Context, a Actor, ref string) ([]store.EnvVar, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return nil, err
	}
	vars, err := s.DB.Env(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	for i := range vars {
		if vars[i].Secret {
			vars[i].Value = ""
		}
	}
	return vars, nil
}

// SetVariable stores a variable (editors) or a secret (owners). AI clients
// cannot change the environment.
func (s *Service) SetVariable(ctx context.Context, a Actor, ref, name, value string, secret bool) error {
	need := store.RoleEditor
	if secret {
		need = store.RoleOwner
	}
	p, err := s.project(ctx, a, ref, need)
	if err != nil {
		return err
	}
	if a.Via == "mcp" {
		return ErrForbidden
	}
	if !envName.MatchString(name) {
		return invalid("validation.env_name", nil, "names may contain letters, digits and _ and must not start with a digit")
	}
	if len(value) > 8192 {
		return invalid("validation.env_value", nil, "the value is too long")
	}
	// An existing secret cannot be turned into a plain variable by editors.
	if !secret {
		if vars, err := s.DB.Env(ctx, p.ID); err == nil {
			for _, v := range vars {
				if v.Name == name && v.Secret && p.Role != store.RoleOwner {
					return ErrForbidden
				}
			}
		}
	}
	stored := value
	if secret {
		if s.Box == nil {
			return errors.New("core: no master key configured")
		}
		if stored, err = s.Box.Seal([]byte(value), "env:"+p.ID+":"+name); err != nil {
			return err
		}
	}
	if err := s.DB.SetEnv(ctx, p.ID, store.EnvVar{Name: name, Value: stored, Secret: secret, UpdatedBy: a.User.ID}); err != nil {
		return err
	}
	s.event(ctx, a, "env.set", p.ID, map[string]any{"name": name, "secret": secret})
	return nil
}

// DeleteVariable removes a variable; secrets need the owner.
func (s *Service) DeleteVariable(ctx context.Context, a Actor, ref, name string) error {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return err
	}
	if a.Via == "mcp" {
		return ErrForbidden
	}
	vars, err := s.DB.Env(ctx, p.ID)
	if err != nil {
		return err
	}
	for _, v := range vars {
		if v.Name == name && v.Secret && p.Role != store.RoleOwner {
			return ErrForbidden
		}
	}
	if err := s.DB.DeleteEnv(ctx, p.ID, name); err != nil {
		return err
	}
	s.event(ctx, a, "env.delete", p.ID, map[string]string{"name": name})
	return nil
}

// envValues returns plain values of all variables and the list of secret
// values (for masking).
func (s *Service) envValues(ctx context.Context, projectID string) (map[string]string, []string, error) {
	vars, err := s.DB.Env(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	values := map[string]string{}
	var secrets []string
	for _, v := range vars {
		if !v.Secret {
			values[v.Name] = v.Value
			continue
		}
		if s.Box == nil {
			continue
		}
		plain, err := s.Box.Open(v.Value, "env:"+projectID+":"+v.Name)
		if err != nil {
			continue // wrong master key: the variable stays unknown
		}
		values[v.Name] = string(plain)
		secrets = append(secrets, string(plain))
	}
	return values, secrets, nil
}
