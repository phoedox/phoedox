package blocks

import (
	"testing"

	"github.com/phoedox/phoedox/resources"
)

// Every block kind has its own reference in the Phoedox skill, so AI clients
// read only the block they are about to write (references/blocks/<kind>.md).
func TestEveryBlockHasASkillReference(t *testing.T) {
	files := map[string]bool{}
	for _, f := range resources.SkillFiles() {
		files[f.Path] = true
	}
	// Registered kinds plus the blocks the core renders itself.
	kinds := append(Kinds(), "request", "compare", "status", "entity", "include", "query", "board", "whiteboard", "app", "map", "mermaid", "math", "dbmodel", "embed", "plugins")
	for _, k := range kinds {
		if !files["phoedox/references/blocks/"+k+".md"] {
			t.Errorf("block %q has no references/blocks/%s.md in the skill", k, k)
		}
	}
}
