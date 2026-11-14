package plugins

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	extism "github.com/extism/go-sdk"
	"github.com/tetratelabs/wazero"
)

// Env is what a plugin may reach during one call. Core implements it for the
// calling user and project, so a plugin never sees more than that user; the
// runtime checks the manifest permissions before calling it.
type Env interface {
	GraphGet(ctx context.Context, ref string) (any, error)
	GraphQuery(ctx context.Context, q Query) (any, error)
	// GraphApply applies ops atomically; allow tells which kinds the plugin
	// may change (core checks it for linked entities too).
	GraphApply(ctx context.Context, ops Ops, allow func(kind string) bool) (any, error)
	Secret(ctx context.Context, name string) (string, error)
	KVGet(ctx context.Context, key string) (json.RawMessage, error)
	KVSet(ctx context.Context, key string, value json.RawMessage) error
	// GraphRecord stores measured values of entries; allow tells on which
	// kinds the plugin may record (graph:write).
	GraphRecord(ctx context.Context, readings json.RawMessage, allow func(kind string) bool) (any, error)
	// ConfiguredHosts are the hosts of the addresses the project owner set
	// for the plugin (secrets "url" or "*_url"); net:fetch:config allows them.
	ConfiguredHosts(ctx context.Context) ([]string, error)
}

// Query searches entities of the project.
type Query struct {
	Kind  string `json:"kind,omitempty"`
	Q     string `json:"q,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

// Ops are graph changes (phoedox-sdk: GraphOps).
type Ops struct {
	Ops []Op `json:"ops"`
}

// Op is one change. Ref names an upserted entity for later ops in the batch.
type Op struct {
	Op    string         `json:"op"` // upsert | link | unlink | delete
	Ref   string         `json:"ref,omitempty"`
	Kind  string         `json:"kind,omitempty"`
	ID    string         `json:"id,omitempty"` // slug of the entity
	Name  string         `json:"name,omitempty"`
	From  string         `json:"from,omitempty"`
	Rel   string         `json:"rel,omitempty"`
	To    string         `json:"to,omitempty"`
	Attrs map[string]any `json:"attrs,omitempty"`
}

// FetchRequest is an HTTP call of a plugin.
type FetchRequest struct {
	Method  string            `json:"method,omitempty"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

// FetchResponse is the answer to a FetchRequest.
type FetchResponse struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body"`
}

// Error codes returned to plugins.
var (
	ErrDenied   = errors.New("permission not granted in the manifest")
	ErrNotFound = errors.New("not found")
	ErrInvalid  = errors.New("invalid input")
)

// Runtime runs plugin backends. Modules are compiled once per package and
// instantiated for every call, so calls share no memory.
type Runtime struct {
	Log *slog.Logger
	// Timeout limits one call (default 10 s).
	Timeout time.Duration
	// HTTP is used for net:fetch; redirects are never followed.
	HTTP *http.Client
	// MaxResponse limits fetched bodies (default 4 MB).
	MaxResponse int64
	// Code maps errors of Env to a code for the plugin ("forbidden", …).
	Code func(error) string

	mu       sync.Mutex
	compiled map[string]*extism.CompiledPlugin
	cache    wazero.CompilationCache
	slots    chan struct{}
	logs     map[string]*ring
}

// NewRuntime returns a runtime that runs at most parallel calls at once.
func NewRuntime(log *slog.Logger, parallel int) *Runtime {
	if parallel <= 0 {
		parallel = 8
	}
	extism.SetLogLevel(extism.LogLevelInfo)
	return &Runtime{
		Log:      log,
		Timeout:  10 * time.Second,
		HTTP:     &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		compiled: map[string]*extism.CompiledPlugin{},
		cache:    wazero.NewCompilationCache(),
		slots:    make(chan struct{}, parallel),
		logs:     map[string]*ring{},
	}
}

// Close frees all compiled modules.
func (r *Runtime) Close(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, c := range r.compiled {
		_ = c.Close(ctx)
		delete(r.compiled, k)
	}
	_ = r.cache.Close(ctx)
}

// Forget drops the compiled module of a package (after uninstalling).
func (r *Runtime) Forget(ctx context.Context, sha string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.compiled[sha]; ok {
		_ = c.Close(ctx)
		delete(r.compiled, sha)
	}
}

type callKey struct{}

type callState struct {
	pkg *Package
	env Env
	ctx context.Context
}

func (r *Runtime) compile(ctx context.Context, p *Package) (*extism.CompiledPlugin, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.compiled[p.SHA256]; ok {
		return c, nil
	}
	mb := 64
	if p.Manifest.Backend.MemoryMB > 0 {
		mb = p.Manifest.Backend.MemoryMB
	}
	host := extism.NewHostFunctionWithStack("phx_host", r.host, []extism.ValueType{extism.ValueTypePTR}, []extism.ValueType{extism.ValueTypePTR})
	host.SetNamespace("extism:host/user")
	c, err := extism.NewCompiledPlugin(ctx,
		extism.Manifest{Wasm: []extism.Wasm{extism.WasmData{Data: p.Wasm(), Name: "main"}}, Memory: &extism.ManifestMemory{MaxPages: uint32(mb * 16)}},
		extism.PluginConfig{EnableWasi: true, RuntimeConfig: wazero.NewRuntimeConfig().WithCompilationCache(r.cache).WithCloseOnContextDone(true)},
		[]extism.HostFunction{host})
	if err != nil {
		return nil, fmt.Errorf("plugin %s: %w", p.Manifest.ID, err)
	}
	r.compiled[p.SHA256] = c
	return c, nil
}

// Has tells whether the backend exports a function.
func (r *Runtime) Has(ctx context.Context, p *Package, fn string) bool {
	if p.Manifest.Backend == nil {
		return false
	}
	c, err := r.compile(ctx, p)
	if err != nil {
		return false
	}
	inst, err := c.Instance(ctx, extism.PluginInstanceConfig{})
	if err != nil {
		return false
	}
	defer inst.Close(ctx)
	return inst.FunctionExists(fn)
}

// Call runs an exported function with JSON input and returns its output.
// logKey groups the plugin's log lines (tenant + plugin).
func (r *Runtime) Call(ctx context.Context, p *Package, logKey, fn string, input any, env Env) (json.RawMessage, error) {
	return r.CallWithin(ctx, p, logKey, fn, input, env, 0)
}

// CallWithin is Call with an own time limit (0 = the runtime default).
func (r *Runtime) CallWithin(ctx context.Context, p *Package, logKey, fn string, input any, env Env, limit time.Duration) (json.RawMessage, error) {
	if limit <= 0 {
		limit = r.Timeout
	}
	if p.Manifest.Backend == nil {
		return nil, fmt.Errorf("plugin %s has no backend", p.Manifest.ID)
	}
	in, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	c, err := r.compile(ctx, p)
	if err != nil {
		return nil, err
	}
	select {
	case r.slots <- struct{}{}:
		defer func() { <-r.slots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	// Nanosleep lets plugins wait (e.g. power off, wait, power on) within their limit.
	cfg := wazero.NewModuleConfig().WithSysWalltime().WithSysNanotime().WithSysNanosleep().WithRandSource(rand.Reader)
	inst, err := c.Instance(ctx, extism.PluginInstanceConfig{ModuleConfig: cfg})
	if err != nil {
		return nil, fmt.Errorf("plugin %s: %w", p.Manifest.ID, err)
	}
	defer inst.Close(context.Background())
	inst.SetLogger(func(l extism.LogLevel, msg string) { r.logLine(logKey, p.Manifest.ID, l, msg) })
	if !inst.FunctionExists(fn) {
		return nil, fmt.Errorf("plugin %s does not export %s", p.Manifest.ID, fn)
	}
	ctx = context.WithValue(ctx, callKey{}, &callState{pkg: p, env: env, ctx: ctx})
	started := time.Now()
	rc, out, err := inst.CallWithContext(ctx, fn, in)
	if ctx.Err() != nil {
		err = fmt.Errorf("plugin %s: %s took longer than %s", p.Manifest.ID, fn, limit)
	} else if err == nil && rc != 0 {
		err = fmt.Errorf("plugin %s: %s returned %d", p.Manifest.ID, fn, rc)
	} else if err != nil {
		err = fmt.Errorf("plugin %s: %s: %w", p.Manifest.ID, fn, err)
	}
	if err != nil {
		r.logLine(logKey, p.Manifest.ID, extism.LogLevelError, err.Error())
		return nil, err
	}
	r.Log.Debug("plugin call", "plugin", p.Manifest.ID, "fn", fn, "took", time.Since(started))
	if len(out) == 0 {
		return json.RawMessage("null"), nil
	}
	if !json.Valid(out) {
		return nil, fmt.Errorf("plugin %s: %s returned no JSON", p.Manifest.ID, fn)
	}
	return out, nil
}

type hostRequest struct {
	Op   string          `json:"op"`
	Args json.RawMessage `json:"args"`
}

type hostResponse struct {
	OK     bool   `json:"ok"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
	Code   string `json:"code,omitempty"`
}

// host is the single host function phx_host(request) -> response. One
// dispatching function keeps the ABI stable when operations are added.
func (r *Runtime) host(ctx context.Context, cp *extism.CurrentPlugin, stack []uint64) {
	resp := hostResponse{OK: true}
	st, _ := ctx.Value(callKey{}).(*callState)
	raw, err := cp.ReadBytes(stack[0])
	if err == nil && st == nil {
		err = errors.New("no call state")
	}
	var req hostRequest
	if err == nil {
		err = json.Unmarshal(raw, &req)
	}
	if err == nil {
		resp.Result, err = r.dispatch(st, req)
	}
	if err != nil {
		resp = hostResponse{Error: err.Error(), Code: r.code(err)}
	}
	out, _ := json.Marshal(resp)
	off, werr := cp.WriteBytes(out)
	if werr != nil {
		panic(werr) // aborts the call; the plugin ran out of memory
	}
	stack[0] = off
}

func (r *Runtime) code(err error) string {
	switch {
	case errors.Is(err, ErrDenied):
		return "denied"
	case errors.Is(err, ErrNotFound):
		return "not_found"
	case errors.Is(err, ErrInvalid):
		return "invalid"
	}
	if r.Code != nil {
		if c := r.Code(err); c != "" {
			return c
		}
	}
	return "failed"
}

func decode[T any](raw json.RawMessage) (T, error) {
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		return v, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return v, nil
}

func (r *Runtime) dispatch(st *callState, req hostRequest) (any, error) {
	m, ctx := st.pkg.Manifest, st.ctx
	switch req.Op {
	case "graph.get":
		if !m.CanRead("graph") {
			return nil, fmt.Errorf("%w: graph:read", ErrDenied)
		}
		a, err := decode[struct{ Ref string }](req.Args)
		if err != nil {
			return nil, err
		}
		return st.env.GraphGet(ctx, a.Ref)
	case "graph.query":
		if !m.CanRead("graph") {
			return nil, fmt.Errorf("%w: graph:read", ErrDenied)
		}
		q, err := decode[Query](req.Args)
		if err != nil {
			return nil, err
		}
		return st.env.GraphQuery(ctx, q)
	case "graph.apply":
		ops, err := decode[Ops](req.Args)
		if err != nil {
			return nil, err
		}
		if len(ops.Ops) > 500 {
			return nil, fmt.Errorf("%w: at most 500 ops per call", ErrInvalid)
		}
		for _, o := range ops.Ops {
			if (o.Op == "upsert" || o.Op == "delete") && o.Kind != "" && !m.CanWriteKind(o.Kind) {
				return nil, fmt.Errorf("%w: graph:write:%s", ErrDenied, o.Kind)
			}
		}
		return st.env.GraphApply(ctx, ops, m.CanWriteKind)
	case "graph.record":
		a, err := decode[struct {
			Readings json.RawMessage `json:"readings"`
		}](req.Args)
		if err != nil {
			return nil, err
		}
		return st.env.GraphRecord(ctx, a.Readings, m.CanWriteKind)
	case "secret.get":
		a, err := decode[struct{ Name string }](req.Args)
		if err != nil {
			return nil, err
		}
		if !m.CanSecret(a.Name) {
			return nil, fmt.Errorf("%w: secrets:%s", ErrDenied, a.Name)
		}
		return st.env.Secret(ctx, a.Name)
	case "kv.get", "kv.set":
		a, err := decode[struct {
			Key   string
			Value json.RawMessage
		}](req.Args)
		if err != nil {
			return nil, err
		}
		if a.Key == "" || len(a.Key) > 200 {
			return nil, fmt.Errorf("%w: key must have 1 to 200 characters", ErrInvalid)
		}
		if req.Op == "kv.get" {
			return st.env.KVGet(ctx, a.Key)
		}
		if len(a.Value) > 256<<10 {
			return nil, fmt.Errorf("%w: value larger than 256 KB", ErrInvalid)
		}
		return nil, st.env.KVSet(ctx, a.Key, a.Value)
	case "http.fetch":
		f, err := decode[FetchRequest](req.Args)
		if err != nil {
			return nil, err
		}
		return r.fetch(ctx, m, f, st.env)
	}
	return nil, fmt.Errorf("%w: unknown operation %q", ErrInvalid, req.Op)
}

func (r *Runtime) fetch(ctx context.Context, m Manifest, f FetchRequest, env Env) (*FetchResponse, error) {
	u, err := url.Parse(f.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return nil, fmt.Errorf("%w: url must be http(s)://host/…", ErrInvalid)
	}
	if !m.CanFetch(u.Hostname()) && !configuredHost(ctx, m, env, u.Hostname()) {
		return nil, fmt.Errorf("%w: net:fetch:%s", ErrDenied, strings.ToLower(u.Hostname()))
	}
	method := strings.ToUpper(f.Method)
	if method == "" {
		method = http.MethodGet
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), strings.NewReader(f.Body))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	for k, v := range f.Headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("User-Agent", "Phoedox-Plugin/"+m.ID)
	res, err := r.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %v", u.Host, err)
	}
	defer res.Body.Close()
	limit := r.MaxResponse
	if limit <= 0 {
		limit = 4 << 20
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %v", u.Host, err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%w: response larger than %d bytes", ErrInvalid, limit)
	}
	out := &FetchResponse{Status: res.StatusCode, Body: string(body), Headers: map[string]string{}}
	for k := range res.Header {
		out.Headers[strings.ToLower(k)] = res.Header.Get(k)
	}
	return out, nil
}

// LogLine is one line a plugin logged (or an error of a call).
type LogLine struct {
	Time  time.Time `json:"time"`
	Level string    `json:"level"`
	Msg   string    `json:"msg"`
}

type ring struct {
	lines []LogLine
	next  int
	full  bool
}

const logLines = 200

func (r *Runtime) logLine(key, id string, l extism.LogLevel, msg string) {
	if len(msg) > 2000 {
		msg = msg[:2000] + "…"
	}
	level := strings.ToLower(l.String())
	r.Log.Info("plugin log", "plugin", id, "level", level, "msg", msg)
	r.mu.Lock()
	defer r.mu.Unlock()
	rg := r.logs[key]
	if rg == nil {
		rg = &ring{lines: make([]LogLine, logLines)}
		r.logs[key] = rg
	}
	rg.lines[rg.next] = LogLine{Time: time.Now().UTC(), Level: level, Msg: msg}
	rg.next = (rg.next + 1) % logLines
	if rg.next == 0 {
		rg.full = true
	}
}

// Logs returns the recent log lines of a plugin, newest first.
func (r *Runtime) Logs(key string) []LogLine {
	r.mu.Lock()
	defer r.mu.Unlock()
	rg := r.logs[key]
	out := []LogLine{}
	if rg == nil {
		return out
	}
	n := rg.next
	if rg.full {
		n = logLines
	}
	for i := 1; i <= n; i++ {
		out = append(out, rg.lines[(rg.next-i+logLines)%logLines])
	}
	return out
}

// configuredHost tells whether net:fetch:config lets the plugin call host:
// the project owner entered an address with this host for the plugin.
func configuredHost(ctx context.Context, m Manifest, env Env, host string) bool {
	if !m.Has("net:fetch:config") || env == nil {
		return false
	}
	hosts, err := env.ConfiguredHosts(ctx)
	if err != nil {
		return false
	}
	for _, h := range hosts {
		if strings.EqualFold(h, host) {
			return true
		}
	}
	return false
}
