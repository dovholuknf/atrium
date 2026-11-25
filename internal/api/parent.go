package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/dovholuknf/atrium/internal/base/address"
	"github.com/dovholuknf/atrium/internal/store"
)

// ParentChanged is the `by` of the `notified` event that records an operator moving a card under another card.
const ParentChanged = "parent-changed"

// postParent nests a card under another card, or under nobody when `parent` is empty, `top`, `atrium`, `none` or `@human`. The parent is a card id or
// a handle of this room, or name@room for a card on another room.
func (s *Server) postParent(w http.ResponseWriter, r *http.Request) {
	t, err := s.st.Get(r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusNotFound, NotOnRoom(r.PathValue("id"), s.Room))
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	var in struct {
		Parent string `json:"parent"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<14)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var parent *store.Task
	ref := strings.TrimSpace(in.Parent)
	switch strings.ToLower(ref) {
	case "top", "atrium", "none", store.HumanLauncher:
		ref = ""
	}
	// A parent on another room is not in this store: record its handle, as a cross-room launch does.
	if name, room, err := address.SplitAddress(ref); err == nil && strings.Contains(ref, "@") && room != "" &&
		!strings.EqualFold(room, strings.TrimSpace(s.Room)) {
		parent = &store.Task{WireName: name + "@" + room}
	} else if ref != "" {
		if parent, err = s.st.Get(ref); errors.Is(err, sql.ErrNoRows) {
			parent, err = s.st.GetByWireName(ref)
		}
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, http.StatusNotFound, errors.New("no card on this room is called "+ref))
			return
		}
		if err != nil {
			s.fail(w, err)
			return
		}
		if parent.WireName == "" {
			writeErr(w, http.StatusBadRequest, errors.New("that card has no handle to be a parent by"))
			return
		}
	}
	if err := s.st.ChangeParent(t.ID, parent); errors.Is(err, store.ErrParentCycle) {
		writeErr(w, http.StatusConflict, err)
		return
	} else if err != nil {
		s.fail(w, err)
		return
	}
	to := ""
	if parent != nil {
		to = parent.WireName
	}
	if err := s.st.AppendEvent(t.ID, store.EventNotified, map[string]any{
		"by": ParentChanged, "from": t.SpawnedBy, "to": to,
		"what": "the operator changed this card's parent from " + t.SpawnedBy + " to " + to,
	}); err != nil {
		s.fail(w, err)
		return
	}
	s.publishTaskID(t.ID)
	writeJSON(w, http.StatusOK, map[string]any{"spawned_by": to})
}
