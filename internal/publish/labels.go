package publish

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/phoedox/phoedox/resources"
)

// Names of kinds, attributes, relations and choices for server-side pages
// (offline copy). They come from the web app's translation tables:
// `go run ./internal/publish/labelsgen` writes resources/publish/labels.*.json,
// and TestLabelsInSync fails when they drift apart.

//go:generate go run ./labelsgen

var labelKey = regexp.MustCompile(`(?m)^\s*"((?:kind|rel|attr|opt)\.[^"]+)":\s*"((?:[^"\\]|\\.)*)",?\s*$`)

// ExtractLabels reads the label entries of a web translation table (de.ts).
func ExtractLabels(ts string) map[string]string {
	out := map[string]string{}
	for _, m := range labelKey.FindAllStringSubmatch(ts, -1) {
		var v string
		if json.Unmarshal([]byte(`"`+m[2]+`"`), &v) == nil {
			out[m[1]] = v
		}
	}
	return out
}

type labels map[string]string

func labelsFor(locale string) labels {
	l := labels{}
	_ = json.Unmarshal(resources.PublishLabels(locale), &l)
	return l
}

func (l labels) get(prefix, id string) string {
	if v, ok := l[prefix+"."+id]; ok {
		return v
	}
	return strings.ReplaceAll(id, "_", " ")
}
