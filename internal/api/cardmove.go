package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// A card's move to another room, for the board's `move to room`. The hub drives it, so the room only names the card
// and the hub puts the room it moves from in front, from the link. See internal/link/moveask.go.

const hubMovePath = "/_hub/move"

func (s *Server) cardMove(w http.ResponseWriter, r *http.Request) {
	t, err := s.st.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	method, path, body := http.MethodGet, hubMovePath+"/"+url.PathEscape(strings.TrimSpace(r.URL.Query().Get("move"))), []byte(nil)
	if r.Method == http.MethodPost {
		var in struct {
			To   string `json:"to"`
			Wip  bool   `json:"wip"`
			Undo string `json:"undo"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&in); err != nil && !errors.Is(err, io.EOF) {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		method, path = http.MethodPost, hubMovePath
		body, _ = json.Marshal(map[string]any{"card": t.ID, "to": strings.TrimSpace(in.To), "wip": in.Wip})
		if u := strings.TrimSpace(in.Undo); u != "" {
			path, body = hubMovePath+"/"+url.PathEscape(u)+"/undo", []byte("{}")
		}
	} else if path == hubMovePath+"/" {
		writeErr(w, http.StatusBadRequest, errors.New("say which move, as ?move=<id>"))
		return
	}
	code, data, msg := s.HubCall(r.Context(), t.WireName, method, path, body)
	if code != http.StatusOK {
		writeJSON(w, code, map[string]string{"error": msg})
		return
	}
	if len(data) == 0 {
		data = []byte("{}")
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
