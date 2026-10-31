package link

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/base/edge"
	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link/wire"
)

// ITEMS, ON THE HUB. A room pushes the revisions of its items here and an owner acts on one here. See
// internal/hubstore/item.go for the rows and docs/changes/items-stage2a.md for the design.
//
//	GET  /_hub/items?room=&owner_kind=&owner=&state=   the items, newest change first
//	GET  /_hub/items/<id>                              one, with `holder` {room, card} when its owner is a role that is held,
//	                                                   and `history` [{revision, state, at}] oldest first
//	POST /_hub/items/sync                              a room's push of one revision: the row as the hub's store takes it.
//	                                                   {applied:false} for a replay or a push older than the row
//	POST /_hub/items                                   {do: submit, room, pool, brief, owner?, by}: queue an item on a pool.
//	                                                   Carried to the pool's room, which types it to an idle card, one item
//	                                                   at a time per card. The hub sees the item when a card takes it
//	POST /_hub/items/<id>                              {do: unblock|cancel, by, answer?}: an owner acts. Authorised here,
//	                                                   then carried to the room that runs the item
//
// WHO MAY WRITE is what the backlog allows: the machine the hub runs on. A room reaches `sync` and the owner's actions
// through the `hub` relay (hubask.go), which stamps `room` and `by` from the link, so a room cannot push for another
// or act as another.
//
// THE HUB DOES NOT MOVE AN ITEM WHEN AN OWNER ACTS. unblock is typed by the room, and the item is `running` again
// only when the worker's turn starts from it, which the room pushes as the next revision.
//
// THE OWNER CHECK IS A CLAIM, like every control tool's: `by` is a name and not a proof. A human owner has no
// atrium_item, so the board's items tab acts for them as `by: board`, from the hub's own machine (see ownerMayAct).

// itemRoomCallFunc is how the hub asks a room, a function so a test need not attach one.
type itemRoomCallFunc func(ctx context.Context, room, path string, body []byte) (int, []byte, error)

const itemRoomWait = 30 * time.Second

// SetItems wires the store the items live in. Without it /_hub/items answers 404.
func (p *Proxy) SetItems(st *hubstore.Store) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.items = st
}

func (p *Proxy) itemStore() *hubstore.Store {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.items
}

// postItemRoom is itemRoomCall for a real room.
func (p *Proxy) postItemRoom(ctx context.Context, room, path string, body []byte) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, itemRoomWait)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+hostFor(room)+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := p.roomClient(room).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 1<<16))
	return res.StatusCode, b, err
}

func (p *Proxy) callItemRoom(ctx context.Context, room, path string, body []byte) (int, []byte, error) {
	p.mu.Lock()
	f := p.itemRoomCall
	p.mu.Unlock()
	if f != nil {
		return f(ctx, room, path, body)
	}
	return p.postItemRoom(ctx, room, path, body)
}

func (p *Proxy) serveItems(w http.ResponseWriter, r *http.Request, sub string) {
	st := p.itemStore()
	if st == nil {
		http.NotFound(w, r)
		return
	}
	rest := strings.Trim(strings.TrimPrefix(sub, "items"), "/")
	write := r.Method != http.MethodGet && r.Method != http.MethodHead
	if write {
		switch {
		case r.Method != http.MethodPost:
			crFail(w, http.StatusMethodNotAllowed, "that has to be a GET or a POST")
			return
		case wire.DocsCrossOrigin.Check(r) != nil:
			crFail(w, http.StatusForbidden, "a page on another origin cannot write here")
			return
		case !edge.LocalOperator(r):
			crFail(w, http.StatusForbidden, "items are written only from the machine the hub runs on"+edge.ProxyNote(r))
			return
		}
	}
	switch {
	case rest == "" && !write:
		p.itemsList(w, r, st)
	case rest == "" && write:
		p.itemSubmit(w, r)
	case rest == "sync" && write:
		p.itemSync(w, r, st)
	case rest == "tasks/sync" && write:
		p.taskSync(w, r, st)
	case strings.HasSuffix(rest, "/tasks") && !strings.Contains(strings.TrimSuffix(rest, "/tasks"), "/"):
		p.serveTasks(w, r, st, strings.TrimSuffix(rest, "/tasks"), write)
	case rest != "" && !strings.Contains(rest, "/") && !write:
		it, err := st.ItemByID(rest)
		if err != nil {
			ifail(w, err)
			return
		}
		out := map[string]any{"item": it}
		if room, card, ok, err := st.ItemHolder(it); err == nil && ok {
			out["holder"] = map[string]string{"room": room, "card": card}
		}
		// Every revision the hub applied, for the board's timeline.
		hist, _ := st.ItemHistory(it.ID)
		if hist == nil {
			hist = []hubstore.ItemEvent{}
		}
		out["history"] = hist
		// The item's own fields at the top too, so a reader of one item and of a list read the same names.
		flat, _ := json.Marshal(it)
		var m map[string]any
		_ = json.Unmarshal(flat, &m)
		for k, v := range out {
			m[k] = v
		}
		crJSON(w, http.StatusOK, m)
	case rest != "" && !strings.Contains(rest, "/") && write:
		p.itemAct(w, r, st, rest)
	default:
		http.NotFound(w, r)
	}
}

func ifail(w http.ResponseWriter, err error) {
	code := http.StatusServiceUnavailable
	switch {
	case errors.Is(err, hubstore.ErrNoItem):
		code = http.StatusNotFound
	case hubstore.IsRefusal(err):
		code = http.StatusBadRequest
	}
	crFail(w, code, err.Error())
}

func (p *Proxy) itemsList(w http.ResponseWriter, r *http.Request, st *hubstore.Store) {
	q := r.URL.Query()
	f := hubstore.ItemQuery{Room: q.Get("room"), OwnerKind: q.Get("owner_kind"), Owner: q.Get("owner")}
	if s := strings.TrimSpace(q.Get("state")); s != "" {
		f.States = strings.Split(s, ",")
	}
	items, err := st.Items(f)
	if err != nil {
		ifail(w, err)
		return
	}
	if items == nil {
		items = []hubstore.Item{}
	}
	crJSON(w, http.StatusOK, map[string]any{"items": items})
}

// qualifyOwner gives a session handle its room when it has none, so an owner is `handle@room` wherever it is read.
func qualifyOwner(handle, room string) string {
	handle = strings.TrimSpace(handle)
	if handle == "" || strings.Contains(handle, "@") {
		return handle
	}
	return handle + "@" + room
}

func (p *Proxy) itemSync(w http.ResponseWriter, r *http.Request, st *hubstore.Store) {
	// NOT STRICT: the relay adds `by`, and a newer room may send a field this hub does not know yet.
	var it hubstore.Item
	if err := json.NewDecoder(io.LimitReader(r.Body, crBody)).Decode(&it); err != nil {
		crFail(w, http.StatusBadRequest, "could not read that: "+err.Error())
		return
	}
	if !validRoomName(it.Room) {
		crFail(w, http.StatusBadRequest, "room is not a room name")
		return
	}
	if it.OwnerKind == hubstore.OwnerSession {
		it.Owner = qualifyOwner(it.Owner, it.Room)
	}
	it.CreatedBy = qualifyOwner(it.CreatedBy, it.Room)
	got, applied, err := st.PutItem(it)
	if err != nil {
		ifail(w, err)
		return
	}
	if applied {
		p.backlogEvent("item", map[string]string{"id": got.ID, "state": got.State, "owner_kind": got.OwnerKind})
		// THE ITEM'S STEPS FOLLOW ITS ASSIGNMENTS. Off this request's path: reading git may take seconds, and the room waits.
		p.refreshWorkSoon(st, got.WorkID)
		if got.WorkID != "" {
			_, _ = st.TaskCardSeen(got.WorkID, got.Room, got.Card, got.CreatedAt)
		}
	}
	crJSON(w, http.StatusOK, map[string]any{"ok": true, "applied": applied, "item": got})
}

// ownerMayAct is whether `by` is the owner of the item, and the sentence for a no.
func (p *Proxy) ownerMayAct(ctx context.Context, st *hubstore.Store, it hubstore.Item, by string) (bool, string) {
	by = strings.TrimSpace(by)
	// THE BOARD IS THE OPERATOR. Every write here already passed edge.LocalOperator, so `board` is the human at the hub's
	// machine and is the owner of whatever it is shown, the human-owned items included. The items tab sends it.
	if strings.EqualFold(by, "board") {
		return true, ""
	}
	switch it.OwnerKind {
	case hubstore.OwnerHuman:
		return false, "that item is owned by the human, who has no atrium_item. they answer it in the worker's terminal"
	case hubstore.OwnerSession:
		if by != "" && strings.EqualFold(qualifyOwner(by, it.Room), it.Owner) {
			return true, ""
		}
		return false, "only the owner of an item acts on it"
	case hubstore.OwnerRole:
		room, card, ok, err := st.ItemHolder(it)
		if err != nil || !ok {
			return false, "nobody holds the " + it.Owner + " role right now, so nobody owns that item"
		}
		name, byRoom, _ := strings.Cut(by, "@")
		if name == "" || !strings.EqualFold(byRoom, room) {
			return false, "only the owner of an item acts on it"
		}
		// The holder is a card on its room, and its handle is what the caller claims to be.
		if p.cardHasHandle(ctx, room, card, name) {
			return true, ""
		}
		return false, "only the owner of an item acts on it"
	}
	return false, "only the owner of an item acts on it"
}

// cardHasHandle asks a room whether a card goes by a name. A wire name carries its atrium's prefix, so the bare
// handle is a suffix of it.
func (p *Proxy) cardHasHandle(ctx context.Context, room, card, handle string) bool {
	ctx, cancel := context.WithTimeout(ctx, itemRoomWait)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+hostFor(room)+"/v1/tasks/"+card, nil)
	if err != nil {
		return false
	}
	res, err := p.roomClient(room).Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	var t struct {
		WireName string `json:"wire_name"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&t) != nil || t.WireName == "" {
		return false
	}
	w, h := strings.ToLower(t.WireName), strings.ToLower(strings.TrimSpace(handle))
	return w == h || strings.HasSuffix(w, "-"+h)
}

func (p *Proxy) itemAct(w http.ResponseWriter, r *http.Request, st *hubstore.Store, id string) {
	var in struct {
		Do     string `json:"do"`
		By     string `json:"by"`
		Answer string `json:"answer"`
	}
	if err := crDecode(r, &in); err != nil {
		crFail(w, http.StatusBadRequest, "could not read that: "+err.Error())
		return
	}
	it, err := st.ItemByID(id)
	if err != nil {
		ifail(w, err)
		return
	}
	do := strings.ToLower(strings.TrimSpace(in.Do))
	if do != "unblock" && do != "cancel" {
		crFail(w, http.StatusBadRequest, "do is unblock or cancel")
		return
	}
	if ok, why := p.ownerMayAct(r.Context(), st, it, in.By); !ok {
		crFail(w, http.StatusForbidden, why)
		return
	}
	body := map[string]string{"by": strings.TrimSpace(in.By)}
	if do == "unblock" {
		answer := strings.TrimSpace(in.Answer)
		switch {
		case it.State != hubstore.ItemStateBlocked:
			crFail(w, http.StatusConflict, "that item is "+it.State+", not blocked, so there is nothing to unblock")
			return
		case answer == "":
			crFail(w, http.StatusBadRequest, "unblock needs the answer to type to the worker")
			return
		case len(answer) > hubstore.ItemNoteMax:
			crFail(w, http.StatusBadRequest, "that answer is too long")
			return
		}
		body["answer"] = answer
	} else if it.State == hubstore.ItemStateFinished {
		crFail(w, http.StatusConflict, "that item is finished")
		return
	}
	raw, _ := json.Marshal(body)
	code, resp, err := p.callItemRoom(r.Context(), it.Room, "/v1/items/"+it.ID+"/"+do, raw)
	if err != nil {
		crFail(w, http.StatusBadGateway, "the room that runs that item is not answering: "+err.Error())
		return
	}
	p.RecordAudit(it.Room, "item-"+do, it.ID)
	w.WriteHeader(code)
	_, _ = w.Write(resp)
}

// itemSubmit queues an item on a pool, on the room the pool is on. The submitter is the owner unless it names another,
// and has to be a session on that room: an owner is told by its own room, and a pool lives on one.
func (p *Proxy) itemSubmit(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Do    string `json:"do"`
		Room  string `json:"room"`
		Pool  string `json:"pool"`
		Brief string `json:"brief"`
		Owner string `json:"owner"`
		By    string `json:"by"`
	}
	if err := crDecode(r, &in); err != nil {
		crFail(w, http.StatusBadRequest, "could not read that: "+err.Error())
		return
	}
	by, byRoom, _ := strings.Cut(strings.TrimSpace(in.By), "@")
	room := strings.TrimSpace(in.Room)
	if room == "" {
		room = byRoom
	}
	switch {
	case strings.ToLower(strings.TrimSpace(in.Do)) != "submit":
		crFail(w, http.StatusBadRequest, "do is submit")
		return
	case !validRoomName(room):
		crFail(w, http.StatusBadRequest, "say which room the pool is on")
		return
	case strings.TrimSpace(in.Pool) == "" || strings.TrimSpace(in.Brief) == "":
		crFail(w, http.StatusBadRequest, "submit needs the pool and the brief")
		return
	case by != "" && byRoom != "" && !strings.EqualFold(byRoom, room):
		crFail(w, http.StatusBadRequest, "a pool's items are owned on the pool's room, and "+by+" is on "+byRoom+
			". name `human` or a role as the owner")
		return
	}
	body, _ := json.Marshal(map[string]string{"brief": in.Brief, "owner": in.Owner, "by": by})
	code, resp, err := p.callItemRoom(r.Context(), room, "/v1/pools/"+url.PathEscape(strings.TrimSpace(in.Pool))+"/items", body)
	if err != nil {
		crFail(w, http.StatusBadGateway, "the room that runs that pool is not answering: "+err.Error())
		return
	}
	p.RecordAudit(room, "item-submit", strings.TrimSpace(in.Pool))
	w.WriteHeader(code)
	_, _ = w.Write(resp)
}
