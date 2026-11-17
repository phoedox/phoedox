package core

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/phoedox/phoedox/internal/ai"
	"github.com/phoedox/phoedox/internal/store"
)

// fakeAI answers every call with the same text.
type fakeAI struct {
	text  string
	calls int
}

func (f *fakeAI) Models(context.Context) ([]string, error) { return []string{"claude-opus-5-5"}, nil }

func (f *fakeAI) Stream(ctx context.Context, req ai.Request, emit func(ai.Event)) (ai.Turn, error) {
	f.calls++
	raw, _ := json.Marshal(map[string]any{"role": "assistant", "content": []map[string]any{{"type": "text", "text": f.text}}})
	return ai.Turn{Message: raw, Stop: "end_turn", Usage: ai.Usage{Model: "claude-opus-5-5", Input: 1000, Output: 500}}, nil
}

func TestBackgroundAILimits(t *testing.T) {
	ten, five, zero := 10, 5, 0
	for _, c := range []struct {
		l    BackgroundAILimits
		want int
	}{
		{BackgroundAILimits{}, 0},
		{BackgroundAILimits{OrgGeneral: &ten}, 10},
		{BackgroundAILimits{OrgGeneral: &five, OrgProject: &ten}, 10}, // the project limit replaces the general one
		{BackgroundAILimits{OrgGeneral: &ten, Owner: &five}, 5},
		{BackgroundAILimits{OrgProject: &ten, Owner: &zero}, -1}, // 0 = off
	} {
		if got := c.l.effective(); got != c.want {
			t.Errorf("%+v: %d, want %d", c.l, got, c.want)
		}
	}
}

func TestBackgroundAsk(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	f := &fakeAI{text: "hello"}
	s.AI = f
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Spiel", AIWriteMode: AISuggest})
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleReader)
	proj, _ := s.DB.ProjectFor(ctx, p.ID, "")
	out, err := s.BackgroundAsk(ctx, proj, "test", []string{"system"}, "hi", 100)
	if err != nil || out != "hello" {
		t.Fatalf("ask: %v %q", err, out)
	}
	if _, err := s.SetOwnerBackgroundAI(ctx, reader, p.Slug, &[]int{1}[0]); !errors.Is(err, ErrForbidden) {
		t.Fatalf("readers must not set the limit: %v", err)
	}
	st, err := s.SetOwnerBackgroundAI(ctx, owner, p.Slug, &[]int{3}[0])
	if err != nil || st.Limit != 3 || st.Used < 1 {
		t.Fatalf("owner limit: %v %+v", err, st)
	}
	for i := 0; i < 5; i++ {
		_, err = s.BackgroundAsk(ctx, proj, "test", nil, "hi", 100)
	}
	if !errors.Is(err, ErrNoBackgroundAI) {
		t.Fatalf("limit reached: %v", err)
	}
	st, _ = s.BackgroundAI(ctx, reader, p.Slug)
	if st.Available || st.Reason != "bgai.limit" || st.CanSetOwner {
		t.Fatalf("status: %+v", st)
	}
	// Background usage counts against the owner's budget in personal projects.
	if used, _ := s.DB.AIUsageSince(ctx, "", owner.User.ID, monthStart(store.Now().Time), false); used == 0 {
		t.Fatal("owner's budget not charged")
	}
}
