package requests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"syscall"
	"time"
)

// Limits for executed requests.
const (
	Timeout      = 15 * time.Second
	MaxBody      = 1 << 20 // response bytes kept
	MaxRedirects = 3
)

// Policy decides which addresses may be reached.
type Policy struct {
	// AllowPrivate allows private networks and loopback (home IT). Hosted
	// instances turn it off.
	AllowPrivate bool
}

// ErrBlocked is returned for addresses the policy forbids.
var ErrBlocked = errors.New("requests: address not allowed")

func (p Policy) allowed(ip net.IP) bool {
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false // e.g. cloud metadata at 169.254.169.254
	}
	if ip.IsLoopback() || ip.IsPrivate() {
		return p.AllowPrivate
	}
	return true
}

// Client returns an HTTP client that checks every address it connects to
// (after DNS resolution, so rebinding does not help).
func (p Policy) Client() *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		if ip := net.ParseIP(host); ip == nil || !p.allowed(ip) {
			return ErrBlocked
		}
		return nil
	}}
	tr := &http.Transport{DialContext: dialer.DialContext, Proxy: nil, TLSHandshakeTimeout: 5 * time.Second,
		ResponseHeaderTimeout: Timeout, MaxIdleConns: 10, IdleConnTimeout: 30 * time.Second}
	return &http.Client{Transport: tr, Timeout: Timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= MaxRedirects {
			return errors.New("too many redirects")
		}
		return nil
	}}
}

// Result is what the web app shows.
type Result struct {
	Status    int         `json:"status"`
	TimeMs    int64       `json:"timeMs"`
	URL       string      `json:"url"`
	Headers   [][2]string `json:"headers"`
	Body      string      `json:"body"`
	Truncated bool        `json:"truncated,omitempty"`
	Checks    []Check     `json:"checks"`
	Error     string      `json:"error,omitempty"` // network error (masked)
}

// Run sends a filled request. Network errors are part of the result.
func Run(ctx context.Context, client *http.Client, r Request) Result {
	res := Result{URL: r.URL}
	u, err := url.Parse(r.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		res.Error = "the URL must start with http:// or https://"
		return res
	}
	var body io.Reader
	if r.Body != "" {
		body = strings.NewReader(r.Body)
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, r.URL, body)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	req.Header.Set("User-Agent", "Phoedox")
	for _, h := range r.Headers {
		req.Header.Add(h[0], h[1])
	}
	start := time.Now()
	resp, err := client.Do(req)
	res.TimeMs = time.Since(start).Milliseconds()
	if err != nil {
		if errors.Is(err, ErrBlocked) {
			res.Error = "this address is not allowed on this instance"
		} else {
			res.Error = err.Error()
		}
		return res
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, MaxBody+1))
	if len(data) > MaxBody {
		data, res.Truncated = data[:MaxBody], true
	}
	res.Status = resp.StatusCode
	res.Checks = Evaluate(r.Expects, resp.StatusCode, res.TimeMs, resp.Header.Get, data)
	keys := make([]string, 0, len(resp.Header))
	for k := range resp.Header {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		res.Headers = append(res.Headers, [2]string{k, strings.Join(resp.Header[k], ", ")})
	}
	if strings.Contains(resp.Header.Get("Content-Type"), "json") {
		var pretty bytes.Buffer
		if json.Indent(&pretty, data, "", "  ") == nil {
			data = pretty.Bytes()
		}
	}
	res.Body = string(bytes.ToValidUTF8(data, []byte("�")))
	return res
}

// Mask replaces secret values in the whole result.
func (r *Result) Mask(secrets []string) {
	m := func(s string) string {
		for _, sec := range secrets {
			if len(sec) >= 4 {
				s = strings.ReplaceAll(s, sec, "••••")
			}
		}
		return s
	}
	r.URL, r.Body, r.Error = m(r.URL), m(r.Body), m(r.Error)
	for i := range r.Headers {
		r.Headers[i][1] = m(r.Headers[i][1])
	}
	for i := range r.Checks {
		r.Checks[i].Actual = m(r.Checks[i].Actual)
	}
}

// Safe reports whether a method only reads.
func Safe(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}
