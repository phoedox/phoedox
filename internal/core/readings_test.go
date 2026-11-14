package core

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/plugins"
	"github.com/phoedox/phoedox/internal/plugins/plugintest"
	"github.com/phoedox/phoedox/internal/store"
)

func TestReadings(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _, priv := pluginSetup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus"})
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleReader)
	_, _ = s.UpsertEntity(ctx, owner, p.Slug, EntityInput{Kind: "room", Name: "Keller"})

	now := time.Now().Add(-2 * time.Hour)
	var in []ReadingInput
	for i := range 600 {
		at := now.Add(time.Duration(i) * 10 * time.Second)
		in = append(in, ReadingInput{Entity: "keller", Metric: "temperature", Value: 18 + float64(i%10)/10, Unit: "°C", At: &at})
	}
	if _, err := s.RecordReadings(ctx, owner, p.Slug, in); invalidKey(err) != "validation.readings_count" {
		t.Fatalf("too many: %v", err)
	}
	if _, err := s.RecordReadings(ctx, reader, p.Slug, in[:10]); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reader recorded: %v", err)
	}
	if _, err := s.RecordReadings(ctx, Actor{User: owner.User, Via: "mcp"}, p.Slug, in[:10]); !errors.Is(err, ErrForbidden) {
		t.Fatalf("AI recorded: %v", err)
	}
	future := time.Now().Add(time.Hour)
	if _, err := s.RecordReadings(ctx, owner, p.Slug, []ReadingInput{{Entity: "keller", Metric: "x", Value: 1, At: &future}}); invalidKey(err) != "validation.reading" {
		t.Fatalf("future: %v", err)
	}
	if _, err := s.RecordReadings(ctx, owner, p.Slug, []ReadingInput{{Entity: "nope", Metric: "x", Value: 1}}); invalidKey(err) != "validation.reading" {
		t.Fatalf("unknown entry: %v", err)
	}
	// Readings closer than 30 s are skipped: every third of 10 s steps is kept.
	res, err := s.RecordReadings(ctx, owner, p.Slug, in[:300])
	if err != nil || res.Recorded != 100 || res.Skipped != 200 {
		t.Fatalf("record: %+v %v", res, err)
	}
	res, _ = s.RecordReadings(ctx, owner, p.Slug, in[300:])
	_, _ = s.RecordReadings(ctx, owner, p.Slug, []ReadingInput{{Entity: "keller", Metric: "switch.licht", Value: 1}})

	series, err := s.EntityReadings(ctx, reader, p.Slug, "keller", "", 24)
	if err != nil || len(series) != 2 || series[1].Metric != "temperature" || series[1].Unit != "°C" || len(series[1].Points) != 200 || series[0].Last != 1 {
		t.Fatalf("series: %+v %v", series, err)
	}
	// Longer series are condensed to at most 240 buckets with min and max.
	var many []ReadingInput
	for i := range 400 {
		at := time.Now().Add(-time.Duration(i+1) * 40 * time.Second)
		many = append(many, ReadingInput{Entity: "keller", Metric: "power", Value: float64(i), At: &at})
	}
	_, _ = s.RecordReadings(ctx, owner, p.Slug, many)
	series, _ = s.EntityReadings(ctx, reader, p.Slug, "keller", "power", 5)
	if len(series) != 1 || len(series[0].Points) > 240 || len(series[0].Points) < 100 || series[0].Points[0][2] > series[0].Points[0][1] || series[0].Points[0][3] < series[0].Points[0][1] {
		t.Fatalf("condensed: %d %+v", len(series[0].Points), series[0].Points[:2])
	}

	// Plugins record on entries of kinds they may write.
	admin := owner
	admin.User.IsAdmin = true
	data := plugintest.Package(t, plugintest.Manifest)
	if _, err := s.InstallPlugin(ctx, admin, data, signed(t, priv, data), "upload"); err != nil {
		t.Fatal(err)
	}
	_ = s.SetProjectPlugin(ctx, owner, p.Slug, "de.example.ups", true)
	_, _ = s.UpsertEntity(ctx, owner, p.Slug, EntityInput{Kind: "ups", Name: "USV", Attrs: map[string]any{"kapazitaet": 900}})
	ap, _ := s.activePlugin(ctx, p.ID, "de.example.ups")
	env := s.pluginEnv(Actor{User: owner.User, Via: "web", Plugin: "de.example.ups"}, p, ap)
	raw, _ := json.Marshal([]ReadingInput{{Entity: "usv", Metric: "charge", Value: 97, Unit: "%"}})
	if out, err := env.GraphRecord(ctx, raw, ap.pkg.Manifest.CanWriteKind); err != nil || out.(RecordResult).Recorded != 1 {
		t.Fatalf("plugin record: %v %v", out, err)
	}
	raw, _ = json.Marshal([]ReadingInput{{Entity: "keller", Metric: "charge", Value: 1}})
	if _, err := env.GraphRecord(ctx, raw, ap.pkg.Manifest.CanWriteKind); !errors.Is(err, ErrForbidden) {
		t.Fatalf("plugin recorded on a room: %v", err)
	}
	readerEnv := s.pluginEnv(Actor{User: reader.User, Via: "web", Plugin: "de.example.ups"}, p, ap)
	raw, _ = json.Marshal([]ReadingInput{{Entity: "usv", Metric: "charge", Value: 50}})
	if _, err := readerEnv.GraphRecord(ctx, raw, ap.pkg.Manifest.CanWriteKind); !errors.Is(err, ErrForbidden) {
		t.Fatalf("plugin recorded for a reader: %v", err)
	}
	_ = plugins.ErrDenied

	s.PruneReadings(store.System(ctx), time.Now().AddDate(0, 0, ReadingDays).Add(time.Hour))
	if series, _ = s.EntityReadings(ctx, reader, p.Slug, "keller", "", 24*90); len(series) != 0 {
		t.Fatalf("after prune: %+v", series)
	}
}
