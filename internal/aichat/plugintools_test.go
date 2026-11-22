package aichat

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/ai"
	"github.com/phoedox/phoedox/internal/blob"
	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/plugins"
	"github.com/phoedox/phoedox/internal/plugins/plugintest"
)

// The chat offers the tools of plugins enabled in its project and runs
// them there; elsewhere they are not offered.
func TestChatOffersProjectPluginTools(t *testing.T) {
	ctx := context.Background()
	s, f, a, p := setup(t)
	dir := t.TempDir()
	s.Core.Blobs = blob.Local{Dir: filepath.Join(dir, "files")}
	s.Core.TempDir = dir
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	rt := plugins.NewRuntime(slog.New(slog.NewTextHandler(io.Discard, nil)), 4)
	rt.Code = core.PluginErrorCode
	t.Cleanup(func() { rt.Close(context.Background()) })
	s.Core.Plugins = core.PluginConfig{Runtime: rt, Keys: map[string]ed25519.PublicKey{"store-1": pub}, Version: "1.4.0"}

	admin := a
	admin.User.IsAdmin = true
	data := plugintest.Package(t, plugintest.Manifest)
	pk, err := plugins.ReadPackage(data)
	if err != nil {
		t.Fatal(err)
	}
	sig := plugins.Sign(priv, "store-1", pk.Manifest.ID, pk.Manifest.Version, pk.SHA256)
	if _, err := s.Core.InstallPlugin(ctx, admin, data, &sig, "upload"); err != nil {
		t.Fatal(err)
	}
	chat, err := s.Create(ctx, a, p.Slug, "")
	if err != nil {
		t.Fatal(err)
	}
	has := func(req ai.Request, name string) bool {
		for _, tl := range req.Tools {
			if tl.Name == name {
				return true
			}
		}
		return false
	}

	// Not enabled in the project: not offered.
	f.turns = []ai.Turn{turn("Nothing to check.")}
	if err := s.Send(ctx, a, p.Slug, chat.ID, Input{Text: "How long does the UPS last?"}, func(Event) {}); err != nil {
		t.Fatal(err)
	}
	if has(f.reqs[0], "plugin_ups_runtime") {
		t.Fatal("tool of a plugin that is off in the project")
	}

	if err := s.Core.SetProjectPlugin(ctx, a, p.Slug, "de.example.ups", true); err != nil {
		t.Fatal(err)
	}
	f.reqs = nil
	f.turns = []ai.Turn{
		turn("", call("p1", "plugin_ups_runtime", `{"ups":"keller"}`)),
		turn("About an hour."),
	}
	var events []Event
	if err := s.Send(ctx, a, p.Slug, chat.ID, Input{Text: "And now?"}, func(e Event) { events = append(events, e) }); err != nil {
		t.Fatal(err)
	}
	if !has(f.reqs[0], "plugin_ups_runtime") || !has(f.reqs[0], "plugin_ups_switch") {
		t.Fatalf("plugin tools not offered: %+v", f.reqs[0].Tools)
	}
	ok := false
	for _, e := range events {
		if e.Type == "tool_done" && e.Tool == "plugin_ups_runtime" {
			ok = e.OK
		}
	}
	if !ok || !strings.Contains(string(f.reqs[1].History[len(f.reqs[1].History)-1].Raw), "keller") {
		t.Fatalf("tool run: %+v", events)
	}
}
