package plugins

import "testing"

func TestManifestItemTypes(t *testing.T) {
	theme := `{"type":"theme","id":"de.example.forest","name":"Forest","version":"1.0.0","engines":{"phoedox":">=1.0.0"}}`
	m, err := ParseManifest([]byte(theme))
	if err != nil || m.Kind() != TypeTheme {
		t.Fatalf("theme without hostApi: %v", err)
	}
	if err := m.Supported("1.4.0"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		`{"type":"widget","id":"de.example.x","name":"X","version":"1.0.0","engines":{"phoedox":">=1.0.0"}}`,
		`{"type":"app","id":"de.example.x","name":"X","version":"1.0.0","engines":{"phoedox":">=1.0.0"},"permissions":["graph:read"]}`,
		`{"id":"de.example.x","name":"X","version":"1.0.0","engines":{"phoedox":">=1.0.0"}}`,
	} {
		if _, err := ParseManifest([]byte(bad)); err == nil {
			t.Fatalf("must fail: %s", bad)
		}
	}
}
