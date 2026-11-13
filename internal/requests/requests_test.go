package requests

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const block = `GET {{base}}/status?key={{api_key}}
Authorization: Bearer {{token}}
Accept: application/json

expect status == 200
expect $.state >= -70
expect $.items.length == 2
expect $.items[1].name == "nas"
expect $.name contains "Haupt"
expect header Content-Type contains json
expect $.missing !exists
expect time < 5000
expect $.state > 0`

func TestParse(t *testing.T) {
	r, err := Parse(block)
	if err != nil || r.Method != "GET" || len(r.Headers) != 2 || r.Body != "" || len(r.Expects) != 9 {
		t.Fatalf("parse: %v %+v", err, r)
	}
	post, err := Parse("# Gerät anlegen\nPOST {{base}}/items\nContent-Type: application/json\n\n{\n  \"name\": \"x\"\n}\n\nexpect status == 201")
	if err != nil || post.Method != "POST" || post.Body != "{\n  \"name\": \"x\"\n}" || post.Expects[0] != "status == 201" {
		t.Fatalf("post: %v %+v", err, post)
	}
	for _, bad := range []string{"", "FETCH http://x", "GET http://x\nkein header"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if vars := r.Variables(); strings.Join(vars, ",") != "base,api_key,token" {
		t.Fatalf("variables: %v", vars)
	}
	filled, missing := r.Fill(map[string]string{"base": "http://x", "token": "t0k3n"})
	if filled.URL != "http://x/status?key={{api_key}}" || filled.Headers[0][1] != "Bearer t0k3n" || len(missing) != 1 || missing[0] != "api_key" {
		t.Fatalf("fill: %+v %v", filled, missing)
	}
}

func TestBlocks(t *testing.T) {
	md := "# Doku\n\n```request\nGET http://a\n```\n\nText\n\n```go\nfmt.Println()\n```\n\n````http Statusabfrage\nGET http://b\n\n```\nnicht das Ende\n```\n````\n"
	bs := Blocks(md)
	if len(bs) != 2 || strings.TrimSpace(bs[0]) != "GET http://a" || !strings.Contains(bs[1], "nicht das Ende") {
		t.Fatalf("blocks: %q", bs)
	}
}

func TestEvaluate(t *testing.T) {
	r, _ := Parse(block)
	body := []byte(`{"state": -42, "name": "Hauptrouter", "items": [{"name": "router"}, {"name": "nas"}]}`)
	checks := Evaluate(r.Expects, 200, 120, func(h string) string {
		if h == "Content-Type" {
			return "application/json"
		}
		return ""
	}, body)
	for i, c := range checks {
		want := i != 8 // "$.state > 0" fails
		if c.OK != want {
			t.Errorf("%s: ok=%v actual=%q err=%s", c.Expr, c.OK, c.Actual, c.Error)
		}
	}
	if checks[1].Actual != "-42" {
		t.Fatalf("actual: %q", checks[1].Actual)
	}
	if c := Evaluate([]string{"wurst"}, 200, 0, func(string) string { return "" }, nil); c[0].Error != "syntax" {
		t.Fatal("syntax errors must be reported")
	}
}

func TestRunPolicyAndMask(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		default:
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Echo", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"state":-50,"echo":"` + r.Header.Get("Authorization") + `"}`))
		}
	}))
	defer srv.Close()
	req := Request{Method: "GET", URL: srv.URL + "/status", Headers: [][2]string{{"Authorization", "Bearer geheim-123"}}, Expects: []string{"status == 200", "$.state >= -70"}}

	// local addresses are blocked unless private networks are allowed
	if res := Run(context.Background(), Policy{}.Client(), req); res.Error == "" || res.Status != 0 {
		t.Fatalf("loopback must be blocked: %+v", res)
	}
	res := Run(context.Background(), Policy{AllowPrivate: true}.Client(), req)
	if res.Error != "" || res.Status != 200 || !res.Checks[0].OK || !res.Checks[1].OK || !strings.Contains(res.Body, "\n  \"state\"") {
		t.Fatalf("run: %+v", res)
	}
	res.Mask([]string{"geheim-123"})
	if strings.Contains(res.Body, "geheim-123") || !strings.Contains(res.Body, "••••") {
		t.Fatalf("body not masked: %s", res.Body)
	}
	for _, h := range res.Headers {
		if strings.Contains(h[1], "geheim") {
			t.Fatalf("header not masked: %v", h)
		}
	}
	// cloud metadata stays blocked even with private networks allowed
	meta := Run(context.Background(), Policy{AllowPrivate: true}.Client(), Request{Method: "GET", URL: "http://169.254.169.254/latest/meta-data"})
	if !strings.Contains(meta.Error, "not allowed") {
		t.Fatalf("metadata: %+v", meta)
	}
	if r := Run(context.Background(), Policy{AllowPrivate: true}.Client(), Request{Method: "GET", URL: srv.URL + "/loop"}); !strings.Contains(r.Error, "redirects") {
		t.Fatalf("redirect loop: %+v", r)
	}
	if r := Run(context.Background(), Policy{}.Client(), Request{Method: "GET", URL: "file:///etc/passwd"}); r.Error == "" {
		t.Fatal("file URL accepted")
	}
	if !Safe("GET") || Safe("POST") {
		t.Fatal("Safe")
	}
}
