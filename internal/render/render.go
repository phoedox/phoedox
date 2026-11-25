// Package render drives a headless Chrome that runs as its own service
// (ADR-0095: chromedp/headless-shell, started by the Helm chart or
// docker-compose, switched on by the system admin). Phoedox keeps one
// connection to it and opens a tab per job: pictures of HTML, of Phoedox
// pages (signed in with a short-lived read-only pass) and PDFs.
package render

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/remote"
)

// ErrOff: no renderer is configured.
var ErrOff = errors.New("render: no renderer configured")

// Renderer is a connection to a headless Chrome (DevTools protocol).
type Renderer struct {
	// URL of the DevTools endpoint, e.g. http://phoedox-renderer:9222.
	URL string
	// Timeout of one job (default 30 s).
	Timeout time.Duration

	mu      sync.Mutex
	browser context.Context
	cancel  context.CancelFunc
	sem     chan struct{}
}

// New returns a renderer for the DevTools endpoint at u ("" = off) that
// runs at most parallel jobs at a time.
func New(u string, parallel int) *Renderer {
	if parallel < 1 {
		parallel = 2
	}
	return &Renderer{URL: strings.TrimRight(strings.TrimSpace(u), "/"), Timeout: 30 * time.Second, sem: make(chan struct{}, parallel)}
}

// Configured tells whether a renderer address is set.
func (r *Renderer) Configured() bool { return r != nil && r.URL != "" }

// Status is what the administration shows.
type Status struct {
	Configured bool   `json:"configured"`
	Reachable  bool   `json:"reachable"`
	Browser    string `json:"browser,omitempty"`
	Error      string `json:"error,omitempty"`
}

// Status asks the service for its version.
func (r *Renderer) Status(ctx context.Context) Status {
	if !r.Configured() {
		return Status{}
	}
	st := Status{Configured: true}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	base, err := endpoint(ctx, r.URL)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/json/version", nil)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	defer res.Body.Close()
	var v struct {
		Browser string `json:"Browser"`
	}
	if err := json.NewDecoder(res.Body).Decode(&v); err != nil || res.StatusCode != http.StatusOK {
		st.Error = fmt.Sprintf("unexpected answer (%d)", res.StatusCode)
		return st
	}
	st.Reachable, st.Browser = true, v.Browser
	return st
}

// endpoint is the DevTools base address with the host as an IP: Chrome
// answers only requests whose Host is an IP address or localhost (DNS
// rebinding protection), and in a cluster the service has a name.
func endpoint(ctx context.Context, u string) (string, error) {
	base := httpBase(u)
	p, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	host := p.Hostname()
	if host == "" || host == "localhost" || net.ParseIP(host) != nil {
		return base, nil
	}
	ips, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil || len(ips) == 0 {
		return "", fmt.Errorf("render: resolve %s: %w", host, err)
	}
	if port := p.Port(); port != "" {
		p.Host = net.JoinHostPort(ips[0], port)
	} else {
		p.Host = ips[0]
	}
	return p.String(), nil
}

func httpBase(u string) string {
	u = strings.Replace(u, "ws://", "http://", 1)
	u = strings.Replace(u, "wss://", "https://", 1)
	if p, err := url.Parse(u); err == nil {
		p.Path, p.RawQuery = "", ""
		return strings.TrimRight(p.String(), "/")
	}
	return u
}

// tab opens a new tab in the (re)connected browser.
func (r *Renderer) tab(ctx context.Context) (context.Context, context.CancelFunc, error) {
	if !r.Configured() {
		return nil, nil, ErrOff
	}
	select {
	case r.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
	release := func() { <-r.sem }
	r.mu.Lock()
	if r.browser == nil || r.browser.Err() != nil {
		base, err := endpoint(ctx, r.URL)
		if err != nil {
			r.mu.Unlock()
			release()
			return nil, nil, err
		}
		alloc, cancelAlloc := remote.NewAllocator(context.Background(), base+"/")
		b, cancelB := chromedp.NewContext(alloc)
		if err := chromedp.Do(b); err != nil {
			cancelB()
			cancelAlloc()
			r.mu.Unlock()
			release()
			return nil, nil, fmt.Errorf("render: connect: %w", err)
		}
		r.browser, r.cancel = b, func() { cancelB(); cancelAlloc() }
	}
	b := r.browser
	r.mu.Unlock()
	t, cancelTab := chromedp.NewContext(b)
	t, cancelTime := context.WithTimeout(t, r.timeout())
	// The job ends with the caller too.
	stop := context.AfterFunc(ctx, cancelTab)
	return t, func() { stop(); cancelTime(); cancelTab(); release() }, nil
}

func (r *Renderer) timeout() time.Duration {
	if r.Timeout <= 0 {
		return 30 * time.Second
	}
	return r.Timeout
}

// Close drops the connection (the service keeps running).
func (r *Renderer) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		r.cancel()
		r.browser, r.cancel = nil, nil
	}
}

// call runs a protocol command as a step.
func call[P any](cmd cdp.Command[P, cdp.Empty], params P) chromedp.Action[chromedp.Void] {
	return chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		_, err := cdp.Call(ctx, t, cmd, params)
		return err
	})
}

func viewport(w, h int, scale float64) chromedp.Action[chromedp.Void] {
	return call(emulation.SetDeviceMetricsOverride, emulation.SetDeviceMetricsOverrideParams{Width: int64(max(w, 50)), Height: int64(max(h, 50)), DeviceScaleFactor: orOne(scale)})
}

// HTML draws a complete HTML document at w×h CSS pixels (scale: device
// pixel ratio) without network: every request is blocked, so the page can
// use only what it carries (inline CSS, data: URLs).
func (r *Renderer) HTML(ctx context.Context, doc string, w, h int, scale float64) ([]byte, error) {
	t, done, err := r.tab(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	err = chromedp.Do(t,
		call(network.Enable, network.EnableParams{}),
		call(network.SetBlockedURLs, network.SetBlockedURLsParams{URLPatterns: []*network.BlockPattern{{URLPattern: "*://*:*/*", Block: true}}}),
		viewport(w, h, scale),
		chromedp.Navigate("about:blank"),
		chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
			tree, err := cdp.Call(ctx, t, page.GetFrameTree, cdp.Empty{})
			if err != nil {
				return err
			}
			_, err = cdp.Call(ctx, t, page.SetDocumentContent, page.SetDocumentContentParams{FrameID: tree.FrameTree.Frame.ID, HTML: doc})
			return err
		}),
		settle(),
	)
	if err != nil {
		return nil, err
	}
	return chromedp.Run(t, chromedp.CaptureScreenshot())
}

// PageOptions describe a picture or PDF of a Phoedox page.
type PageOptions struct {
	// Headers go with every request of the page (the render pass).
	Headers map[string]string
	W, H    int
	Scale   float64
	// Ready: a JavaScript expression that is true when the page is done
	// (after the document has loaded).
	Ready string
	// Full: the whole page height instead of the viewport.
	Full bool
	// Dark: prefer the dark colour scheme.
	Dark bool
}

func (o PageOptions) steps(u string) []chromedp.Action[chromedp.Void] {
	hdr := network.Headers{}
	for k, v := range o.Headers {
		hdr[k] = v
	}
	scheme := "light"
	if o.Dark {
		scheme = "dark"
	}
	steps := []chromedp.Action[chromedp.Void]{
		call(network.Enable, network.EnableParams{}),
		call(network.SetExtraHTTPHeaders, network.SetExtraHTTPHeadersParams{Headers: hdr}),
		viewport(o.W, o.H, o.Scale),
		call(emulation.SetEmulatedMedia, emulation.SetEmulatedMediaParams{Features: []*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: scheme}}}),
		chromedp.Navigate(u),
	}
	if o.Ready != "" {
		steps = append(steps, chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
			_, err := chromedp.Poll[bool](o.Ready, chromedp.WithPollingInterval(150*time.Millisecond))(ctx, t)
			return err
		}))
	}
	return append(steps, settle())
}

// Page takes a picture of a page.
func (r *Renderer) Page(ctx context.Context, u string, o PageOptions) ([]byte, error) {
	t, done, err := r.tab(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	if err := chromedp.Do(t, o.steps(u)...); err != nil {
		return nil, err
	}
	if o.Full {
		return chromedp.Run(t, chromedp.FullScreenshot(100))
	}
	return chromedp.Run(t, chromedp.CaptureScreenshot())
}

// PDF prints a page as the browser's print view does (A4, backgrounds).
func (r *Renderer) PDF(ctx context.Context, u string, o PageOptions) ([]byte, error) {
	t, done, err := r.tab(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	if o.W == 0 {
		o.W, o.H = 1000, 1400
	}
	if err := chromedp.Do(t, o.steps(u)...); err != nil {
		return nil, err
	}
	return chromedp.Run(t, chromedp.PrintToPDF(func(p *page.PrintToPDFParams) {
		p.PrintBackground = ptr(true)
		p.PreferCSSPageSize = ptr(true)
	}))
}

// settle waits for fonts and two frames, so the picture shows the page as drawn.
func settle() chromedp.Action[chromedp.Void] {
	return chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		_, err := chromedp.Evaluate[bool](`(async () => { await document.fonts.ready; await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))); return true })()`,
			chromedp.EvalAwaitPromise)(ctx, t)
		return err
	})
}

func ptr[T any](v T) *T { return &v }

func orOne(f float64) float64 {
	if f <= 0 {
		return 1
	}
	return f
}
