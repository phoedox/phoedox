package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/phoedox/phoedox/internal/payments"
	"github.com/phoedox/phoedox/internal/store"
)

// Payments (ADR-0079): the system admin chooses the payment provider –
// off, Stripe (Phoedox is the merchant, optionally with Stripe Tax), Stripe
// Managed Payments (Stripe is the merchant of record for digital products)
// or Paddle (merchant of record; checkout on the instance's /pay page). Features start purchases through StartPurchase; the provider's
// webhook updates the purchase and calls the feature's handler.

const settingPayments = "payments.config"

// providerKeys are one provider's sealed secrets.
type providerKeys struct {
	SecretKey     string `json:"secretKey,omitempty"`
	WebhookSecret string `json:"webhookSecret,omitempty"`
	KeyHint       string `json:"keyHint,omitempty"`
	// ClientToken is Paddle's public client-side token (not secret).
	ClientToken string `json:"clientToken,omitempty"`
}

// paymentConfig is the stored configuration (secrets sealed, kept per
// provider so switching back and forth loses nothing).
type paymentConfig struct {
	Provider string                  `json:"provider"` // "" (off) | stripe | paddle
	Mode     string                  `json:"mode"`     // stripe: classic | managed
	Keys     map[string]providerKeys `json:"keys,omitempty"`
	// TaxCode (Stripe, txcd_…) and TaxCategory (Paddle: saas, standard …).
	TaxCode      string `json:"taxCode,omitempty"`
	TaxCategory  string `json:"taxCategory,omitempty"`
	AutomaticTax bool   `json:"automaticTax,omitempty"`
	Currency     string `json:"currency,omitempty"`
}

func (c paymentConfig) keys() providerKeys { return c.Keys[c.Provider] }

// PaymentSettings is what the administration shows.
type PaymentSettings struct {
	Provider         string `json:"provider"`
	Mode             string `json:"mode"`
	HasSecretKey     bool   `json:"hasSecretKey"`
	KeyHint          string `json:"keyHint,omitempty"`
	Live             bool   `json:"live"`
	HasWebhookSecret bool   `json:"hasWebhookSecret"`
	// Ready: a provider is chosen and its key is set.
	Ready        bool   `json:"ready"`
	TaxCode      string `json:"taxCode"`
	TaxCategory  string `json:"taxCategory"`
	AutomaticTax bool   `json:"automaticTax"`
	Currency     string `json:"currency"`
	// WebhookURL is where the provider sends events.
	WebhookURL string `json:"webhookUrl"`
	// Events the webhook has to send.
	WebhookEvents []string `json:"webhookEvents"`
	// Providers lists what can be chosen.
	Providers []string `json:"providers"`
	// ClientToken (Paddle) is public; PayURL must be approved as Paddle's
	// default payment link.
	ClientToken string `json:"clientToken,omitempty"`
	PayURL      string `json:"payUrl,omitempty"`
	// TaxCategories (Paddle) to choose from.
	TaxCategories []string `json:"taxCategories"`
}

// PaymentSettingsInput changes the settings; nil secrets stay. Secrets
// belong to the provider given.
type PaymentSettingsInput struct {
	Provider      string  `json:"provider"`
	Mode          string  `json:"mode"`
	SecretKey     *string `json:"secretKey,omitempty"`
	WebhookSecret *string `json:"webhookSecret,omitempty"`
	ClientToken   *string `json:"clientToken,omitempty"`
	TaxCode       string  `json:"taxCode"`
	TaxCategory   string  `json:"taxCategory"`
	AutomaticTax  bool    `json:"automaticTax"`
	Currency      string  `json:"currency"`
}

var (
	stripeKeyRe   = regexp.MustCompile(`^(sk|rk)_(test|live)_[A-Za-z0-9]{10,}$`)
	stripeHookRe  = regexp.MustCompile(`^whsec_[A-Za-z0-9+/=]{10,}$`)
	taxCodeRe     = regexp.MustCompile(`^txcd_\d{8}$`)
	currencyRe    = regexp.MustCompile(`^[a-z]{3}$`)
	stripeWebhook = []string{"checkout.session.completed", "checkout.session.async_payment_succeeded", "checkout.session.async_payment_failed",
		"checkout.session.expired", "charge.refunded", "invoice.paid", "customer.subscription.updated", "customer.subscription.deleted"}
	paymentProviders = []string{"stripe", "paddle"}
)

func (s *Service) sealSecret(v, purpose string) (string, error) {
	if s.Box == nil {
		return "plain:" + v, nil // tests
	}
	return s.Box.Seal([]byte(v), purpose)
}

func (s *Service) openSecret(v, purpose string) string {
	if v == "" {
		return ""
	}
	if p, ok := strings.CutPrefix(v, "plain:"); ok && s.Box == nil {
		return p
	}
	if s.Box == nil {
		return ""
	}
	raw, err := s.Box.Open(v, purpose)
	if err != nil {
		return ""
	}
	return string(raw)
}

func (s *Service) readPaymentConfig(ctx context.Context) paymentConfig {
	var c paymentConfig
	if raw, _ := s.DB.Setting(ctx, settingPayments, ""); raw != "" {
		_ = json.Unmarshal([]byte(raw), &c)
	}
	if c.Keys == nil {
		c.Keys = map[string]providerKeys{}
	}
	if c.Mode == "" {
		c.Mode = payments.ModeClassic
	}
	if c.Currency == "" {
		c.Currency = "eur"
	}
	if c.TaxCode == "" {
		c.TaxCode = payments.DefaultTaxCode
	}
	if c.TaxCategory == "" {
		c.TaxCategory = payments.PaddleDefaultTaxCategory
	}
	return c
}

func (s *Service) paymentView(c paymentConfig) PaymentSettings {
	k := c.keys()
	v := PaymentSettings{Provider: c.Provider, Mode: c.Mode, HasSecretKey: k.SecretKey != "", KeyHint: k.KeyHint, Live: strings.Contains(k.KeyHint, "live"),
		HasWebhookSecret: k.WebhookSecret != "", Ready: c.Provider != "" && k.SecretKey != "", TaxCode: c.TaxCode, TaxCategory: c.TaxCategory,
		AutomaticTax: c.AutomaticTax, Currency: c.Currency, WebhookEvents: []string{}, Providers: paymentProviders,
		TaxCategories: payments.PaddleTaxCategories}
	switch c.Provider {
	case "stripe":
		v.WebhookURL, v.WebhookEvents = s.PublicURL+"/api/v1/payments/webhook/stripe", stripeWebhook
	case "paddle":
		v.WebhookURL, v.WebhookEvents = s.PublicURL+"/api/v1/payments/webhook/paddle", payments.PaddleWebhookEvents
		v.ClientToken, v.PayURL = k.ClientToken, s.PublicURL+"/pay"
		v.Ready = v.Ready && k.ClientToken != ""
	}
	return v
}

// providerFor builds the provider of a configuration (nil when off or
// without a key).
func (s *Service) providerFor(c paymentConfig) payments.Provider {
	k := c.keys()
	if k.SecretKey == "" {
		return nil
	}
	key, hook := s.openSecret(k.SecretKey, "payments-key"), s.openSecret(k.WebhookSecret, "payments-webhook")
	switch c.Provider {
	case "stripe":
		return &payments.Stripe{SecretKey: key, WebhookSecret: hook, Managed: c.Mode == payments.ModeManaged, AutomaticTax: c.AutomaticTax, TaxCode: c.TaxCode, BaseURL: s.PaymentsBaseURL}
	case "paddle":
		return &payments.Paddle{APIKey: key, WebhookSecret: hook, TaxCategory: c.TaxCategory, PayURL: s.PublicURL + "/pay", BaseURL: s.PaymentsBaseURL}
	}
	return nil
}

// PaymentProvider is the configured provider (nil when payments are off).
func (s *Service) PaymentProvider(ctx context.Context) payments.Provider {
	return s.providerFor(s.readPaymentConfig(ctx))
}

// AdminPayments reads the payment settings (system admins).
func (s *Service) AdminPayments(ctx context.Context, a Actor) (PaymentSettings, error) {
	if err := s.adminOnly(a); err != nil {
		return PaymentSettings{}, err
	}
	return s.paymentView(s.readPaymentConfig(ctx)), nil
}

// keyHint shows the kind of a key and its last four characters.
// (sk_test_…mnop, pdl_sdbx_apikey_…_abc).
func keyHint(k string) string {
	if len(k) < 8 {
		return "…"
	}
	head := k[:min(len(k)-4, 24)]
	i := strings.LastIndex(head, "_")
	if i < 0 {
		return "…" + k[len(k)-4:]
	}
	return k[:i+1] + "…" + k[len(k)-4:]
}

// SetAdminPayments changes the payment settings (system admins).
func (s *Service) SetAdminPayments(ctx context.Context, a Actor, in PaymentSettingsInput) (PaymentSettings, error) {
	if err := s.adminOnly(a); err != nil {
		return PaymentSettings{}, err
	}
	c := s.readPaymentConfig(ctx)
	if in.Provider != "" && !slices.Contains(paymentProviders, in.Provider) {
		return PaymentSettings{}, invalid("validation.payments_provider", map[string]any{"provider": in.Provider}, "unknown payment provider %q", in.Provider)
	}
	if in.Mode != payments.ModeClassic && in.Mode != payments.ModeManaged || in.Provider != "stripe" {
		if in.Provider != "stripe" {
			in.Mode = payments.ModeClassic
		} else if in.Mode != payments.ModeManaged {
			in.Mode = payments.ModeClassic
		}
	}
	in.TaxCode = strings.TrimSpace(in.TaxCode)
	if in.TaxCode == "" {
		in.TaxCode = c.TaxCode
	}
	if !taxCodeRe.MatchString(in.TaxCode) {
		return PaymentSettings{}, invalid("validation.payments_tax_code", nil, "a tax code looks like txcd_10103001")
	}
	in.TaxCategory = strings.TrimSpace(in.TaxCategory)
	if in.TaxCategory == "" {
		in.TaxCategory = c.TaxCategory
	}
	if !slices.Contains(payments.PaddleTaxCategories, in.TaxCategory) {
		return PaymentSettings{}, invalid("validation.payments_tax_category", nil, "unknown tax category")
	}
	in.Currency = strings.ToLower(strings.TrimSpace(in.Currency))
	if in.Currency == "" {
		in.Currency = "eur"
	}
	if !currencyRe.MatchString(in.Currency) {
		return PaymentSettings{}, invalid("validation.payments_currency", nil, "a currency is a three-letter code such as eur")
	}
	k := c.Keys[in.Provider]
	if in.SecretKey != nil {
		key := strings.TrimSpace(*in.SecretKey)
		switch {
		case key == "":
			k.SecretKey, k.KeyHint = "", ""
		case in.Provider == "stripe" && !stripeKeyRe.MatchString(key):
			return PaymentSettings{}, invalid("validation.payments_key", nil, "this is not a Stripe secret or restricted key (sk_… or rk_…)")
		case in.Provider == "paddle" && !payments.PaddleKeyRe.MatchString(key):
			return PaymentSettings{}, invalid("validation.payments_paddle_key", nil, "this is not a Paddle API key (pdl_live_apikey_… or pdl_sdbx_apikey_…)")
		case in.Provider == "":
			return PaymentSettings{}, invalid("validation.payments_provider", map[string]any{"provider": ""}, "choose a provider first")
		default:
			sealed, err := s.sealSecret(key, "payments-key")
			if err != nil {
				return PaymentSettings{}, err
			}
			k.SecretKey, k.KeyHint = sealed, keyHint(key)
		}
	}
	if in.WebhookSecret != nil {
		w := strings.TrimSpace(*in.WebhookSecret)
		switch {
		case w == "":
			k.WebhookSecret = ""
		case in.Provider == "stripe" && !stripeHookRe.MatchString(w):
			return PaymentSettings{}, invalid("validation.payments_webhook", nil, "a webhook signing secret starts with whsec_")
		case in.Provider == "paddle" && !payments.PaddleHookRe.MatchString(w):
			return PaymentSettings{}, invalid("validation.payments_paddle_webhook", nil, "a Paddle webhook secret starts with pdl_ntfset_")
		case in.Provider == "":
			return PaymentSettings{}, invalid("validation.payments_provider", map[string]any{"provider": ""}, "choose a provider first")
		default:
			sealed, err := s.sealSecret(w, "payments-webhook")
			if err != nil {
				return PaymentSettings{}, err
			}
			k.WebhookSecret = sealed
		}
	}
	if in.ClientToken != nil {
		t := strings.TrimSpace(*in.ClientToken)
		switch {
		case t == "":
			k.ClientToken = ""
		case in.Provider != "paddle" || !payments.PaddleClientTokenRe.MatchString(t):
			return PaymentSettings{}, invalid("validation.payments_client_token", nil, "a Paddle client-side token starts with live_ or test_")
		default:
			k.ClientToken = t
		}
	}
	if in.Provider != "" {
		c.Keys[in.Provider] = k
	}
	c.Provider, c.Mode, c.TaxCode, c.TaxCategory, c.Currency = in.Provider, in.Mode, in.TaxCode, in.TaxCategory, in.Currency
	// Managed Payments and Paddle do tax themselves; Stripe Tax is a classic option.
	c.AutomaticTax = in.AutomaticTax && in.Provider == "stripe" && in.Mode == payments.ModeClassic
	raw, _ := json.Marshal(c)
	if err := s.DB.SetSetting(ctx, settingPayments, string(raw), a.User.ID); err != nil {
		return PaymentSettings{}, err
	}
	s.event(ctx, a, "payments.settings", "instance", map[string]any{"provider": c.Provider, "mode": c.Mode, "automaticTax": c.AutomaticTax, "currency": c.Currency})
	return s.paymentView(c), nil
}

// PaymentCheck is the result of checking the key with the provider.
type PaymentCheck struct {
	payments.Account
	// Warnings: a managed account outside the supported countries, a
	// missing webhook secret, test keys …
	Warnings []string `json:"warnings"`
}

// CheckPayments asks the provider about the account behind the key.
func (s *Service) CheckPayments(ctx context.Context, a Actor) (PaymentCheck, error) {
	if err := s.adminOnly(a); err != nil {
		return PaymentCheck{}, err
	}
	c := s.readPaymentConfig(ctx)
	p := s.providerFor(c)
	if p == nil {
		return PaymentCheck{}, invalid("validation.payments_off", nil, "payments are switched off")
	}
	acc, err := p.Check(ctx)
	if err != nil {
		return PaymentCheck{}, invalid("validation.payments_provider_error", map[string]any{"msg": err.Error()}, "the provider answered: %s", err.Error())
	}
	out := PaymentCheck{Account: acc, Warnings: []string{}}
	if c.Provider == "stripe" && c.Mode == payments.ModeManaged && !acc.ManagedEligible {
		out.Warnings = append(out.Warnings, "managed_country")
	}
	if c.keys().WebhookSecret == "" {
		out.Warnings = append(out.Warnings, "no_webhook")
	}
	if c.Provider == "paddle" && c.keys().ClientToken == "" {
		out.Warnings = append(out.Warnings, "no_client_token")
	}
	if !acc.Live {
		out.Warnings = append(out.Warnings, "test_mode")
	}
	if !acc.Charges {
		out.Warnings = append(out.Warnings, "charges_disabled")
	}
	return out, nil
}

// PurchaseInput is what a feature sells.
type PurchaseInput struct {
	Kind        string // test, ai_credits, storage, domain …
	Item        string // what exactly (e.g. a credit pack id)
	Name        string // shown at checkout
	Description string
	Amount      int64  // smallest currency unit
	Currency    string // "" = the configured currency
	Recurring   string // "" | month | year
	TaxCode     string // "" = the configured default
	OrgID       string
	// ReturnPath is where the buyer comes back (success and cancel get
	// ?purchase=<id>&result=ok|cancel).
	ReturnPath string
}

// PurchaseCheckout is the started checkout.
type PurchaseCheckout struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

// PaymentHandler acts on a purchase when its state changes (paid,
// refunded, renewed, canceled …).
type PaymentHandler func(ctx context.Context, p store.Purchase, ev payments.Event)

// OnPayment registers what happens when purchases of a kind change.
func (s *Service) OnPayment(kind string, h PaymentHandler) {
	s.paymentMu.Lock()
	defer s.paymentMu.Unlock()
	if s.paymentHandlers == nil {
		s.paymentHandlers = map[string]PaymentHandler{}
	}
	s.paymentHandlers[kind] = h
}

// StartPurchase records a pending purchase and opens the provider's checkout.
func (s *Service) StartPurchase(ctx context.Context, a Actor, in PurchaseInput) (PurchaseCheckout, error) {
	if err := s.fullUser(a); err != nil {
		return PurchaseCheckout{}, err
	}
	c := s.readPaymentConfig(ctx)
	p := s.providerFor(c)
	if p == nil {
		return PurchaseCheckout{}, invalid("validation.payments_off", nil, "payments are switched off")
	}
	if in.Currency == "" {
		in.Currency = c.Currency
	}
	if in.Amount <= 0 || strings.TrimSpace(in.Name) == "" {
		return PurchaseCheckout{}, invalid("validation.payments_item", nil, "nothing to pay")
	}
	now := store.Now()
	pur := store.Purchase{ID: "pur_" + strings.ToLower(newID()), UserID: a.User.ID, OrgID: in.OrgID, Kind: in.Kind, Item: in.Item, Description: in.Name,
		Amount: in.Amount, Currency: in.Currency, Recurring: in.Recurring, Status: "pending", Provider: p.Name(), Mode: p.Mode(), ReturnPath: in.ReturnPath, CreatedAt: now, UpdatedAt: now}
	ret := s.PublicURL + in.ReturnPath
	sep := "?"
	if strings.Contains(ret, "?") {
		sep = "&"
	}
	co, err := p.Checkout(ctx, payments.CheckoutRequest{Reference: pur.ID, Tenant: store.TenantOf(ctx), Email: a.User.Email,
		Customer:   s.customerOf(ctx, a.User.ID, p.Name()),
		Items:      []payments.LineItem{{Name: in.Name, Description: in.Description, Amount: in.Amount, Currency: in.Currency, Recurring: in.Recurring, TaxCode: in.TaxCode}},
		SuccessURL: ret + sep + "purchase=" + pur.ID + "&result=ok", CancelURL: ret + sep + "purchase=" + pur.ID + "&result=cancel",
		Locale: s.locale(a.User), Metadata: map[string]string{"kind": in.Kind}})
	if err != nil {
		if msg := providerMessage(err); msg != "" {
			return PurchaseCheckout{}, invalid("validation.payments_provider_error", map[string]any{"msg": msg}, "the provider answered: %s", msg)
		}
		return PurchaseCheckout{}, err
	}
	pur.CheckoutRef = co.ID
	if err := s.DB.SavePurchase(ctx, pur); err != nil {
		return PurchaseCheckout{}, err
	}
	s.event(ctx, a, "payments.checkout", pur.ID, map[string]any{"kind": in.Kind, "amount": in.Amount, "currency": in.Currency, "mode": p.Mode()})
	return PurchaseCheckout{ID: pur.ID, URL: co.URL}, nil
}

// providerMessage is the provider's own error text ("" for other errors).
func providerMessage(err error) string {
	var se *payments.StripeError
	if errors.As(err, &se) {
		return se.Message
	}
	var pe *payments.PaddleError
	if errors.As(err, &pe) {
		return pe.Detail
	}
	return ""
}

func (s *Service) customerOf(ctx context.Context, userID, provider string) string {
	c, _ := s.DB.CustomerOf(ctx, userID, provider)
	return c
}

// TestPurchase opens a checkout for a small test item (system admins), to
// try the setup end to end – with test keys nothing is charged.
func (s *Service) TestPurchase(ctx context.Context, a Actor) (PurchaseCheckout, error) {
	if err := s.adminOnly(a); err != nil {
		return PurchaseCheckout{}, err
	}
	return s.StartPurchase(ctx, a, PurchaseInput{Kind: "test", Name: "Phoedox payment test", Description: "A test of the payment setup", Amount: 100, ReturnPath: "/admin/payments"})
}

// stripeTenant reads the tenant a Stripe event names (unverified – it only
// chooses whose webhook secret checks the signature).
func stripeTenant(body []byte) string {
	var ev struct {
		Data struct {
			Object struct {
				Metadata map[string]string `json:"metadata"`
				Parent   *struct {
					SubscriptionDetails *struct {
						Metadata map[string]string `json:"metadata"`
					} `json:"subscription_details"`
				} `json:"parent"`
			} `json:"object"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &ev) != nil {
		return ""
	}
	o := ev.Data.Object
	if t := o.Metadata["tenant"]; t != "" {
		return t
	}
	if o.Parent != nil && o.Parent.SubscriptionDetails != nil {
		return o.Parent.SubscriptionDetails.Metadata["tenant"]
	}
	return ""
}

var tenantIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// HandlePaymentWebhook verifies a provider event, stores it once and
// updates the purchase; the feature's handler then acts on it.
func (s *Service) HandlePaymentWebhook(ctx context.Context, provider string, h http.Header, body []byte) error {
	var tenant string
	switch provider {
	case "stripe":
		tenant = stripeTenant(body)
	case "paddle":
		tenant = payments.PaddleTenant(body)
	default:
		return ErrNotFound
	}
	if tenant == "" || !tenantIDRe.MatchString(tenant) {
		tenant = store.DefaultTenant
	}
	ctx = store.WithTenant(ctx, tenant)
	c := s.readPaymentConfig(ctx)
	p := s.providerFor(c)
	if p == nil || p.Name() != provider {
		return ErrNotFound
	}
	ev, err := p.Webhook(h, body)
	if err != nil {
		return invalid("validation.payments_signature", nil, "the signature does not match")
	}
	pur, perr := s.purchaseForEvent(ctx, ev)
	fresh, err := s.DB.RecordPaymentEvent(ctx, provider, ev.ID, tenant, ev.Type, ev.Original, pur.ID)
	if err != nil || !fresh {
		return err // a repeated delivery changes nothing
	}
	if perr != nil {
		return nil // an event for something we did not sell here
	}
	before := pur.Status
	switch ev.Type {
	case payments.EventPaid:
		pur.Status = "paid"
	case payments.EventPending:
		if pur.Status == "pending" || pur.Status == "" {
			pur.Status = "pending"
		}
	case payments.EventFailed:
		pur.Status = "failed"
	case payments.EventExpired:
		if pur.Status == "pending" {
			pur.Status = "expired"
		}
	case payments.EventRefunded:
		pur.Status = "refunded"
	case payments.EventSubCanceled:
		pur.Status = "canceled"
	}
	if ev.Payment != "" {
		pur.PaymentRef = ev.Payment
	}
	if ev.Subscription != "" {
		pur.Subscription = ev.Subscription
	}
	if ev.Customer != "" {
		pur.CustomerRef = ev.Customer
	}
	pur.UpdatedAt = store.Now()
	if err := s.DB.SavePurchase(ctx, pur); err != nil {
		return err
	}
	s.event(ctx, Actor{Via: "system"}, "payments."+strings.ReplaceAll(ev.Type, ".", "_"), pur.ID, map[string]any{"from": before, "to": pur.Status, "event": ev.Original})
	s.paymentMu.Lock()
	hdl := s.paymentHandlers[pur.Kind]
	s.paymentMu.Unlock()
	if hdl != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("payment handler", "kind", pur.Kind, "panic", fmt.Sprint(r))
				}
			}()
			hdl(ctx, pur, ev)
		}()
	}
	return nil
}

func (s *Service) purchaseForEvent(ctx context.Context, ev payments.Event) (store.Purchase, error) {
	if ev.Reference != "" {
		if p, err := s.DB.GetPurchase(ctx, ev.Reference); err == nil {
			return p, nil
		}
	}
	return s.DB.PurchaseBy(ctx, ev.Subscription, ev.Payment)
}

// AdminPurchases lists the latest purchases (system admins).
func (s *Service) AdminPurchases(ctx context.Context, a Actor) ([]store.Purchase, error) {
	if err := s.adminOnly(a); err != nil {
		return nil, err
	}
	return s.DB.Purchases(ctx, "", 200)
}

// MyPurchases lists a person's purchases.
func (s *Service) MyPurchases(ctx context.Context, a Actor) ([]store.Purchase, error) {
	if err := s.fullUser(a); err != nil {
		return nil, err
	}
	return s.DB.Purchases(ctx, a.User.ID, 200)
}

// RefundPurchase refunds a paid purchase in full (system admins). The
// purchase becomes "refunded" when the provider's event arrives.
func (s *Service) RefundPurchase(ctx context.Context, a Actor, id string) error {
	if err := s.adminOnly(a); err != nil {
		return err
	}
	pur, err := s.DB.GetPurchase(ctx, id)
	if err != nil {
		return err
	}
	if pur.Status != "paid" || pur.PaymentRef == "" {
		return invalid("validation.payments_not_refundable", nil, "only paid one-time purchases can be refunded here")
	}
	p := s.PaymentProvider(ctx)
	if p == nil || p.Name() != pur.Provider {
		return invalid("validation.payments_off", nil, "payments are switched off")
	}
	if err := p.Refund(ctx, pur.PaymentRef, 0); err != nil {
		return invalid("validation.payments_provider_error", map[string]any{"msg": err.Error()}, "the provider answered: %s", err.Error())
	}
	s.event(ctx, a, "payments.refund", pur.ID, nil)
	return nil
}

// PaymentPortal opens the provider's page where people manage their payment
// methods, invoices and subscriptions.
func (s *Service) PaymentPortal(ctx context.Context, a Actor, returnPath string) (string, error) {
	if err := s.fullUser(a); err != nil {
		return "", err
	}
	p := s.PaymentProvider(ctx)
	if p == nil {
		return "", invalid("validation.payments_off", nil, "payments are switched off")
	}
	cust, err := s.DB.CustomerOf(ctx, a.User.ID, p.Name())
	if err != nil {
		return "", invalid("validation.payments_no_customer", nil, "you have not bought anything yet")
	}
	if !strings.HasPrefix(returnPath, "/") {
		returnPath = "/settings/billing"
	}
	return p.Portal(ctx, cust, s.PublicURL+returnPath)
}

// PayInfo is what the /pay page needs to open a provider's embedded
// checkout (Paddle.js): public values only.
type PayInfo struct {
	Provider    string `json:"provider"`
	ClientToken string `json:"clientToken"`
	Sandbox     bool   `json:"sandbox"`
	Locale      string `json:"locale,omitempty"`
	SuccessURL  string `json:"successUrl"`
	CancelURL   string `json:"cancelUrl"`
}

// PaymentPayInfo looks up a started checkout by the provider's reference
// (Paddle's transaction id from ?_ptxn=). Public: the reference is a
// capability handed out by the provider.
func (s *Service) PaymentPayInfo(ctx context.Context, ref string) (PayInfo, error) {
	c := s.readPaymentConfig(ctx)
	if c.Provider != "paddle" || !strings.HasPrefix(ref, "txn_") {
		return PayInfo{}, ErrNotFound
	}
	pur, err := s.DB.PurchaseByCheckout(ctx, ref)
	if err != nil || pur.Provider != "paddle" {
		return PayInfo{}, ErrNotFound
	}
	k := c.keys()
	ret := s.PublicURL + pur.ReturnPath
	if pur.ReturnPath == "" {
		ret = s.PublicURL + "/settings/billing"
	}
	sep := "?"
	if strings.Contains(ret, "?") {
		sep = "&"
	}
	return PayInfo{Provider: "paddle", ClientToken: k.ClientToken, Sandbox: strings.HasPrefix(k.ClientToken, "test_"),
		SuccessURL: ret + sep + "purchase=" + pur.ID + "&result=ok", CancelURL: ret + sep + "purchase=" + pur.ID + "&result=cancel"}, nil
}
