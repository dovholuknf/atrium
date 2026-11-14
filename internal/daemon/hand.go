package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"
)

// A session started by hand (`claude --resume <id>`, no task id) whose conversation a card on ANOTHER room holds.
//
// Its hooks reach this room, because that is the listener the machine's hooks point at. heldConversation answers for
// this room's own cards. Here the hub is asked, and the room that holds the conversation takes the start. From then on
// every hook of that agent is carried to that card and answered with the card's own answer, so the permission chain and
// every other rule run on the room that owns the card, unchanged.

// handHookPath is the board route a carried hook arrives on. It is the hub that calls it, over the link.
const handHookPath = "/v1/hand/hook"

// handFindWait bounds asking the hub who holds a conversation. A session start must not wait on a slow hub.
const handFindWait = 3 * time.Second

// handHold bounds a carried hook on the owning room. Under the relay's own wait, so the answer can still travel.
const handHold = 20 * time.Second

// handForward is where the hooks of one agent on this room go: the room and the card holding its conversation.
type handForward struct{ room, card string }

// handReply is what the owning room answers for a carried hook.
type handReply struct {
	Card   string `json:"card"`
	Room   string `json:"room,omitempty"`
	Status int    `json:"status"`
	CType  string `json:"ctype,omitempty"`
	Body   []byte `json:"body,omitempty"`
}

// handedOn wraps a hook handler so a session adopted by a card elsewhere is answered by that card's room.
func (d *Daemon) handedOn(path string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var raw []byte
		agent := r.URL.Query().Get("agent")
		if r.Method == http.MethodPost {
			raw, _ = io.ReadAll(io.LimitReader(r.Body, 1<<20))
			r.Body = io.NopCloser(bytes.NewReader(raw))
			var a struct {
				Agent  string `json:"agent"`
				TaskID string `json:"task_id"`
			}
			if json.Unmarshal(raw, &a) == nil {
				agent = a.Agent
				// A launched session has its own card here, whatever an older hand session of the name left behind.
				if a.TaskID != "" {
					d.handed.Delete(agent)
				}
			}
		}
		if agent == "" {
			h(w, r)
			return
		}
		if f, ok := d.handed.Load(agent); ok {
			if d.carryHook(w, agent, f.(handForward), path, raw) {
				return
			}
		} else if path == "/session" {
			if f, rep := d.findHeldElsewhere(r.Context(), agent, raw); f != nil {
				d.handed.Store(agent, *f)
				d.writeHandReply(w, path, rep)
				return
			}
		}
		h(w, r)
	}
}

// findHeldElsewhere asks the hub for the card another room holds for this session's conversation. Only an unbound start
// of a conversation no card here holds is asked about, and a failure is nil: the session then gets a card here as it
// always did.
func (d *Daemon) findHeldElsewhere(ctx context.Context, agent string, raw []byte) (*handForward, *handReply) {
	var in SessionEvent
	if json.Unmarshal(raw, &in) != nil || in.TaskID != "" || in.Resume == "" || (in.Event != "" && in.Event != "start") {
		return nil, nil
	}
	if _, err := d.st.GetByResumeID(in.Resume); err == nil {
		return nil, nil
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), handFindWait)
	defer cancel()
	code, data, _ := d.HandCall(cctx, "", "", "/session", raw)
	if code != http.StatusOK {
		return nil, nil
	}
	var rep handReply
	if json.Unmarshal(data, &rep) != nil || rep.Room == "" || rep.Card == "" {
		return nil, nil
	}
	log.Printf("[atrium] a session started outside atrium resumed %s, held by card %s on %s: its hooks go there",
		logLine(in.Resume), logLine(rep.Card), logLine(rep.Room))
	return &handForward{room: rep.Room, card: rep.Card}, &rep
}

// carryHook sends one hook to the owning room and writes its answer. False means it was not carried, and the caller
// answers it here: the card is gone or the room did not take it.
func (d *Daemon) carryHook(w http.ResponseWriter, agent string, f handForward, path string, raw []byte) bool {
	if path == "/gate" {
		raw, _ = json.Marshal(map[string]string{"agent": agent})
	}
	if path == "/activity" {
		// The hot path of every tool call: acknowledged first and carried off it, as handleActivity does.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, _, _ = d.HandCall(ctx, f.room, f.card, path, raw)
		}()
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), handHold+2*time.Second)
	defer cancel()
	code, data, msg := d.HandCall(ctx, f.room, f.card, path, raw)
	var rep handReply
	if code != http.StatusOK || json.Unmarshal(data, &rep) != nil {
		if code == http.StatusNotFound {
			d.handed.Delete(agent)
			return false
		}
		log.Printf("[atrium] a hook %s for %s was not carried to %s: %s", logLine(path), logLine(agent), logLine(f.room), logLine(msg))
		d.writeHandReply(w, path, nil)
		return true
	}
	if path == "/session" {
		var ev SessionEvent
		if json.Unmarshal(raw, &ev) == nil && ev.Event == "end" && EndsTheSession(ev.Reason) {
			d.handed.Delete(agent)
		}
	}
	d.writeHandReply(w, path, &rep)
	return true
}

// writeHandReply answers the hook with the owning room's answer. With none, the answer a hook gets from a room that
// could not help: nothing gated for a permission, and ok for the rest, so a session never fails over this.
func (d *Daemon) writeHandReply(w http.ResponseWriter, path string, rep *handReply) {
	if rep == nil || rep.Status == 0 || (path == "/permission" && len(rep.Body) == 0) {
		if path == "/permission" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if path == "/gate" {
			_, _ = w.Write([]byte(`{"gate":false}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
		return
	}
	if rep.CType != "" {
		w.Header().Set("Content-Type", rep.CType)
	}
	w.WriteHeader(rep.Status)
	_, _ = w.Write(rep.Body)
}

// boardWithHand is the board handler with the route a carried hook arrives on beside it.
func (d *Daemon) boardWithHand() http.Handler {
	board := d.ap.Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == handHookPath {
			d.handHook(w, r)
			return
		}
		board.ServeHTTP(w, r)
	})
}

// handHook runs a carried hook on the card this room holds. A hook with no card is a session start looking for the
// card holding its conversation, and answers 404 when this room does not hold it.
func (d *Daemon) handHook(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path string          `json:"path"`
		Card string          `json:"card"`
		Body json.RawMessage `json:"body"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	handlers := map[string]http.HandlerFunc{"/session": d.handleSession, "/activity": d.handleActivity,
		"/permission": d.handlePermission, "/stop": d.handleStop, "/finish": d.handleFinish, "/gate": d.handleGate}
	h := handlers[in.Path]
	m := map[string]any{}
	if h == nil || json.Unmarshal(in.Body, &m) != nil {
		writeJSONError(w, http.StatusBadRequest, "not a hook this room carries")
		return
	}
	var ev SessionEvent
	_ = json.Unmarshal(in.Body, &ev)
	card := in.Card
	if card == "" {
		held := d.heldConversation(ev)
		if in.Path != "/session" || ev.TaskID != "" || (ev.Event != "" && ev.Event != "start") || held == nil {
			writeJSONError(w, http.StatusNotFound, "no card on this room holds that conversation")
			return
		}
		card = held.ID
	}
	t, err := d.st.Get(card)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "no card "+card+" on this room")
		return
	}
	m["agent"] = t.WireName
	delete(m, "name_source")
	if in.Path == "/session" {
		m["task_id"] = t.ID
	}
	body, _ := json.Marshal(m)
	ctx, cancel := context.WithTimeout(r.Context(), handHold)
	defer cancel()
	var req *http.Request
	if in.Path == "/gate" {
		req = httptest.NewRequest(http.MethodGet, "/gate?agent="+url.QueryEscape(t.WireName), nil)
	} else {
		req = httptest.NewRequest(http.MethodPost, in.Path, bytes.NewReader(body))
	}
	rec := httptest.NewRecorder()
	h(rec, req.WithContext(ctx))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(handReply{Card: t.ID, Status: rec.Code, CType: rec.Header().Get("Content-Type"),
		Body: rec.Body.Bytes()})
}

func writeJSONError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	b, _ := json.Marshal(map[string]string{"error": strings.TrimSpace(msg)})
	_, _ = w.Write(b)
}
