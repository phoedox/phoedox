package core

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"

	"github.com/phoedox/phoedox/internal/store"
)

// Blocks that keep something per reader (steps ticked off, a vote in a
// poll, answers in a quiz) store it here, per page, block and person. Any
// reader of the page may keep their own state; polls also show the count of
// all votes (never who voted).

const maxBlockState = 16 << 10

var blockKeyRe = regexp.MustCompile(`^[A-Za-z0-9-]{1,40}$`)

// BlockStateView is a reader's state of a block and, for polls, the votes of all.
type BlockStateView struct {
	Mine   json.RawMessage `json:"mine"`
	Votes  map[string]int  `json:"votes,omitempty"`  // choice → number of people
	Voters int             `json:"voters,omitempty"` // people who voted
}

func (s *Service) blockDoc(ctx context.Context, a Actor, ref, docRef, key string) (store.Project, store.Document, error) {
	if !blockKeyRe.MatchString(key) {
		return store.Project{}, store.Document{}, invalid("validation.block_key", nil, "a block key is 1–40 letters, digits and -")
	}
	return s.docAccess(ctx, a, ref, docRef, store.RoleReader)
}

// BlockState reads the caller's state of a block and the votes of all readers.
func (s *Service) BlockState(ctx context.Context, a Actor, ref, docRef, key string) (BlockStateView, error) {
	_, doc, err := s.blockDoc(ctx, a, ref, docRef, key)
	if err != nil {
		return BlockStateView{}, err
	}
	v := BlockStateView{Mine: json.RawMessage("null")}
	all, err := s.DB.BlockStates(ctx, doc.ID, key)
	if err != nil {
		return v, err
	}
	for _, b := range all {
		if b.UserID == a.User.ID {
			v.Mine = json.RawMessage(b.Data)
		}
		var d struct {
			Choices []string `json:"choices"`
		}
		if json.Unmarshal([]byte(b.Data), &d) == nil && len(d.Choices) > 0 {
			if v.Votes == nil {
				v.Votes = map[string]int{}
			}
			seen := map[string]bool{}
			for _, c := range d.Choices {
				if !seen[c] && len(c) <= 200 {
					seen[c] = true
					v.Votes[c]++
				}
			}
			v.Voters++
		}
	}
	return v, nil
}

// SetBlockState stores the caller's state of a block (a JSON object; null
// removes it) and returns the new view.
func (s *Service) SetBlockState(ctx context.Context, a Actor, ref, docRef, key string, data json.RawMessage) (BlockStateView, error) {
	if err := s.fullUser(a); err != nil {
		return BlockStateView{}, err
	}
	p, doc, err := s.blockDoc(ctx, a, ref, docRef, key)
	if err != nil {
		return BlockStateView{}, err
	}
	val := ""
	if len(data) > 0 && string(data) != "null" {
		var obj map[string]any
		if len(data) > maxBlockState || json.Unmarshal(data, &obj) != nil {
			return BlockStateView{}, invalid("validation.block_state", map[string]any{"max": maxBlockState >> 10}, "a block state is a JSON object up to %d KB", maxBlockState>>10)
		}
		if c, ok := obj["choices"].([]any); ok && len(c) > 50 {
			return BlockStateView{}, invalid("validation.block_state", map[string]any{"max": maxBlockState >> 10}, "too many choices")
		}
		val = string(data)
	}
	if err := s.DB.PutBlockState(ctx, p.ID, store.BlockState{DocumentID: doc.ID, Key: key, UserID: a.User.ID, Data: val}); err != nil {
		return BlockStateView{}, err
	}
	s.event(ctx, a, "block.state", doc.ID, map[string]any{"document": doc.ID, "block": key})
	return s.BlockState(ctx, a, p.Slug, doc.ID, key)
}

// sortedVotes is for tests and exports: choices by count.
func sortedVotes(v map[string]int) []string {
	out := make([]string, 0, len(v))
	for k := range v {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return v[out[i]] > v[out[j]] || v[out[i]] == v[out[j]] && out[i] < out[j] })
	return out
}
