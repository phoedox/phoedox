package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Blocks of pages that keep something per reader (steps, polls, quizzes).

type blockStateIn struct {
	Data json.RawMessage `json:"data"`
}

func (a *api) blockState(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.BlockState(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"), chi.URLParam(r, "key"))
	respond(w, v, err)
}

func (a *api) setBlockState(w http.ResponseWriter, r *http.Request) {
	var in blockStateIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.SetBlockState(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"), chi.URLParam(r, "key"), in.Data)
	respond(w, v, err)
}
