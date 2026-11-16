package botcheck

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Third-party providers, each with the operator's own keys. The browser
// shows the provider's widget; the server checks its answer here.
var verifyURLs = map[string]string{
	ProviderTurnstile: "https://challenges.cloudflare.com/turnstile/v0/siteverify",
	ProviderHCaptcha:  "https://api.hcaptcha.com/siteverify",
	ProviderReCaptcha: "https://www.google.com/recaptcha/api/siteverify",
}

// ProviderInput is a provider widget's answer.
type ProviderInput struct {
	Challenge string `json:"challenge"`
	Response  string `json:"response"`
}

// VerifyProvider checks a third-party answer and returns a pass token.
func (s *Service) VerifyProvider(ctx context.Context, in ProviderInput, ip, ipHash string) (Result, error) {
	ch, p, err := s.loadChallenge(ctx, in.Challenge, ipHash)
	if err != nil {
		return Result{}, err
	}
	if p.Provider == ProviderOwn || in.Response == "" || len(in.Response) > 4096 {
		return Result{}, ErrInvalid
	}
	if first, err := s.Store.MarkBotUsed(ctx, "c:"+ch.ID, ch.Expires); err != nil || !first {
		return Result{}, ErrInvalid
	}
	keys := s.Config(ctx).Providers[p.Provider]
	if keys == nil || !keys.HasSecret {
		return Result{}, ErrInvalid
	}
	secret, err := s.Sealer.Open(keys.Secret, "botcheck:"+p.Provider)
	if err != nil {
		return Result{}, err
	}
	ok, err := s.siteverify(ctx, p.Provider, string(secret), in.Response, ip)
	ms := int(s.Now().Sub(ch.Issued).Milliseconds())
	if err != nil {
		s.log(ctx, ch, "failed", "provider_unreachable", ipHash, ms)
		return Result{}, err
	}
	if !ok {
		s.log(ctx, ch, "blocked", p.Provider, ipHash, ms)
		s.checkSpike(ctx, ch.Place)
		return Result{Blocked: true}, nil
	}
	s.log(ctx, ch, "passed", p.Provider, ipHash, ms)
	return s.passFor(ch)
}

func (s *Service) siteverify(ctx context.Context, provider, secret, response, ip string) (bool, error) {
	form := url.Values{"secret": {secret}, "response": {response}}
	if ip != "" {
		form.Set("remoteip", ip)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, verifyURLs[provider], strings.NewReader(form.Encode()))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := s.HTTP.Do(req)
	if err != nil {
		return false, err
	}
	defer res.Body.Close()
	var out struct {
		Success bool    `json:"success"`
		Score   float64 `json:"score"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return false, err
	}
	// reCAPTCHA v3 answers with a score; 0.5 is Google's suggested threshold.
	if provider == ProviderReCaptcha && out.Score > 0 && out.Score < 0.5 {
		return false, nil
	}
	return out.Success, nil
}
