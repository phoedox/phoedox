package publish

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/phoedox/phoedox/resources"
)

func TestLabelsInSync(t *testing.T) {
	for _, l := range []string{"de", "en"} {
		ts, err := os.ReadFile("../../web/src/i18n/" + l + ".ts")
		if err != nil {
			t.Skip("web sources not available")
		}
		var have map[string]string
		_ = json.Unmarshal(resources.PublishLabels(l), &have)
		if want := ExtractLabels(string(ts)); !reflect.DeepEqual(have, want) || len(want) < 50 {
			t.Fatalf("resources/publish/labels.%s.json is out of date: run go run ./internal/publish/labelsgen", l)
		}
	}
}
