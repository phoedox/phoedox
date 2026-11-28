// Package plugintest builds the test plugin (internal/plugins/testdata) for
// tests in other packages.
package plugintest

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/phoedox/phoedox/internal/plugins"
)

var (
	once sync.Once
	wasm []byte
	err  error
)

// Wasm returns the compiled test plugin (built once per test binary).
func Wasm(t testing.TB) []byte {
	t.Helper()
	once.Do(func() {
		_, file, _, _ := runtime.Caller(0)
		src := filepath.Join(filepath.Dir(file), "..", "testdata", "testplugin")
		dir, e := os.MkdirTemp("", "phx-plugin-")
		if e != nil {
			err = e
			return
		}
		out := filepath.Join(dir, "plugin.wasm")
		cmd := exec.Command("go", "build", "-buildmode=c-shared", "-o", out, ".")
		cmd.Dir = src
		cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
		if b, e := cmd.CombinedOutput(); e != nil {
			err = errors.New(string(b))
			return
		}
		wasm, err = os.ReadFile(out)
	})
	if err != nil {
		t.Fatalf("build test plugin: %v", err)
	}
	return wasm
}

// Manifest is the manifest of the test package ("de.example.ups").
const Manifest = `{
  "id": "de.example.ups", "name": "USV", "version": "1.2.0", "hostApi": "1",
  "icon": "battery-charging", "category": "servers", "logo": "logo.svg",
  "description": "Test plugin",
  "engines": { "phoedox": ">=0.1.0 <2.0.0" },
  "backend": { "wasm": "backend.wasm", "memoryMb": 32 },
  "frontend": { "module": "frontend/index.js" },
  "contributes": {
    "kinds": [{ "id": "ups", "schema": "schemas/ups.json", "icon": "battery-charging" }],
    "relations": [{ "id": "powers", "from": ["ups"], "to": ["*"] }],
    "blocks": [{ "id": "timeline", "label": "Timeline", "icon": "chart-gantt", "element": "phx-timeline", "board": true, "live": true, "size": { "w": 500, "h": 240 }, "example": "2026-01-01" }, { "id": "chart", "label": "Not mine" }],
    "mcpTools": [{ "name": "ups.runtime", "description": "Runtime of a UPS.", "input": "schemas/runtime.json" },
      { "name": "ups.switch", "description": "Switch the UPS outlet.", "control": true, "timeoutSec": 20, "target": { "arg": "ups" } }],
    "events": ["schedule:every=60s", "entity.*"],
    "settings": "schemas/settings.json"
  },
  "permissions": ["graph:read", "graph:write:ups", "secrets:nut_password", "net:fetch:127.0.0.1"]
}`

// Package builds a package with the test plugin and the given manifest.
func Package(t testing.TB, manifest string) []byte {
	t.Helper()
	data, e := plugins.BuildPackage(map[string][]byte{
		plugins.ManifestFile:   []byte(manifest),
		"backend.wasm":         Wasm(t),
		"frontend/index.js":    []byte("export default {}"),
		"schemas/ups.json":     []byte(`{"title":"USV","x-i18n":{"en":{"title":"UPS"}},"type":"object","required":["kapazitaet"],"properties":{"kapazitaet":{"type":"number","title":"Kapazität","x-unit":"VA","x-i18n":{"en":{"title":"Capacity"}}},"ladung":{"type":"number","x-unit":"%"},"modell":{"type":"string"}}}`),
		"schemas/runtime.json": []byte(`{"type":"object","properties":{"ups":{"type":"string"}}}`),
		"schemas/settings.json": []byte(`{"type":"object","properties":{"interval":{"type":"integer","title":"Interval","minimum":10,"maximum":600,"default":60},` +
			`"mode":{"type":"string","enum":["quiet","loud"],"default":"quiet","x-i18n":{"de":{"title":"Modus","enum":{"quiet":"leise"}}}},"notify":{"type":"boolean"},"list":{"type":"array"}}}`),
		"logo.svg": []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 8 8"><circle cx="4" cy="4" r="3"/></svg>`),
	})
	if e != nil {
		t.Fatal(e)
	}
	return data
}
