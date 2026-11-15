// Command labelsgen copies the names of kinds, attributes, relations and
// choices from the web app's translation tables into resources/publish.
package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"

	"github.com/phoedox/phoedox/internal/publish"
)

func main() {
	root := findRoot()
	for _, l := range []string{"de", "en"} {
		ts, err := os.ReadFile(filepath.Join(root, "web", "src", "i18n", l+".ts"))
		if err != nil {
			log.Fatal(err)
		}
		b, _ := json.MarshalIndent(publish.ExtractLabels(string(ts)), "", "  ")
		if err := os.WriteFile(filepath.Join(root, "resources", "publish", "labels."+l+".json"), append(b, '\n'), 0o644); err != nil {
			log.Fatal(err)
		}
	}
}

func findRoot() string {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			log.Fatal("go.mod not found")
		}
		dir = parent
	}
}
