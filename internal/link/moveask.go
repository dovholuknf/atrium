package link

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/link/ctl"
	"github.com/dovholuknf/atrium/internal/link/wire"
	"github.com/dovholuknf/atrium/internal/roommove"
)

// A card's move to another room, asked by its room over the link and driven here, because the hub reaches every room's
// board and a room reaches none. POST /_hub/move starts one and GET /_hub/move/<id> follows it. The room moved FROM is
// the asking room, from the link, never the body.

const hubMovePath = "/_hub/move"

type moveRun struct {
	mu          sync.Mutex
	from, to    string
	card        string
	state, err  string
	step, newID string
	log         bytes.Buffer
	stop        atomic.Bool
}

func (m *moveRun) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.log.Write(p)
}

type moveStatus struct {
	MoveID string   `json:"move_id"`
	Card   string   `json:"card"`
	From   string   `json:"from"`
	To     string   `json:"to"`
	Step   string   `json:"step,omitempty"`
	NewID  string   `json:"new_id,omitempty"`
	State  string   `json:"state"`
	Error  string   `json:"error,omitempty"`
	Log    []string `json:"log,omitempty"`
}

func (c *controlMCP) moveAsk(from, method, base string, body []byte) RelayAnswer {
	if base == hubMovePath {
		if method != http.MethodPost {
			return RelayAnswer{Code: http.StatusBadRequest, Error: "a move is a POST"}
		}
		var in struct {
			Card string `json:"card"`
			To   string `json:"to"`
			Wip  bool   `json:"wip"`
		}
		if json.Unmarshal(body, &in) != nil {
			return RelayAnswer{Code: http.StatusBadRequest, Error: "the body is not a JSON object"}
		}
		st, err := c.moveStart(from, in.Card, in.To, in.Wip)
		if err != nil {
			return RelayAnswer{Code: http.StatusBadRequest, Error: err.Error()}
		}
		data, _ := json.Marshal(st)
		return RelayAnswer{OK: true, Data: data}
	}
	if id, ok := strings.CutSuffix(strings.TrimPrefix(base, hubMovePath+"/"), "/undo"); ok {
		if method != http.MethodPost {
			return RelayAnswer{Code: http.StatusBadRequest, Error: "an undo is a POST"}
		}
		st, code, err := c.moveUndo(from, id)
		if err != nil {
			return RelayAnswer{Code: code, Error: err.Error()}
		}
		data, _ := json.Marshal(st)
		return RelayAnswer{OK: true, Data: data}
	}
	if method != http.MethodGet {
		return RelayAnswer{Code: http.StatusBadRequest, Error: "a move's progress is a GET"}
	}
	st, ok := c.moveStatusOf(from, strings.TrimPrefix(base, hubMovePath+"/"))
	if !ok {
		return RelayAnswer{Code: http.StatusNotFound, Error: "no such move from this room"}
	}
	data, _ := json.Marshal(st)
	return RelayAnswer{OK: true, Data: data}
}

// moveStart starts the driver and answers at once. The driver's own record says the step, and moveRun the outcome.
func (c *controlMCP) moveStart(from, card, to string, wip bool) (moveStatus, error) {
	card, to = strings.TrimSpace(card), strings.TrimSpace(to)
	if card == "" || to == "" {
		return moveStatus{}, fmt.Errorf("say the card and the room to move it to")
	}
	if wire.EqualFold(from, to) {
		return moveStatus{}, fmt.Errorf("the card is on %s already", to)
	}
	id := roommove.NewID()
	run := &moveRun{from: from, to: to, card: card, state: "running"}
	c.moves.Store(id, run)
	go func() {
		err := roommove.Run(run, card, roommove.Opts{BoardURL: c.board, From: from, To: to, Wip: wip, ID: id,
			Lease: 1800, Wait: time.Minute, Stop: run.stop.Load,
			OnStep: func(step, newID string) { run.mu.Lock(); run.step, run.newID = step, newID; run.mu.Unlock() }})
		run.mu.Lock()
		defer run.mu.Unlock()
		run.state = "done"
		if err != nil {
			run.state, run.err = "failed", err.Error()
		}
	}()
	return c.moveStatusFor(id, run), nil
}

// moveUndo stops a move before the park, and the driver undoes what it did. A move this hub no longer holds, after a
// restart, is undone here from its record.
func (c *controlMCP) moveUndo(from, id string) (moveStatus, int, error) {
	if v, ok := c.moves.Load(id); ok {
		run := v.(*moveRun)
		if !wire.EqualFold(run.from, from) {
			return moveStatus{}, http.StatusNotFound, fmt.Errorf("no such move from this room")
		}
		st := c.moveStatusFor(id, run)
		if st.State != "running" {
			return st, http.StatusConflict, fmt.Errorf("the move has ended: %s", st.State)
		}
		if !moveBeforePark(st.Step) {
			return st, http.StatusConflict, fmt.Errorf("the move is past the park at %s, so it only goes forward", st.Step)
		}
		run.stop.Store(true)
		return st, http.StatusOK, nil
	}
	rec, err := roommove.Load(id)
	if err != nil || !wire.EqualFold(rec.From, from) {
		return moveStatus{}, http.StatusNotFound, fmt.Errorf("no such move from this room")
	}
	if !moveBeforePark(rec.Step) {
		return moveStatus{}, http.StatusConflict, fmt.Errorf("the move is at %s, so there is nothing to undo", rec.Step)
	}
	run := &moveRun{from: rec.From, to: rec.To, card: rec.Card, state: "failed"}
	if err := roommove.Undo(run, id, roommove.Opts{BoardURL: c.board}); err != nil {
		run.err = err.Error()
	} else {
		run.err = "stopped on request (undone, move id " + id + ")"
	}
	if rec, err := roommove.Load(id); err == nil {
		run.step = rec.Step
	}
	c.moves.Store(id, run)
	return c.moveStatusFor(id, run), http.StatusOK, nil
}

func moveBeforePark(step string) bool {
	return step == "" || step == "frozen" || step == "checked" || step == "worktree"
}

func (c *controlMCP) moveStatusOf(room, id string) (moveStatus, bool) {
	v, ok := c.moves.Load(id)
	if !ok {
		return moveStatus{}, false
	}
	run := v.(*moveRun)
	if !wire.EqualFold(run.from, room) && !wire.EqualFold(run.to, room) {
		return moveStatus{}, false
	}
	return c.moveStatusFor(id, run), true
}

func (c *controlMCP) moveStatusFor(id string, run *moveRun) moveStatus {
	run.mu.Lock()
	st := moveStatus{MoveID: id, Card: run.card, From: run.from, To: run.to, State: run.state, Error: run.err,
		Step: run.step, NewID: run.newID}
	for _, l := range strings.Split(strings.TrimSpace(run.log.String()), "\n") {
		if l = strings.TrimSpace(strings.TrimPrefix(l, "["+id+"]")); l != "" {
			st.Log = append(st.Log, l)
		}
	}
	run.mu.Unlock()
	if st.State == "failed" && st.Step == "" {
		st.State = "refused"
	}
	return st
}

const moveToolDesc = "Move a card on your room to another room: its branch, its conversation and its files. The card " +
	"is frozen, carried, parked here and resumed there on the same conversation, and this card is then done. A " +
	"failure before the cut-over undoes it all.\n\n" +
	"- start: `card` (default your own) and `to`, the room. `wip` commits a dirty tree as WIP first, or a dirty " +
	"tree refuses the move. Answers a `move_id` at once.\n" +
	"- follow: `move_id` alone answers its `step`, its `state` (running, done, failed or refused) and the log."

type moveInput struct {
	Card   string `json:"card,omitempty" jsonschema:"a card id on this room. default your own card"`
	To     string `json:"to,omitempty" jsonschema:"the room to move it to"`
	Wip    bool   `json:"wip,omitempty" jsonschema:"commit a dirty tree as WIP instead of refusing"`
	MoveID string `json:"move_id,omitempty" jsonschema:"follow this move instead of starting one"`
}

func (c *controlMCP) moveHandler(ctx context.Context, req *mcp.CallToolRequest, in moveInput) (
	*mcp.CallToolResult, moveStatus, error) {

	room, me := ctl.RoomOf(req), ctl.AgentOf(req)
	if room == "" {
		return nil, moveStatus{}, fmt.Errorf("there is no room calling this to move")
	}
	if id := strings.TrimSpace(in.MoveID); id != "" {
		st, ok := c.moveStatusOf(room, id)
		if !ok {
			return nil, st, fmt.Errorf("no move %s from %s", id, room)
		}
		return nil, st, nil
	}
	card := strings.TrimSpace(in.Card)
	if card == "" {
		if me == "" {
			return nil, moveStatus{}, fmt.Errorf("say the card to move")
		}
		card = me
	}
	_, id, _, err := c.resolveCardStrict(ctx, room, card)
	if err != nil {
		return nil, moveStatus{}, err
	}
	st, err := c.moveStart(room, id, in.To, in.Wip)
	return nil, st, err
}
