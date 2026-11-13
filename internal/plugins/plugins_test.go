package plugins

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	wasmOnce sync.Once
	wasmData []byte
	wasmErr  error
)

// testWasm builds testdata/testplugin once per test run.
func testWasm(t *testing.T) []byte {
	t.Helper()
	wasmOnce.Do(func() {
		dir, err := os.MkdirTemp("", "phx-plugin-")
		if err != nil {
			wasmErr = err
			return
		}
		out := filepath.Join(dir, "plugin.wasm")
		cmd := exec.Command("go", "build", "-buildmode=c-shared", "-o", out, ".")
		cmd.Dir = filepath.Join("testdata", "testplugin")
		cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
		if b, err := cmd.CombinedOutput(); err != nil {
			wasmErr = errors.New(string(b))
			return
		}
		wasmData, wasmErr = os.ReadFile(out)
	})
	if wasmErr != nil {
		t.Fatalf("build test plugin: %v", wasmErr)
	}
	return wasmData
}

const testManifest = `{
  "id": "de.example.ups", "name": "USV", "version": "1.2.0", "hostApi": "1",
  "engines": { "phoedox": ">=0.1.0 <2.0.0" },
  "backend": { "wasm": "backend.wasm", "memoryMb": 32 },
  "frontend": { "module": "frontend/index.js" },
  "contributes": {
    "kinds": [{ "id": "ups", "schema": "schemas/ups.json", "icon": "battery-charging" }],
    "mcpTools": [{ "name": "ups.runtime" }],
    "events": ["schedule:every=60s", "entity.*"]
  },
  "permissions": ["graph:read", "graph:write:ups", "secrets:nut_password", "net:fetch:127.0.0.1"]
}`

func testPackage(t *testing.T, manifest string) *Package {
	t.Helper()
	data, err := BuildPackage(map[string][]byte{
		ManifestFile:        []byte(manifest),
		"backend.wasm":      testWasm(t),
		"frontend/index.js": []byte("export default {}"),
		"schemas/ups.json":  []byte(`{"type":"object","properties":{"ladung":{"type":"number"}}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	p, err := ReadPackage(data)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

type fakeEnv struct {
	mu      sync.Mutex
	kv      map[string]json.RawMessage
	applied []Ops
}

func (f *fakeEnv) GraphGet(_ context.Context, ref string) (any, error) {
	if ref != "usv-keller" {
		return nil, ErrNotFound
	}
	return map[string]any{"slug": ref, "kind": "ups"}, nil
}
func (f *fakeEnv) GraphQuery(_ context.Context, q Query) (any, error) {
	return []map[string]any{{"kind": q.Kind}}, nil
}
func (f *fakeEnv) GraphApply(_ context.Context, ops Ops, allow func(string) bool) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applied = append(f.applied, ops)
	return map[string]any{"applied": len(ops.Ops)}, nil
}
func (f *fakeEnv) Secret(_ context.Context, name string) (string, error) { return "s3cret", nil }
func (f *fakeEnv) KVGet(_ context.Context, key string) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.kv[key], nil
}
func (f *fakeEnv) KVSet(_ context.Context, key string, v json.RawMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.kv == nil {
		f.kv = map[string]json.RawMessage{}
	}
	f.kv[key] = v
	return nil
}

func newRuntime(t *testing.T) *Runtime {
	r := NewRuntime(slog.New(slog.NewTextHandler(io.Discard, nil)), 4)
	t.Cleanup(func() { r.Close(context.Background()) })
	return r
}

func callHost(t *testing.T, r *Runtime, p *Package, env Env, op string, args any) map[string]any {
	t.Helper()
	out, err := r.Call(context.Background(), p, "t", "call", map[string]any{"op": op, "args": args}, env)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCallAndPermissions(t *testing.T) {
	p := testPackage(t, testManifest)
	r := newRuntime(t)
	env := &fakeEnv{}
	ctx := context.Background()

	out, err := r.Call(ctx, p, "t", "hello", map[string]string{"name": "Jan"}, env)
	if err != nil || !strings.Contains(string(out), "Hello Jan") {
		t.Fatalf("hello: %s %v", out, err)
	}
	if logs := r.Logs("t"); len(logs) == 0 || logs[0].Msg != "hello Jan" {
		t.Fatalf("logs: %+v", logs)
	}

	if m := callHost(t, r, p, env, "graph.get", map[string]any{"ref": "usv-keller"}); m["ok"] != true {
		t.Fatalf("graph.get: %v", m)
	}
	if m := callHost(t, r, p, env, "graph.get", map[string]any{"ref": "nope"}); m["code"] != "not_found" {
		t.Fatalf("graph.get missing: %v", m)
	}
	ok := callHost(t, r, p, env, "graph.apply", map[string]any{"ops": []any{map[string]any{"op": "upsert", "kind": "ups", "id": "a"}}})
	if ok["ok"] != true || len(env.applied) != 1 {
		t.Fatalf("own kind: %v", ok)
	}
	denied := callHost(t, r, p, env, "graph.apply", map[string]any{"ops": []any{map[string]any{"op": "upsert", "kind": "server", "id": "a"}}})
	if denied["code"] != "denied" || len(env.applied) != 1 {
		t.Fatalf("foreign kind must be denied: %v", denied)
	}
	if m := callHost(t, r, p, env, "secret.get", map[string]any{"name": "nut_password"}); m["result"] != "s3cret" {
		t.Fatalf("secret: %v", m)
	}
	if m := callHost(t, r, p, env, "secret.get", map[string]any{"name": "other"}); m["code"] != "denied" {
		t.Fatalf("undeclared secret: %v", m)
	}
	if m := callHost(t, r, p, env, "nope", nil); m["code"] != "invalid" {
		t.Fatalf("unknown op: %v", m)
	}

	// Events: the plugin keeps a counter in its key-value store.
	for i := 1; i <= 2; i++ {
		out, err := r.Call(ctx, p, "t", "on_event", map[string]any{"type": "schedule"}, env)
		if err != nil || !strings.Contains(string(out), `"count":`+string(rune('0'+i))) {
			t.Fatalf("on_event %d: %s %v", i, out, err)
		}
	}

	if _, err := r.Call(ctx, p, "t", "tool", map[string]any{"name": "fail"}, env); err == nil || !strings.Contains(err.Error(), "on purpose") {
		t.Fatalf("plugin error must surface: %v", err)
	}
	if _, err := r.Call(ctx, p, "t", "missing", nil, env); err == nil {
		t.Fatal("missing export must fail")
	}
}

func TestFetch(t *testing.T) {
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "http://example.com/", http.StatusFound)
			return
		}
		w.Header().Set("X-Test", "1")
		_, _ = w.Write([]byte(`{"charge":87}`))
	}))
	defer srv.Close()
	p := testPackage(t, testManifest)
	r := newRuntime(t)
	m := callHost(t, r, p, &fakeEnv{}, "http.fetch", map[string]any{"url": srv.URL + "/ups", "headers": map[string]string{"Accept": "application/json"}})
	res, _ := m["result"].(map[string]any)
	if m["ok"] != true || res["body"] != `{"charge":87}` || got.Header.Get("Accept") != "application/json" {
		t.Fatalf("fetch: %v", m)
	}
	m = callHost(t, r, p, &fakeEnv{}, "http.fetch", map[string]any{"url": srv.URL + "/redirect"})
	if res, _ := m["result"].(map[string]any); res["status"] != float64(302) {
		t.Fatalf("redirects must not be followed: %v", m)
	}
	u, _ := url.Parse(srv.URL)
	other := strings.Replace(srv.URL, u.Hostname(), "localhost", 1)
	if m := callHost(t, r, p, &fakeEnv{}, "http.fetch", map[string]any{"url": other}); m["code"] != "denied" {
		t.Fatalf("undeclared host must be denied: %v", m)
	}
}

func TestLimits(t *testing.T) {
	p := testPackage(t, testManifest)
	r := newRuntime(t)
	r.Timeout = 500 * time.Millisecond
	start := time.Now()
	if _, err := r.Call(context.Background(), p, "t", "spin", nil, &fakeEnv{}); err == nil || !strings.Contains(err.Error(), "longer than") {
		t.Fatalf("endless loop must time out: %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("timeout took too long")
	}
	r.Timeout = 10 * time.Second
	if _, err := r.Call(context.Background(), p, "t", "hog", nil, &fakeEnv{}); err == nil {
		t.Fatal("memory hog must fail at the 32 MB limit")
	}
	// The runtime still works after a failed call.
	if _, err := r.Call(context.Background(), p, "t", "hello", map[string]string{"name": "x"}, &fakeEnv{}); err != nil {
		t.Fatal(err)
	}
}

func TestPackageChecks(t *testing.T) {
	wasm := testWasm(t)
	cases := map[string]map[string][]byte{
		"no manifest":      {"backend.wasm": wasm},
		"missing wasm":     {ManifestFile: []byte(testManifest)},
		"not wasm":         {ManifestFile: []byte(testManifest), "backend.wasm": []byte("#!/bin/sh"), "frontend/index.js": nil, "schemas/ups.json": []byte("{}")},
		"missing frontend": {ManifestFile: []byte(testManifest), "backend.wasm": wasm, "schemas/ups.json": []byte("{}")},
		"bad schema":       {ManifestFile: []byte(testManifest), "backend.wasm": wasm, "frontend/index.js": nil, "schemas/ups.json": []byte("{")},
		"traversal":        {ManifestFile: []byte(testManifest), "backend.wasm": wasm, "frontend/index.js": nil, "schemas/ups.json": []byte("{}"), "../evil": nil},
	}
	for name, files := range cases {
		data, err := BuildPackage(files)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ReadPackage(data); !errors.Is(err, ErrPackage) {
			t.Errorf("%s: want ErrPackage, got %v", name, err)
		}
	}
	if _, err := ReadPackage([]byte("not gzip")); !errors.Is(err, ErrPackage) {
		t.Error("garbage must fail")
	}
}

func TestSignature(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	keys := map[string]ed25519.PublicKey{"store-2026": pub}
	sig := Sign(priv, "store-2026", "de.example.ups", "1.2.0", "abc")
	if err := Verify(keys, "de.example.ups", "1.2.0", "abc", sig); err != nil {
		t.Fatal(err)
	}
	for _, c := range [][3]string{{"de.example.other", "1.2.0", "abc"}, {"de.example.ups", "1.2.1", "abc"}, {"de.example.ups", "1.2.0", "abd"}} {
		if err := Verify(keys, c[0], c[1], c[2], sig); !errors.Is(err, ErrSignature) {
			t.Errorf("%v must fail", c)
		}
	}
	if err := Verify(keys, "de.example.ups", "1.2.0", "abc", Signature{KeyID: "other", Sig: sig.Sig}); !errors.Is(err, ErrSignature) {
		t.Error("unknown key must fail")
	}
	// Test vector shared with phoedox-store (internal/sign).
	if string(SignedMessage("de.example.ups", "1.2.0", "00ff")) != "phoedox-plugin-v1\nde.example.ups\n1.2.0\n00ff" {
		t.Error("signed message changed: the store signs the same bytes")
	}
	parsed, err := ParseKeys(" store-2026:" + b64(pub) + " ")
	if err != nil || !parsed["store-2026"].Equal(pub) {
		t.Fatalf("parse keys: %v", err)
	}
	if _, err := ParseKeys("x:abc"); err == nil {
		t.Error("short key must fail")
	}
}

func TestManifest(t *testing.T) {
	m, err := ParseManifest([]byte(testManifest))
	if err != nil {
		t.Fatal(err)
	}
	if !m.CanWriteKind("ups") || m.CanWriteKind("server") || !m.CanRead("graph") || m.CanRead("docs") {
		t.Error("permissions")
	}
	if m.Schedule() != time.Minute || !m.Subscribes("entity.updated") || m.Subscribes("document.updated") {
		t.Error("events")
	}
	for v, want := range map[string]bool{"1.4.0": true, "0.1.0": true, "2.0.0": false, "0.0.9": false, "dev": true, "1.9.9-rc.1": true} {
		if err := m.Supported(v); (err == nil) != want {
			t.Errorf("version %s: %v", v, err)
		}
	}
	bad := []string{
		strings.Replace(testManifest, `"graph:read"`, `"root:all"`, 1),
		strings.Replace(testManifest, `every=60s`, `every=1s`, 1),
		strings.Replace(testManifest, `"id": "ups"`, `"id": "UPS"`, 1),
		strings.Replace(testManifest, `>=0.1.0 <2.0.0`, `latest`, 1),
	}
	for _, b := range bad {
		if _, err := ParseManifest([]byte(b)); err == nil {
			t.Errorf("must fail: %s", b[:0])
		}
	}
	if CompareVersions("1.10.0", "1.9.0") != 1 || CompareVersions("1.0.0-rc.1", "1.0.0") != -1 {
		t.Error("compare")
	}
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
