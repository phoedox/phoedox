package core

import (
	"context"
	"testing"

	"github.com/phoedox/phoedox/internal/store"
)

func TestFeedbackTriage(t *testing.T) {
	ctx := context.Background()
	s, owner, _, _ := setup(t)
	s.SyncNotify = true
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Starfall", AIWriteMode: AISuggest})
	settings := DefaultPortalSettings()
	settings.Anonymous = true
	settings.Categories = []string{"UI", "Server"}
	settings.Tags = []string{"crash", "map"}
	settings.AITriage = true
	if _, err := s.SaveFeedbackPortal(ctx, owner, p.Slug, "", PortalInput{Handle: "starfall", Name: "Starfall", Settings: &settings}); err != nil {
		t.Fatal(err)
	}
	// Without AI the rules sort the report as before.
	rec, err := s.SubmitFeedback(ctx, nil, "starfall", ReportInput{Type: "bug", Title: "Map crashes", Body: "When I open the map the game closes."}, nil, Intake{Via: "form"})
	if err != nil {
		t.Fatal(err)
	}
	first, _ := s.DB.FeedbackTopic(ctx, p.ID, rec.TopicID)
	if first.AINote != "" || first.Priority != 2 {
		t.Fatalf("rules only: %+v", first)
	}
	f := &fakeAI{text: "```json\n{\"summary\":\"Inventory loses items after saving.\",\"category\":\"Server\",\"tags\":[\"crash\",\"unknown\"],\"priority\":1,\"reason\":\"data loss\",\"duplicate_of\":\"" + rec.TopicID + "\",\"spam\":false}\n```"}
	s.AI = f
	rec2, err := s.SubmitFeedback(ctx, nil, "starfall", ReportInput{Type: "bug", Title: "Items vanish from inventory", Body: "After saving, my inventory is empty."}, nil, Intake{Via: "form"})
	if err != nil {
		t.Fatal(err)
	}
	topic, _ := s.DB.FeedbackTopic(ctx, p.ID, rec2.TopicID)
	if f.calls != 1 || topic.Summary != "Inventory loses items after saving." || topic.Category != "Server" || topic.Priority != 1 ||
		len(topic.Tags) != 1 || topic.Tags[0] != "crash" || topic.AINote != "data loss" || topic.DuplicateOf != rec.TopicID || topic.Spam {
		t.Fatalf("triaged: calls %d %+v", f.calls, topic)
	}
	// Usage counts as background AI of the project.
	if used, _ := s.DB.AIProjectUsageSince(ctx, p.ID, "bg:", monthStart(store.Now().Time)); used == 0 {
		t.Fatal("no background usage")
	}
}
