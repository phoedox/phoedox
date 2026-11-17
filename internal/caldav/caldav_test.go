package caldav

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/ical"
)

const (
	user     = "alice"
	password = "s3cret-pass"
)

// masterICS is a weekly event with one moved occurrence, as a CalDAV
// server stores it.
const masterICS = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\n" +
	"BEGIN:VEVENT\r\nUID:weekly-1\r\nSUMMARY:Weekly & more\r\nDTSTART;TZID=Europe/Berlin:20260105T100000\r\n" +
	"DTEND;TZID=Europe/Berlin:20260105T110000\r\nRRULE:FREQ=WEEKLY;COUNT=4\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:weekly-1\r\nRECURRENCE-ID;TZID=Europe/Berlin:20260112T100000\r\nSUMMARY:Weekly (moved)\r\n" +
	"DTSTART;TZID=Europe/Berlin:20260113T100000\r\nDTEND;TZID=Europe/Berlin:20260113T110000\r\nEND:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

// expandedICS is what a server that supports <C:expand> returns for
// masterICS in January 2026.
const expandedICS = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\n" +
	"BEGIN:VEVENT\r\nUID:weekly-1\r\nSUMMARY:Weekly & more\r\nRECURRENCE-ID:20260105T090000Z\r\nDTSTART:20260105T090000Z\r\nDTEND:20260105T100000Z\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:weekly-1\r\nSUMMARY:Weekly (moved)\r\nRECURRENCE-ID:20260112T090000Z\r\nDTSTART:20260113T090000Z\r\nDTEND:20260113T100000Z\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:weekly-1\r\nSUMMARY:Weekly & more\r\nRECURRENCE-ID:20260119T090000Z\r\nDTSTART:20260119T090000Z\r\nDTEND:20260119T100000Z\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:weekly-1\r\nSUMMARY:Weekly & more\r\nRECURRENCE-ID:20260126T090000Z\r\nDTSTART:20260126T090000Z\r\nDTEND:20260126T100000Z\r\nEND:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

const singleICS = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\n" +
	"BEGIN:VEVENT\r\nUID:single-1\r\nSUMMARY:Dentist\r\nDTSTART;VALUE=DATE:20260120\r\nEND:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

type resource struct {
	data []byte
	etag string
}

// fakeServer behaves like a small CalDAV server.
//
//	/dav/cal/       calendar, writable, supports expand (prefixes D: and C:)
//	/dav/ro/        calendar, read only (prefixes d: and cal:, default ns)
//	/dav/noprivs/   calendar that does not report privileges
//	/dav/files/     plain WebDAV collection
//	/dav/noexpand/  calendar that ignores <C:expand>
//	/dav/reject/    calendar that answers 501 to <C:expand>
//	/dav/noetag/    calendar whose PUT response has no ETag
//	/dav/moved/     301 to /dav/cal/
//	/dav/html/      answers PROPFIND with an HTML page
//	/dav/big/       REPORT answer larger than 10 MB
type fakeServer struct {
	t     *testing.T
	mu    sync.Mutex
	res   map[string]resource
	n     int
	last  *http.Request
	lastB []byte
}

func newFake(t *testing.T) (*fakeServer, *httptest.Server) {
	f := &fakeServer{t: t, res: map[string]resource{
		"/dav/cal/weekly-1.ics":      {[]byte(masterICS), `"m1"`},
		"/dav/cal/single-1.ics":      {[]byte(singleICS), `"s1"`},
		"/dav/noexpand/weekly-1.ics": {[]byte(masterICS), `"m1"`},
		"/dav/reject/weekly-1.ics":   {[]byte(masterICS), `"m1"`},
	}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

func esc(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func (f *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.last, f.lastB = r, body
	if r.Header.Get("User-Agent") != "Phoedox-CalDAV" {
		http.Error(w, "bad user agent", http.StatusBadRequest)
		return
	}
	if u, p, ok := r.BasicAuth(); !ok || u != user || p != password {
		w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	path := r.URL.Path
	if !strings.HasSuffix(path, "/") && !strings.HasSuffix(path, ".ics") {
		path += "/" // collections are reachable without the trailing slash
	}
	coll := path[:strings.LastIndex(path, "/")+1]
	switch {
	case coll == "/dav/moved/":
		http.Redirect(w, r, "/dav/cal/"+strings.TrimPrefix(path, "/dav/moved/"), http.StatusMovedPermanently)
		return
	case coll == "/dav/html/":
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html><body>Hello</body></html>")
		return
	}
	switch r.Method {
	case "PROPFIND":
		f.propfind(w, r, path, body)
	case "REPORT":
		f.report(w, r, coll, body)
	case http.MethodPut:
		if coll == "/dav/nope/" {
			http.Error(w, "no such collection", http.StatusConflict)
			return
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "text/calendar") {
			http.Error(w, "bad content type", http.StatusUnsupportedMediaType)
			return
		}
		if _, err := ical.Parse(body, time.Time{}, time.Time{}); err != nil {
			http.Error(w, "bad calendar data", http.StatusBadRequest)
			return
		}
		_, exists := f.res[path]
		f.n++
		etag := fmt.Sprintf(`"e%d"`, f.n)
		f.res[path] = resource{body, etag}
		if coll != "/dav/noetag/" {
			w.Header().Set("ETag", etag)
		}
		if exists {
			w.WriteHeader(http.StatusNoContent)
		} else {
			w.WriteHeader(http.StatusCreated)
		}
	case http.MethodDelete:
		if _, ok := f.res[path]; !ok {
			http.NotFound(w, r)
			return
		}
		delete(f.res, path)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (f *fakeServer) propfind(w http.ResponseWriter, r *http.Request, path string, body []byte) {
	if r.Header.Get("Depth") != "0" {
		http.Error(w, "depth", http.StatusBadRequest)
		return
	}
	if !bytes.Contains(body, []byte("propfind")) {
		http.Error(w, "body", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusMultiStatus)
	if strings.HasSuffix(path, ".ics") {
		res, ok := f.res[path]
		if !ok {
			fmt.Fprintf(w, `<D:multistatus xmlns:D="DAV:"><D:response><D:href>%s</D:href><D:status>HTTP/1.1 404 Not Found</D:status></D:response></D:multistatus>`, path)
			return
		}
		fmt.Fprintf(w, `<D:multistatus xmlns:D="DAV:"><D:response><D:href>%s</D:href><D:propstat><D:prop><D:getetag>%s</D:getetag></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response></D:multistatus>`, path, esc(res.etag))
		return
	}
	switch path {
	case "/dav/cal/", "/dav/noexpand/", "/dav/reject/", "/dav/noetag/":
		// A second response for a child first: the client must pick the collection by href.
		fmt.Fprintf(w, `<?xml version="1.0"?>
<D:multistatus xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
 <D:response><D:href>%sother.ics</D:href><D:propstat><D:prop><D:displayname>Not me</D:displayname><D:resourcetype/></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response>
 <D:response>
  <D:href>%s</D:href>
  <D:propstat><D:prop>
   <D:resourcetype><D:collection/><C:calendar/></D:resourcetype>
   <D:displayname>Family &amp; home</D:displayname>
   <D:current-user-privilege-set><D:privilege><D:read/></D:privilege><D:privilege><D:write-content/></D:privilege></D:current-user-privilege-set>
  </D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat>
 </D:response>
</D:multistatus>`, path, path)
	case "/dav/ro/":
		// Full URL as href, other prefixes, DAV: as default namespace.
		fmt.Fprintf(w, `<?xml version="1.0"?>
<multistatus xmlns="DAV:" xmlns:cal="urn:ietf:params:xml:ns:caldav">
 <response><href>http://%s/dav/ro</href>
  <propstat><prop>
   <resourcetype><collection/><cal:calendar/></resourcetype>
   <displayname>Holidays</displayname>
   <current-user-privilege-set><privilege><read/></privilege><privilege><read-current-user-privilege-set/></privilege></current-user-privilege-set>
  </prop><status>HTTP/1.1 200 OK</status></propstat>
 </response>
</multistatus>`, r.Host)
	case "/dav/noprivs/":
		fmt.Fprint(w, `<d:multistatus xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
 <d:response><d:href>/dav/noprivs/</d:href>
  <d:propstat><d:prop><d:resourcetype><d:collection/><c:calendar/></d:resourcetype><d:displayname>Work</d:displayname></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat>
  <d:propstat><d:prop><d:current-user-privilege-set/></d:prop><d:status>HTTP/1.1 404 Not Found</d:status></d:propstat>
 </d:response>
</d:multistatus>`)
	case "/dav/files/":
		// A calendar element in the wrong namespace does not count.
		fmt.Fprint(w, `<d:multistatus xmlns:d="DAV:" xmlns:x="urn:example:other">
 <d:response><d:href>/dav/files/</d:href>
  <d:propstat><d:prop><d:resourcetype><d:collection/><x:calendar/></d:resourcetype><d:displayname>Files</d:displayname></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat>
 </d:response>
</d:multistatus>`)
	default:
		fmt.Fprintf(w, `<d:multistatus xmlns:d="DAV:"><d:response><d:href>%s</d:href><d:status>HTTP/1.1 404 Not Found</d:status></d:response></d:multistatus>`, path)
	}
}

func (f *fakeServer) report(w http.ResponseWriter, r *http.Request, coll string, body []byte) {
	if r.Header.Get("Depth") != "1" {
		http.Error(w, "depth", http.StatusBadRequest)
		return
	}
	s := string(body)
	if !strings.Contains(s, "calendar-query") || !strings.Contains(s, `time-range start="`) {
		http.Error(w, "query", http.StatusBadRequest)
		return
	}
	expand := strings.Contains(s, "expand")
	if coll == "/dav/reject/" && expand {
		http.Error(w, "expand not supported", http.StatusNotImplemented)
		return
	}
	if coll == "/dav/big/" {
		w.WriteHeader(http.StatusMultiStatus)
		fmt.Fprint(w, `<d:multistatus xmlns:d="DAV:"><d:response><d:href>x</d:href><!--`)
		chunk := bytes.Repeat([]byte("x"), 1<<20)
		for range 11 {
			_, _ = w.Write(chunk)
		}
		fmt.Fprint(w, `--></d:response></d:multistatus>`)
		return
	}
	var paths []string
	for p := range f.res {
		if strings.HasPrefix(p, coll) {
			paths = append(paths, p)
		}
	}
	slices.Sort(paths)
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusMultiStatus)
	fmt.Fprint(w, `<?xml version="1.0"?><x:multistatus xmlns:x="DAV:" xmlns:y="urn:ietf:params:xml:ns:caldav">`)
	for _, p := range paths {
		data := string(f.res[p].data)
		if expand && coll == "/dav/cal/" && strings.HasSuffix(p, "weekly-1.ics") {
			data = expandedICS
		}
		fmt.Fprintf(w, `<x:response><x:href>%s</x:href><x:propstat><x:prop><x:getetag>%s</x:getetag><y:calendar-data>%s</y:calendar-data></x:prop><x:status>HTTP/1.1 200 OK</x:status></x:propstat></x:response>`,
			p, esc(f.res[p].etag), esc(data))
	}
	// A broken resource the server reports as 404 is skipped.
	fmt.Fprint(w, `<x:response><x:href>/gone.ics</x:href><x:propstat><x:prop><y:calendar-data>garbage</y:calendar-data></x:prop><x:status>HTTP/1.1 404 Not Found</x:status></x:propstat></x:response>`)
	fmt.Fprint(w, `</x:multistatus>`)
}

func client(srv *httptest.Server) *Client {
	hc := srv.Client()
	hc.Timeout = 10 * time.Second
	return &Client{HTTP: hc, User: user, Password: password}
}

var (
	jan  = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	feb  = time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	ctxb = context.Background()
)

func TestCheck(t *testing.T) {
	_, srv := newFake(t)
	c := client(srv)
	tests := []struct {
		path     string
		name     string
		writable bool
		err      error
	}{
		{"/dav/cal/", "Family & home", true, nil},
		{"/dav/cal", "Family & home", true, nil}, // href with slash still matches
		{"/dav/ro/", "Holidays", false, nil},
		{"/dav/noprivs/", "Work", true, nil},
		{"/dav/moved/", "Family & home", true, nil},
		{"/dav/files/", "", false, ErrNotCalendar},
		{"/dav/html/", "", false, ErrNotCalendar},
	}
	for _, tt := range tests {
		name, writable, err := c.Check(ctxb, srv.URL+tt.path)
		if !errors.Is(err, tt.err) || name != tt.name || writable != tt.writable {
			t.Errorf("%s: got %q %v %v", tt.path, name, writable, err)
		}
	}
	// Unknown collection: a 207 with a 404 response.
	if _, _, err := c.Check(ctxb, srv.URL+"/dav/missing/"); !errors.Is(err, ErrNotCalendar) {
		t.Errorf("missing: %v", err)
	}
}

func TestErrors(t *testing.T) {
	_, srv := newFake(t)
	bad := &Client{HTTP: srv.Client(), User: user, Password: "wrong-topsecret"}
	_, _, err := bad.Check(ctxb, srv.URL+"/dav/cal/")
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("wrong password: %v", err)
	}
	if strings.Contains(err.Error(), "topsecret") {
		t.Errorf("password in error: %v", err)
	}
	if _, err := (&Client{HTTP: srv.Client()}).Events(ctxb, srv.URL+"/dav/cal/", jan, feb); !errors.Is(err, ErrAuth) {
		t.Errorf("no credentials: %v", err)
	}
	if _, err := bad.Put(ctxb, srv.URL+"/dav/cal/", ical.Event{UID: "x", Start: jan}, "p"); !errors.Is(err, ErrAuth) {
		t.Errorf("put: %v", err)
	}
	if err := bad.Delete(ctxb, srv.URL+"/dav/cal/", "x"); !errors.Is(err, ErrAuth) {
		t.Errorf("delete: %v", err)
	}

	c := client(srv)
	for _, u := range []string{"ftp://example.test/cal/", "/dav/cal/", "file:///etc/passwd", "http://", "::bad"} {
		if _, _, err := c.Check(ctxb, u); !errors.Is(err, ErrBadURL) {
			t.Errorf("%q: %v", u, err)
		}
	}
	// Other statuses carry the status and no password, also with user info in the URL.
	withPW := strings.Replace(srv.URL, "http://", "http://alice:urlpass@", 1)
	err = c.Delete(ctxb, withPW+"/dav/files/", "x")
	if err != nil {
		t.Errorf("delete of missing resource: %v", err)
	}
	_, err = c.Put(ctxb, withPW+"/dav/nope/", ical.Event{UID: "x", Start: jan}, "p")
	var se *StatusError
	if !errors.As(err, &se) || se.Code != http.StatusConflict || se.Method != http.MethodPut {
		t.Fatalf("put bad data: %v", err)
	}
	if strings.Contains(err.Error(), "urlpass") || strings.Contains(err.Error(), password) {
		t.Errorf("password in error: %v", err)
	}
	if !strings.Contains(err.Error(), "409") {
		t.Errorf("status missing in error: %v", err)
	}
	// Transport errors keep the cause and hide the password.
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := strings.Replace(dead.URL, "http://", "http://alice:urlpass@", 1)
	dead.Close()
	_, _, err = c.Check(ctxb, deadURL+"/dav/cal/")
	if err == nil || strings.Contains(err.Error(), "urlpass") || strings.Contains(err.Error(), password) {
		t.Errorf("dial error: %v", err)
	}
	// No HTTP client.
	if _, _, err := (&Client{}).Check(ctxb, srv.URL+"/dav/cal/"); err == nil {
		t.Error("nil client accepted")
	}
}

func TestEvents(t *testing.T) {
	f, srv := newFake(t)
	c := client(srv)
	want := []string{
		"2026-01-05T09:00:00Z weekly-1 Weekly & more 20260105T090000Z",
		"2026-01-13T09:00:00Z weekly-1 Weekly (moved) 20260112T090000Z",
		"2026-01-19T09:00:00Z weekly-1 Weekly & more 20260119T090000Z",
		"2026-01-20T00:00:00Z single-1 Dentist ",
		"2026-01-26T09:00:00Z weekly-1 Weekly & more 20260126T090000Z",
	}
	for _, coll := range []string{"/dav/cal/", "/dav/noexpand/", "/dav/reject/"} {
		if coll != "/dav/cal/" {
			want = slices.DeleteFunc(slices.Clone(want), func(s string) bool { return strings.Contains(s, "single-1") })
		}
		evs, err := c.Events(ctxb, srv.URL+coll, jan, feb)
		if err != nil {
			t.Fatalf("%s: %v", coll, err)
		}
		var got []string
		for _, e := range evs {
			got = append(got, fmt.Sprintf("%s %s %s %s", e.Start.Format(time.RFC3339), e.UID, e.Summary, e.Recurrence))
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s:\n got %q\nwant %q", coll, got, want)
		}
		f.mu.Lock()
		body := string(f.lastB)
		f.mu.Unlock()
		if !strings.Contains(body, `time-range start="20260101T000000Z" end="20260201T000000Z"`) {
			t.Errorf("%s: query without time range: %s", coll, body)
		}
		if wantExpand := coll == "/dav/cal/"; strings.Contains(body, "<c:expand") != wantExpand && coll != "/dav/noexpand/" {
			t.Errorf("%s: expand=%v in last query", coll, !wantExpand)
		}
	}
	// Window clipping is applied even when the server returns more.
	evs, err := c.Events(ctxb, srv.URL+"/dav/noexpand/", jan, feb)
	if err != nil || len(evs) != 4 {
		t.Fatalf("noexpand: %d %v", len(evs), err)
	}
	// Too large.
	if _, err := c.Events(ctxb, srv.URL+"/dav/big/", jan, feb); !errors.Is(err, ErrTooLarge) {
		t.Errorf("big: %v", err)
	}
	// Empty calendar.
	evs, err = c.Events(ctxb, srv.URL+"/dav/noetag/", jan, feb)
	if err != nil || len(evs) != 0 {
		t.Errorf("empty: %v %v", evs, err)
	}
}

func TestPutDelete(t *testing.T) {
	f, srv := newFake(t)
	c := client(srv)
	ev := ical.Event{UID: "phx/task:42@home ä", Summary: "Change filter", AllDay: true,
		Start: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)}
	etag, err := c.Put(ctxb, srv.URL+"/dav/cal", ev, "-//Phoedox//EN")
	if err != nil || etag == "" {
		t.Fatalf("put: %q %v", etag, err)
	}
	const path = "/dav/cal/phx-task-42-home--.ics"
	f.mu.Lock()
	res, ok := f.res[path]
	ct := f.last.Header.Get("Content-Type")
	f.mu.Unlock()
	if !ok || res.etag != etag {
		t.Fatalf("stored under %v, etag %q", f.res, etag)
	}
	if ct != "text/calendar; charset=utf-8" {
		t.Errorf("content type %q", ct)
	}
	got, err := ical.Parse(res.data, time.Time{}, time.Time{})
	if err != nil || len(got) != 1 || got[0].UID != ev.UID || got[0].Summary != ev.Summary || !got[0].AllDay || !got[0].Start.Equal(ev.Start) {
		t.Errorf("stored event: %+v %v", got, err)
	}

	// Update: new ETag.
	ev.Summary = "Change filter (again)"
	etag2, err := c.Put(ctxb, srv.URL+"/dav/cal/", ev, "-//Phoedox//EN")
	if err != nil || etag2 == etag || etag2 == "" {
		t.Errorf("update: %q %v", etag2, err)
	}
	// Read back via Events.
	evs, err := c.Events(ctxb, srv.URL+"/dav/cal/", time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC))
	if err != nil || len(evs) != 1 || evs[0].UID != ev.UID || evs[0].Summary != ev.Summary {
		t.Errorf("read back: %+v %v", evs, err)
	}

	// ETag from PROPFIND when the PUT response has none.
	etag3, err := c.Put(ctxb, srv.URL+"/dav/noetag/", ev, "p")
	f.mu.Lock()
	want := f.res["/dav/noetag/phx-task-42-home--.ics"].etag
	f.mu.Unlock()
	if err != nil || etag3 != want || want == "" {
		t.Errorf("noetag: %q want %q %v", etag3, want, err)
	}

	// Delete, then delete again (404 counts as done).
	if err := c.Delete(ctxb, srv.URL+"/dav/cal/", ev.UID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	f.mu.Lock()
	_, still := f.res[path]
	f.mu.Unlock()
	if still {
		t.Error("not deleted")
	}
	if err := c.Delete(ctxb, srv.URL+"/dav/cal/", ev.UID); err != nil {
		t.Errorf("second delete: %v", err)
	}
	// PUT and DELETE follow a 301 with their own method.
	if _, err := c.Put(ctxb, srv.URL+"/dav/moved/", ev, "p"); err != nil {
		t.Errorf("put via redirect: %v", err)
	}
	if err := c.Delete(ctxb, srv.URL+"/dav/moved/", ev.UID); err != nil {
		t.Errorf("delete via redirect: %v", err)
	}
	f.mu.Lock()
	_, still = f.res[path]
	f.mu.Unlock()
	if still {
		t.Error("redirected delete did not reach the resource")
	}
	if _, err := c.Put(ctxb, srv.URL+"/dav/cal/", ical.Event{}, "p"); err == nil {
		t.Error("event without UID accepted")
	}
}

func TestSafeName(t *testing.T) {
	tests := map[string]string{
		"abc-DEF_1.2":            "abc-DEF_1.2",
		"a/b\\c d":               "a-b-c-d",
		"../../etc":              "..-..-etc",
		"grüße@example.org":      "gr--e-example.org",
		strings.Repeat("x", 200): strings.Repeat("x", 120),
		strings.Repeat("ü", 200): strings.Repeat("-", 120),
	}
	for in, want := range tests {
		if got := SafeName(in); got != want {
			t.Errorf("SafeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFetchICS(t *testing.T) {
	var gotUA, gotAuth string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		u, p, _ := r.BasicAuth()
		gotAuth = u + ":" + p
		switch r.URL.Path {
		case "/feed.ics":
			w.Header().Set("Content-Type", "text/calendar")
			fmt.Fprint(w, masterICS)
		case "/huge.ics":
			fmt.Fprint(w, "BEGIN:VCALENDAR\r\n")
			line := []byte("X-PAD:" + strings.Repeat("x", 1000) + "\r\n")
			for range MaxResponseSize/len(line) + 1 {
				_, _ = w.Write(line)
			}
		case "/page.html":
			fmt.Fprint(w, "<html></html>")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "https://")
	hc := srv.Client()

	for _, u := range []string{"webcal://" + host + "/feed.ics", "WEBCAL://" + host + "/feed.ics", "webcals://" + host + "/feed.ics", srv.URL + "/feed.ics"} {
		evs, err := FetchICS(ctxb, hc, u, jan, feb)
		if err != nil {
			t.Fatalf("%s: %v", u, err)
		}
		if len(evs) != 4 || evs[1].Summary != "Weekly (moved)" {
			t.Errorf("%s: %+v", u, evs)
		}
	}
	if gotUA != "Phoedox-CalDAV" {
		t.Errorf("user agent %q", gotUA)
	}
	// Window clipping.
	evs, err := FetchICS(ctxb, hc, srv.URL+"/feed.ics", time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC))
	if err != nil || len(evs) != 1 {
		t.Errorf("clipped: %+v %v", evs, err)
	}
	// Credentials in the URL become basic auth and stay out of errors.
	_, err = FetchICS(ctxb, hc, "webcal://bob:feedpass@"+host+"/missing.ics", jan, feb)
	var se *StatusError
	if !errors.As(err, &se) || se.Code != http.StatusNotFound {
		t.Errorf("missing: %v", err)
	}
	if gotAuth != "bob:feedpass" {
		t.Errorf("auth %q", gotAuth)
	}
	if err != nil && strings.Contains(err.Error(), "feedpass") {
		t.Errorf("password in error: %v", err)
	}
	if _, err := FetchICS(ctxb, hc, srv.URL+"/huge.ics", jan, feb); !errors.Is(err, ErrTooLarge) {
		t.Errorf("huge: %v", err)
	}
	if _, err := FetchICS(ctxb, hc, srv.URL+"/page.html", jan, feb); !errors.Is(err, ical.ErrNotICalendar) {
		t.Errorf("html: %v", err)
	}
	for _, u := range []string{"ftp://" + host + "/feed.ics", "feed.ics", "javascript:alert(1)"} {
		if _, err := FetchICS(ctxb, hc, u, jan, feb); !errors.Is(err, ErrBadURL) {
			t.Errorf("%q: %v", u, err)
		}
	}
	// Timeouts come from the given client.
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer slow.Close()
	quick := &http.Client{Timeout: 100 * time.Millisecond}
	if _, err := FetchICS(ctxb, quick, slow.URL+"/x.ics", jan, feb); err == nil {
		t.Error("no timeout")
	}
}
