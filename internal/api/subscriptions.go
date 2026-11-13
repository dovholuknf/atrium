package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

// A card's subscriptions to hub events, for `atrium_subscribe` and `atrium subscribe`. One handler: both act on the
// calling session's own card, named in the path, and the hub keeps the rows. The room says only the card's id. The hub
// puts the room in front of it from the link, so a room can never write another room's card. See
// docs/fabric/land-webhook-design.md, 3.

const hubSubsPath = "/_hub/events/subs"

const hubRemindPath = "/_hub/remind"

func (s *Server) cardSubscriptions(w http.ResponseWriter, r *http.Request) {
	t, err := s.st.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	var in struct {
		Action string `json:"action"`
		ID     string `json:"id"`
		Kind   string `json:"kind"`
		Repo   string `json:"repo"`
		Branch string `json:"branch"`
		Wake   bool   `json:"wake"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&in); err != nil && !errors.Is(err, io.EOF) {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if in.Action == "" {
		in.Action = "list"
	}
	body, _ := json.Marshal(map[string]any{"action": in.Action, "id": strings.TrimSpace(in.ID), "kind": strings.TrimSpace(in.Kind),
		"repo": strings.TrimSpace(in.Repo), "branch": strings.TrimSpace(in.Branch), "wake": in.Wake, "card": t.ID})
	code, data, msg := s.HubCall(r.Context(), t.WireName, http.MethodPost, hubSubsPath, body)
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

// cardRemind is `POST /v1/tasks/{id}/remind`, atrium_remind: the hub keeps the reminder. It names the calling card
// unless `card` is another card of THIS room, and the hub puts the room in front from the link.
func (s *Server) cardRemind(w http.ResponseWriter, r *http.Request) {
	t, err := s.st.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	var in struct {
		Action string `json:"action"`
		ID     string `json:"id"`
		Card   string `json:"card"`
		In     string `json:"in"`
		At     string `json:"at"`
		Text   string `json:"text"`
		Wake   bool   `json:"wake"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<13)).Decode(&in); err != nil && !errors.Is(err, io.EOF) {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	target := t
	if c := strings.TrimSpace(in.Card); c != "" && c != t.ID {
		if target, err = s.st.Get(c); err != nil {
			writeErr(w, http.StatusNotFound, errors.New("card "+c+" is not a card on this room"))
			return
		}
	}
	if in.Action == "" {
		in.Action = "list"
	}
	body, _ := json.Marshal(map[string]any{"action": in.Action, "id": strings.TrimSpace(in.ID), "in": strings.TrimSpace(in.In),
		"at": strings.TrimSpace(in.At), "text": in.Text, "wake": in.Wake, "card": target.ID})
	code, data, msg := s.HubCall(r.Context(), t.WireName, http.MethodPost, hubRemindPath, body)
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

// cardsExist is `POST /v1/tasks/exist` with `{"ids": [...]}`, answering the ids this room still holds a card for. An
// archived or finished card still exists. The hub asks once a day for the cards its subscriptions name.
func (s *Server) cardsExist(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	have := make([]string, 0, len(in.IDs))
	for _, id := range in.IDs {
		_, err := s.st.Get(id)
		switch {
		case err == nil:
			have = append(have, id)
		case !errors.Is(err, sql.ErrNoRows):
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"exist": have})
}
