//go:build wasip1

// Test plugin for the runtime tests. Built by plugins_test.go with
// GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared.
package main

import (
	"encoding/json"

	"github.com/extism/go-pdk"
)

func main() {}

//go:wasmimport extism:host/user phx_host
func phxHost(uint64) uint64

func host(op string, args any) map[string]any {
	b, _ := json.Marshal(map[string]any{"op": op, "args": args})
	mem := pdk.AllocateBytes(b)
	defer mem.Free()
	out := pdk.FindMemory(phxHost(mem.Offset()))
	var resp map[string]any
	_ = json.Unmarshal(out.ReadBytes(), &resp)
	return resp
}

func output(v any) int32 {
	b, _ := json.Marshal(v)
	pdk.Output(b)
	return 0
}

//go:wasmexport hello
func hello() int32 {
	var in struct{ Name string }
	_ = json.Unmarshal(pdk.Input(), &in)
	pdk.Log(pdk.LogInfo, "hello "+in.Name)
	return output(map[string]string{"greeting": "Hello " + in.Name})
}

// call forwards {"op":…, "args":…} to the host and returns the response.
//
//go:wasmexport call
func call() int32 {
	var in struct {
		Op   string
		Args any
	}
	_ = json.Unmarshal(pdk.Input(), &in)
	return output(host(in.Op, in.Args))
}

//go:wasmexport on_event
func onEvent() int32 {
	var ev map[string]any
	_ = json.Unmarshal(pdk.Input(), &ev)
	resp := host("kv.get", map[string]any{"key": "count"})
	n, _ := resp["result"].(float64)
	host("kv.set", map[string]any{"key": "count", "value": n + 1})
	res := host("graph.apply", map[string]any{"ops": []any{map[string]any{"op": "upsert", "kind": "ups", "id": "usv-keller", "name": "USV Keller", "attrs": map[string]any{"kapazitaet": 1500, "ladung": 80 + n}}}})
	if res["ok"] != true {
		msg, _ := res["error"].(string)
		pdk.SetErrorString("graph.apply: " + msg)
		return 1
	}
	return output(map[string]any{"seen": ev["type"], "count": n + 1})
}

//go:wasmexport tool
func tool() int32 {
	var in struct {
		Name string
		Args map[string]any
	}
	_ = json.Unmarshal(pdk.Input(), &in)
	if in.Name == "fail" {
		pdk.SetErrorString("tool failed on purpose")
		return 1
	}
	return output(map[string]any{"tool": in.Name, "args": in.Args})
}

// block_render draws a "timeline" block: the source as text. It also
// sends a script, which the host must remove, and reports "bad" lines.
//
//go:wasmexport block_render
func blockRender() int32 {
	var in struct {
		Kind, Source, Project, Locale string
		Width                         int
		Public                        bool
	}
	_ = json.Unmarshal(pdk.Input(), &in)
	if in.Source == "graph" {
		// Draws what it reads: entries, edges, an option and whether the picture is public.
		g, _ := host("graph.subgraph", map[string]any{"kinds": []string{"ups"}})["result"].(map[string]any)
		ents, _ := g["entities"].([]any)
		edges, _ := g["edges"].([]any)
		st, _ := host("settings.get", map[string]any{})["result"].(map[string]any)
		b, _ := json.Marshal(map[string]any{"entities": len(ents), "edges": len(edges), "mode": st["mode"], "public": in.Public})
		return output(map[string]any{"svg": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 40"><text x="4" y="24">` + string(b) + `</text></svg>`})
	}
	var problems []map[string]any
	if in.Source == "bad" {
		problems = append(problems, map[string]any{"line": 1, "message": "bad is not a date"})
	}
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 40"><script>alert(1)</script>` +
		`<rect width="200" height="40" style="fill:var(--surface)" onclick="alert(2)"/><text x="4" y="24">` + in.Kind + `:` + in.Locale + `</text></svg>`
	return output(map[string]any{"svg": svg, "problems": problems})
}

//go:wasmexport spin
func spin() int32 {
	for {
	}
}

//go:wasmexport hog
func hog() int32 {
	var keep [][]byte
	for {
		keep = append(keep, make([]byte, 1<<20))
		if len(keep) < 0 {
			return 0
		}
	}
}
