package core

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/phoedox/phoedox/internal/plugins/plugintest"
	"github.com/phoedox/phoedox/internal/store"
)

func TestDevices(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, stranger, priv := pluginSetup(t)
	admin := owner
	admin.User.IsAdmin = true
	data := plugintest.Package(t, plugintest.Manifest)
	if _, err := s.InstallPlugin(ctx, admin, data, signed(t, priv, data), "upload"); err != nil {
		t.Fatal(err)
	}
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus", AIWriteMode: AISuggest})
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleReader)
	if err := s.SetProjectPlugin(ctx, owner, p.Slug, "de.example.ups", true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertEntity(ctx, owner, p.Slug, EntityInput{Kind: "ups", Name: "Keller USV", Attrs: map[string]any{"kapazitaet": 1500}}); err != nil {
		t.Fatal(err)
	}
	_, _ = s.UpsertEntity(ctx, owner, p.Slug, EntityInput{Kind: "room", Name: "Keller"})

	info, err := s.Device(ctx, reader, p.Slug, "keller-usv")
	if err != nil || len(info.Tools) != 1 || info.Tools[0].Arg != "ups" || info.Tools[0].Value != "keller-usv" || info.CanControl || info.CanManage || info.Grants != nil {
		t.Fatalf("reader view: %+v %v", info, err)
	}
	if info, _ := s.Device(ctx, owner, p.Slug, "keller"); len(info.Tools) != 0 {
		t.Fatalf("a room is no device: %+v", info)
	}

	// Readers cannot switch until the owner shares the device with them.
	in := ControlInput{Plugin: "de.example.ups", Tool: "ups.switch", Args: json.RawMessage(`{"on":false}`)}
	if _, err := s.ControlDevice(ctx, reader, p.Slug, "keller-usv", in); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reader switched: %v", err)
	}
	if _, err := s.GrantControl(ctx, reader, p.Slug, "keller-usv", DeviceGrantInput{Email: stranger.User.Email}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reader shared: %v", err)
	}
	if _, err := s.GrantControl(ctx, owner, p.Slug, "keller", DeviceGrantInput{Email: stranger.User.Email}); invalidKey(err) != "validation.not_a_device" {
		t.Fatalf("shared a room: %v", err)
	}
	if _, err := s.GrantControl(ctx, owner, p.Slug, "keller-usv", DeviceGrantInput{Email: "nobody@x.de"}); invalidKey(err) != "validation.user_unknown" {
		t.Fatalf("unknown user: %v", err)
	}
	info, err = s.GrantControl(ctx, owner, p.Slug, "keller-usv", DeviceGrantInput{Email: reader.User.Email, Days: 7})
	if err != nil || len(info.Grants) != 1 || info.Grants[0].ExpiresAt.IsZero() {
		t.Fatalf("grant: %+v %v", info, err)
	}
	if out, err := s.ControlDevice(ctx, reader, p.Slug, "keller-usv", in); err != nil || !strings.Contains(string(out), `"ups":"keller-usv"`) {
		t.Fatalf("grantee switch: %s %v", out, err)
	}

	// Someone without any rights in the project sees and switches only the shared device.
	if _, err := s.Project(ctx, stranger, p.Slug); !errors.Is(err, ErrNotFound) {
		t.Fatal("stranger sees the project")
	}
	_, _ = s.GrantControl(ctx, owner, p.Slug, "keller-usv", DeviceGrantInput{Email: stranger.User.Email})
	shared, err := s.SharedDevices(ctx, stranger)
	if err != nil || len(shared) != 1 || shared[0].Name != "Keller USV" || len(shared[0].Tools) != 1 || shared[0].SharedBy == "" {
		t.Fatalf("shared: %+v %v", shared, err)
	}
	if out, err := s.ControlShared(ctx, stranger, shared[0].Grant, in); err != nil || !strings.Contains(string(out), `"on":false`) {
		t.Fatalf("shared switch: %s %v", out, err)
	}
	if _, err := s.ControlShared(ctx, reader, shared[0].Grant, in); !errors.Is(err, ErrNotFound) {
		t.Fatalf("someone else's grant: %v", err)
	}
	if _, err := s.ControlShared(ctx, stranger, shared[0].Grant, ControlInput{Plugin: "de.example.ups", Tool: "ups.runtime"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-control tool via grant: %v", err)
	}

	// AI clients propose, unless the owner allows AI control of this device.
	ai := Actor{User: owner.User, Via: "mcp"}
	args := json.RawMessage(`{"ups":"keller-usv","on":true}`)
	if out, err := s.PluginTool(ctx, ai, p.Slug, "de.example.ups", "ups.switch", args); err != nil || !strings.Contains(string(out), `"applied":false`) {
		t.Fatalf("AI before: %s %v", out, err)
	}
	if _, err := s.SetDeviceAI(ctx, ai, p.Slug, "keller-usv", AIDirect); !errors.Is(err, ErrForbidden) {
		t.Fatalf("AI allowed itself: %v", err)
	}
	if info, err = s.SetDeviceAI(ctx, owner, p.Slug, "keller-usv", AIDirect); err != nil || info.AIControl != AIDirect {
		t.Fatalf("set AI: %+v %v", info, err)
	}
	if out, err := s.PluginTool(ctx, ai, p.Slug, "de.example.ups", "ups.switch", args); err != nil || !strings.Contains(string(out), `"on":true`) {
		t.Fatalf("AI after: %s %v", out, err)
	}
	// Other devices and calls without a target still need a person.
	if out, _ := s.PluginTool(ctx, ai, p.Slug, "de.example.ups", "ups.switch", json.RawMessage(`{"on":true}`)); !strings.Contains(string(out), `"applied":false`) {
		t.Fatalf("AI without target: %s", out)
	}

	// The log shows every switch on the device, with the people behind it.
	log, err := s.DeviceLog(ctx, reader, p.Slug, "keller-usv")
	if err != nil {
		t.Fatal(err)
	}
	controls := 0
	for _, e := range log {
		if e.Kind == "device.control" {
			controls++
		}
	}
	if controls != 3 || log[0].Who == "" {
		t.Fatalf("log: %d %+v", controls, log)
	}

	// Revoking ends it.
	info, _ = s.Device(ctx, owner, p.Slug, "keller-usv")
	for _, g := range info.Grants {
		if _, err := s.RevokeControl(ctx, owner, p.Slug, "keller-usv", g.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.ControlShared(ctx, stranger, shared[0].Grant, in); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after revoke: %v", err)
	}
	if _, err := s.ControlDevice(ctx, reader, p.Slug, "keller-usv", in); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reader after revoke: %v", err)
	}
}
