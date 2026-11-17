// Package blocks holds the content blocks of pages and whiteboards
// (ADR-0085): each kind registers once how the server checks its source and
// what search sees of it. Pages carry a block as a fenced block with the
// kind as language; whiteboards as an element of type "block" with kind and
// source. The web (web/src/blocks) and the app draw them.
package blocks

import (
	"sort"
	"sync"
)

// Problem is one finding in a block.
type Problem struct {
	Block    int    `json:"block"` // 1-based number of the fenced block on the page
	Kind     string `json:"kind"`  // mermaid | dbmodel | chart | …
	Line     int    `json:"line"`  // line in the page (1-based)
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// Def describes one kind of block.
type Def struct {
	Kind string
	// Board: the block may also be placed on whiteboards.
	Board bool
	// Data: the block shows project data (entries, other documents) that
	// only signed-in readers may see; published pages leave it out.
	Data bool
	// Check returns the problems of a source (nil when it is fine).
	Check func(src []byte) []Problem
	// Text is what search and mentions see of a source.
	Text func(src []byte) string
}

// MaxSource is the largest source of a block.
const MaxSource = 128 << 10

// Colors are the palette blocks may use (as on whiteboards).
var Colors = map[string]bool{"gray": true, "red": true, "orange": true, "yellow": true, "green": true, "blue": true, "purple": true, "pink": true}

var (
	mu   sync.RWMutex
	defs = map[string]Def{}
)

// Register adds a kind; called from init functions.
func Register(d Def) {
	mu.Lock()
	defer mu.Unlock()
	defs[d.Kind] = d
}

// Get returns the definition of a kind.
func Get(kind string) (Def, bool) {
	mu.RLock()
	defer mu.RUnlock()
	d, ok := defs[kind]
	return d, ok
}

// Kinds lists the registered kinds, sorted.
func Kinds() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(defs))
	for k := range defs {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Check runs the check of a kind; unknown kinds are a problem themselves.
func Check(kind string, src []byte) []Problem {
	d, ok := Get(kind)
	if !ok {
		return []Problem{{Kind: kind, Line: 1, Severity: "error", Message: "unknown block kind " + kind}}
	}
	if len(src) > MaxSource {
		return []Problem{{Kind: kind, Line: 1, Severity: "error", Message: "the block is larger than 128 KB"}}
	}
	if d.Check == nil {
		return nil
	}
	ps := d.Check(src)
	for i := range ps {
		if ps[i].Kind == "" {
			ps[i].Kind = kind
		}
	}
	return ps
}
