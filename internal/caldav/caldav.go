// Package caldav is a small CalDAV (RFC 4791) and iCalendar-feed client for
// the calendar (ADR-0089): it checks a calendar collection, reads event
// occurrences in a time window, and writes or deletes single events that
// Phoedox owns. Feeds (http(s) and webcal URLs) are read with FetchICS.
//
// The HTTP client is supplied by the caller (it enforces timeouts and
// blocks private addresses). Passwords never appear in errors.
package caldav

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/ical"
)

// MaxResponseSize limits every response body read by this package.
const MaxResponseSize = 10 << 20

const userAgent = "Phoedox-CalDAV"

var (
	// ErrAuth means the server rejected the credentials (HTTP 401 or 403).
	ErrAuth = errors.New("caldav: authentication failed or access denied")
	// ErrNotCalendar means the collection is not a CalDAV calendar.
	ErrNotCalendar = errors.New("caldav: not a calendar collection")
	// ErrTooLarge means a response was larger than MaxResponseSize.
	ErrTooLarge = errors.New("caldav: response larger than 10 MB")
	// ErrBadURL means a URL is not an absolute http(s) URL.
	ErrBadURL = errors.New("caldav: URL must be an absolute http or https URL")
)

// StatusError is returned for unexpected HTTP statuses.
type StatusError struct {
	Method string
	URL    string // without user info
	Code   int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("caldav: %s %s: unexpected status %d %s", e.Method, e.URL, e.Code, http.StatusText(e.Code))
}

// Client talks to one CalDAV account.
type Client struct {
	HTTP     *http.Client // given by the caller (it blocks private addresses)
	User     string
	Password string // basic auth when User != ""
}

const (
	nsDAV    = "DAV:"
	nsCalDAV = "urn:ietf:params:xml:ns:caldav"
)

// Multistatus responses; encoding/xml matches the namespaces whatever
// prefixes the server uses.
type multistatus struct {
	XMLName   xml.Name   `xml:"DAV: multistatus"`
	Responses []response `xml:"DAV: response"`
}

type response struct {
	Href      string     `xml:"DAV: href"`
	Status    string     `xml:"DAV: status"`
	Propstats []propstat `xml:"DAV: propstat"`
}

type propstat struct {
	Prop   prop   `xml:"DAV: prop"`
	Status string `xml:"DAV: status"`
}

type prop struct {
	ResourceType *elements     `xml:"DAV: resourcetype"`
	DisplayName  *string       `xml:"DAV: displayname"`
	PrivilegeSet *privilegeSet `xml:"DAV: current-user-privilege-set"`
	GetETag      string        `xml:"DAV: getetag"`
	CalendarData string        `xml:"urn:ietf:params:xml:ns:caldav calendar-data"`
}

type elements struct {
	Elems []element `xml:",any"`
}

type element struct {
	XMLName xml.Name
}

func (e *elements) has(space, local string) bool {
	if e == nil {
		return false
	}
	for _, el := range e.Elems {
		if el.XMLName.Space == space && el.XMLName.Local == local {
			return true
		}
	}
	return false
}

type privilegeSet struct {
	Privileges []elements `xml:"DAV: privilege"`
}

// ok reports whether a propstat or response status line is 2xx (an
// absent status counts as OK).
func ok(status string) bool {
	f := strings.Fields(status)
	return len(f) < 2 || strings.HasPrefix(f[1], "2")
}

// Check reads the collection (PROPFIND depth 0): it must be a calendar
// (resourcetype <C:calendar/>); returns its display name and whether the
// user may write (current-user-privilege-set contains write or
// write-content or all; servers that do not report it count as writable).
func (c *Client) Check(ctx context.Context, collection string) (name string, writable bool, err error) {
	u, err := parseHTTPURL(collection)
	if err != nil {
		return "", false, err
	}
	body := `<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
  <d:prop><d:resourcetype/><d:displayname/><d:current-user-privilege-set/></d:prop>
</d:propfind>`
	ms, base, err := c.multistatus(ctx, "PROPFIND", u, "0", body)
	var se *StatusError
	if errors.As(err, &se) && se.Code == http.StatusMethodNotAllowed {
		return "", false, ErrNotCalendar // no WebDAV at this URL
	}
	if err != nil {
		return "", false, err
	}
	resp := pickResponse(ms, base)
	if resp == nil {
		return "", false, ErrNotCalendar
	}
	isCal, writable := false, true
	for _, ps := range resp.Propstats {
		if !ok(ps.Status) {
			continue
		}
		if ps.Prop.ResourceType.has(nsCalDAV, "calendar") {
			isCal = true
		}
		if ps.Prop.DisplayName != nil {
			name = strings.TrimSpace(*ps.Prop.DisplayName)
		}
		if set := ps.Prop.PrivilegeSet; set != nil {
			writable = false
			for _, p := range set.Privileges {
				if p.has(nsDAV, "write") || p.has(nsDAV, "write-content") || p.has(nsDAV, "all") {
					writable = true
				}
			}
		}
	}
	if !isCal {
		return "", false, ErrNotCalendar
	}
	return name, writable, nil
}

// pickResponse returns the multistatus response for the collection itself,
// or the first one when no href matches.
func pickResponse(ms *multistatus, collection *url.URL) *response {
	if len(ms.Responses) == 0 {
		return nil
	}
	want := strings.TrimSuffix(collection.EscapedPath(), "/")
	for i, r := range ms.Responses {
		if h, err := resolve(collection, r.Href); err == nil && strings.TrimSuffix(h.EscapedPath(), "/") == want {
			return &ms.Responses[i]
		}
	}
	return &ms.Responses[0]
}

// resolve resolves a multistatus href (often only a path) against the
// collection URL.
func resolve(base *url.URL, href string) (*url.URL, error) {
	ref, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return nil, err
	}
	return base.ResolveReference(ref), nil
}

// Events reads the occurrences in [from, to) with a calendar-query REPORT
// (Depth 1, time-range filter) asking the server to <C:expand>; the
// returned calendar-data is parsed with ical.Parse(data, from, to), so
// servers that ignore expand still work. Servers that reject expand with
// 400, 422 or 501 are asked again without it.
func (c *Client) Events(ctx context.Context, collection string, from, to time.Time) ([]ical.Event, error) {
	u, err := parseHTTPURL(collection)
	if err != nil {
		return nil, err
	}
	ms, _, err := c.multistatus(ctx, "REPORT", u, "1", calendarQuery(from, to, true))
	var se *StatusError
	if errors.As(err, &se) && (se.Code == http.StatusBadRequest || se.Code == http.StatusUnprocessableEntity || se.Code == http.StatusNotImplemented) {
		ms, _, err = c.multistatus(ctx, "REPORT", u, "1", calendarQuery(from, to, false))
	}
	if err != nil {
		return nil, err
	}
	var data bytes.Buffer
	for _, r := range ms.Responses {
		if !ok(r.Status) {
			continue
		}
		for _, ps := range r.Propstats {
			if ok(ps.Status) && strings.TrimSpace(ps.Prop.CalendarData) != "" {
				data.WriteString(ps.Prop.CalendarData)
				data.WriteString("\r\n")
			}
		}
	}
	if data.Len() == 0 {
		return []ical.Event{}, nil
	}
	evs, err := ical.Parse(data.Bytes(), from, to)
	if err != nil {
		return nil, fmt.Errorf("caldav: calendar data from %s: %w", u.Redacted(), err)
	}
	return evs, nil
}

func calendarQuery(from, to time.Time, expand bool) string {
	const layout = "20060102T150405Z"
	start, end := from.UTC().Format(layout), to.UTC().Format(layout)
	data := `<c:calendar-data/>`
	if expand {
		data = `<c:calendar-data><c:expand start="` + start + `" end="` + end + `"/></c:calendar-data>`
	}
	return `<?xml version="1.0" encoding="utf-8"?>
<c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
  <d:prop><d:getetag/>` + data + `</d:prop>
  <c:filter>
    <c:comp-filter name="VCALENDAR">
      <c:comp-filter name="VEVENT">
        <c:time-range start="` + start + `" end="` + end + `"/>
      </c:comp-filter>
    </c:comp-filter>
  </c:filter>
</c:calendar-query>`
}

// Put writes one event as <collection>/<safe name>.ics (safe name: the UID
// with everything outside [A-Za-z0-9._-] replaced by -, at most 120 chars)
// with Content-Type text/calendar; charset=utf-8. It returns the ETag; when
// the server does not send one with the PUT response, it is read with a
// PROPFIND (and "" is returned if that fails too).
func (c *Client) Put(ctx context.Context, collection string, ev ical.Event, prodID string) (etag string, err error) {
	u, err := c.resourceURL(collection, ev.UID)
	if err != nil {
		return "", err
	}
	body := ical.Encode(prodID, []ical.Event{ev})
	resp, err := c.do(ctx, http.MethodPut, u, map[string]string{"Content-Type": "text/calendar; charset=utf-8"}, body)
	if err != nil {
		return "", err
	}
	drain(resp)
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusNoContent:
	default:
		return "", statusErr(http.MethodPut, u, resp.StatusCode)
	}
	if etag = resp.Header.Get("ETag"); etag != "" {
		return etag, nil
	}
	ms, base, err := c.multistatus(ctx, "PROPFIND", u, "0",
		`<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:prop><d:getetag/></d:prop></d:propfind>`)
	if err == nil {
		if r := pickResponse(ms, base); r != nil {
			for _, ps := range r.Propstats {
				if ok(ps.Status) && ps.Prop.GetETag != "" {
					return strings.TrimSpace(ps.Prop.GetETag), nil
				}
			}
		}
	}
	return "", nil
}

// Delete removes the event Put stored for this UID (404 counts as done).
func (c *Client) Delete(ctx context.Context, collection, uid string) error {
	u, err := c.resourceURL(collection, uid)
	if err != nil {
		return err
	}
	resp, err := c.do(ctx, http.MethodDelete, u, nil, nil)
	if err != nil {
		return err
	}
	drain(resp)
	switch resp.StatusCode {
	case http.StatusOK, http.StatusAccepted, http.StatusNoContent, http.StatusNotFound, http.StatusGone:
		return nil
	}
	return statusErr(http.MethodDelete, u, resp.StatusCode)
}

// SafeName is the resource name (without .ics) Put uses for a UID: every
// character outside [A-Za-z0-9._-] becomes -, cut to 120 characters.
func SafeName(uid string) string {
	var b strings.Builder
	for _, r := range uid {
		if b.Len() >= 120 {
			break
		}
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

func (c *Client) resourceURL(collection, uid string) (*url.URL, error) {
	u, err := parseHTTPURL(collection)
	if err != nil {
		return nil, err
	}
	name := SafeName(uid)
	if name == "" {
		return nil, errors.New("caldav: event has no UID")
	}
	r := *u
	r.RawQuery, r.Fragment = "", ""
	r.Path = strings.TrimSuffix(u.Path, "/") + "/" + name + ".ics"
	r.RawPath = ""
	if u.RawPath != "" {
		r.RawPath = strings.TrimSuffix(u.RawPath, "/") + "/" + name + ".ics"
	}
	return &r, nil
}

// FetchICS reads a plain iCalendar URL (webcal:// becomes https://) and
// returns the occurrences in [from, to).
func FetchICS(ctx context.Context, hc *http.Client, rawURL string, from, to time.Time) ([]ical.Event, error) {
	s := strings.TrimSpace(rawURL)
	if l := strings.ToLower(s); strings.HasPrefix(l, "webcal://") {
		s = "https://" + s[len("webcal://"):]
	} else if strings.HasPrefix(l, "webcals://") {
		s = "https://" + s[len("webcals://"):]
	}
	u, err := parseHTTPURL(s)
	if err != nil {
		return nil, err
	}
	c := &Client{HTTP: hc}
	if u.User != nil {
		// Credentials in the URL: send them as basic auth, keep them out of errors.
		c.User = u.User.Username()
		c.Password, _ = u.User.Password()
		u.User = nil
	}
	resp, err := c.do(ctx, http.MethodGet, u, map[string]string{"Accept": "text/calendar, */*;q=0.5"}, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, statusErr(http.MethodGet, u, resp.StatusCode)
	}
	data, err := readLimited(resp.Body)
	if err != nil {
		return nil, err
	}
	evs, err := ical.Parse(data, from, to)
	if err != nil {
		return nil, fmt.Errorf("caldav: %s: %w", u.Redacted(), err)
	}
	return evs, nil
}

// parseHTTPURL accepts only absolute http(s) URLs with a host.
func parseHTTPURL(s string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil {
		return nil, ErrBadURL // the parse error would echo the URL, password included
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, ErrBadURL
	}
	return u, nil
}

func (c *Client) do(ctx context.Context, method string, u *url.URL, headers map[string]string, body []byte) (*http.Response, error) {
	if c.HTTP == nil {
		return nil, errors.New("caldav: no HTTP client")
	}
	// net/http turns PROPFIND, REPORT, PUT and DELETE into GET on a 301 or
	// 302. Stop there instead and repeat the request with its own method.
	hc := *c.HTTP
	orig := c.HTTP.CheckRedirect
	hc.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.Method != via[0].Method {
			return http.ErrUseLastResponse
		}
		if orig != nil {
			return orig(req, via)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
	auth := c.User != ""
	for hop := 0; ; hop++ {
		var rd io.Reader
		if body != nil {
			rd = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, u.String(), rd)
		if err != nil {
			return nil, fmt.Errorf("caldav: %s %s: invalid request", method, u.Redacted())
		}
		req.Header.Set("User-Agent", userAgent)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		if auth {
			req.SetBasicAuth(c.User, c.Password)
		}
		resp, err := hc.Do(req)
		if err != nil {
			// net/http strips passwords from URLs in its errors; add our own
			// redacted context and keep the cause (timeouts, blocked dials).
			var ue *url.Error
			if errors.As(err, &ue) {
				err = ue.Err
			}
			return nil, fmt.Errorf("caldav: %s %s: %w", method, u.Redacted(), err)
		}
		if (resp.StatusCode == http.StatusMovedPermanently || resp.StatusCode == http.StatusFound) &&
			method != http.MethodGet && hop < 5 {
			if next, err := resolve(u, resp.Header.Get("Location")); err == nil && resp.Header.Get("Location") != "" &&
				(next.Scheme == "http" || next.Scheme == "https") {
				drain(resp)
				if next.Host != u.Host {
					auth = false // never send the password to another host
				}
				u = next
				continue
			}
		}
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			drain(resp)
			return nil, ErrAuth
		}
		return resp, nil
	}
}

// multistatus sends a PROPFIND or REPORT and parses the 207 response. The
// returned URL is the one that answered (after redirects); hrefs resolve
// against it.
func (c *Client) multistatus(ctx context.Context, method string, u *url.URL, depth, body string) (*multistatus, *url.URL, error) {
	resp, err := c.do(ctx, method, u, map[string]string{
		"Depth":        depth,
		"Content-Type": "application/xml; charset=utf-8",
	}, []byte(body))
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMultiStatus && resp.StatusCode != http.StatusOK {
		drain(resp)
		return nil, nil, statusErr(method, u, resp.StatusCode)
	}
	data, err := readLimited(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	var ms multistatus
	if err := xml.Unmarshal(data, &ms); err != nil {
		if method == "PROPFIND" && !bytes.Contains(data, []byte("multistatus")) {
			return nil, nil, ErrNotCalendar // e.g. an HTML page instead of a WebDAV collection
		}
		return nil, nil, fmt.Errorf("caldav: %s %s: bad multistatus response: %w", method, u.Redacted(), err)
	}
	base := u
	if resp.Request != nil && resp.Request.URL != nil {
		base = resp.Request.URL
	}
	return &ms, base, nil
}

func statusErr(method string, u *url.URL, code int) error {
	return &StatusError{Method: method, URL: u.Redacted(), Code: code}
}

// readLimited reads at most MaxResponseSize bytes and fails beyond that.
func readLimited(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxResponseSize+1))
	if err != nil {
		return nil, fmt.Errorf("caldav: reading response: %w", err)
	}
	if len(data) > MaxResponseSize {
		return nil, ErrTooLarge
	}
	return data, nil
}

// drain discards a little of the body so the connection can be reused,
// then closes it.
func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
}
