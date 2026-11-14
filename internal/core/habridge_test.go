package core

import (
	"context"
	"encoding/json"
	"testing"
)

// The agent bridge: host readings from reports and Home Assistant switches
// that the agent runs as signed jobs (ADR-0053).
func TestAgentHomeAssistantBridge(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus"})
	e, _ := s.CreateEnrollment(ctx, owner, p.Slug)
	en, err := s.EnrollAgent(ctx, e.Code, "pi")
	if err != nil {
		t.Fatal(err)
	}
	disk, _ := json.Marshal(map[string]any{"mount": "/", "totalBytes": 1000, "freeBytes": 250})
	if err := s.AgentReport(ctx, en.Token, Report{Hostname: "pi", MemTotal: 4000, MemAvail: 1000, Disks: []json.RawMessage{disk},
		HomeAssistant: &HAReport{Devices: json.RawMessage(`[]`), States: []HAState{{EntityID: "switch.flur", State: "on"}}}}); err != nil {
		t.Fatal(err)
	}
	series, err := s.EntityReadings(ctx, owner, p.Slug, "pi", "", 24)
	if err != nil || len(series) != 2 || series[0].Metric != "disk_used:/" || series[0].Last != 75 || series[1].Last != 75 || series[1].Unit != "%" {
		t.Fatalf("host readings: %+v %v", series, err)
	}

	// A device the Home Assistant plugin linked to the agent offers on/off/toggle.
	if _, err := s.UpsertEntity(ctx, owner, p.Slug, EntityInput{Kind: "smart_plug", Name: "Flur", Attrs: map[string]any{"ha_entity": "switch.flur", "bridge_agent": en.AgentID}}); err != nil {
		t.Fatal(err)
	}
	acts, err := s.EntityAgentActions(ctx, owner, p.Slug, "flur")
	if err != nil || len(acts.Actions) != 3 || acts.Actions[0].ID != "ha.turn_on" || acts.Actions[0].Action != "ha.call" || acts.Actions[0].Agent != en.AgentID {
		t.Fatalf("actions: %+v %v", acts, err)
	}
	j, err := s.CreateAgentJob(ctx, owner, p.Slug, "flur", "ha.turn_off")
	if err != nil || j.Action != "ha.call" || j.Status != "unsigned" || j.AgentID != en.AgentID {
		t.Fatalf("job: %+v %v", j, err)
	}
	var job Job
	_ = json.Unmarshal([]byte(j.Payload), &job)
	if len(job.Args) != 2 || job.Args[0] != "switch.flur" || job.Args[1] != "turn_off" {
		t.Fatalf("payload: %+v", job)
	}
	// Direct plugin control is not offered for bridged devices.
	if info, err := s.Device(ctx, owner, p.Slug, "flur"); err != nil || len(info.Tools) != 0 {
		t.Fatalf("bridged device tools: %+v %v", info, err)
	}
	// Entities that cannot be switched get no actions.
	_, _ = s.UpsertEntity(ctx, owner, p.Slug, EntityInput{Kind: "device", Name: "Sensor", Attrs: map[string]any{"ha_entity": "sensor.temp", "bridge_agent": en.AgentID}})
	if acts, _ := s.EntityAgentActions(ctx, owner, p.Slug, "sensor"); len(acts.Actions) != 0 {
		t.Fatalf("sensor actions: %+v", acts.Actions)
	}
}
