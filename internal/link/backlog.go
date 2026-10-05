package link

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/dovholuknf/atrium/internal/edge"
	"github.com/dovholuknf/atrium/internal/hubstore"
)

// The hub's backlog and its director reports, which every room reads and writes through the hub. See
// internal/hubstore/backlog.go for the rows and docs/rnd/reports-channel-design.md for why they are here.
//
//	GET  /_hub/backlog?dept=&status=&open=1   the items, filtered, without their bodies
//	POST /_hub/backlog                        file one {id?, dept, title, body?, priority?, by?, room?}. No id takes
//	                                          the next `<prefix>-<n>` of the department. {upsert:true, status?} makes
//	                                          the row for the id match a file (import), changing only what differs
//	GET  /_hub/backlog/<id>                   one, with its body
//	POST /_hub/backlog/<id>                   {status, by?}, or {card, by?} to link the card launched for it
//	POST /_hub/backlog/follow                 {card, status, by?}: a card's report moves the item linked to it
//	GET  /_hub/reports?to=&unread=1&limit=    the reports, newest first
//	POST /_hub/reports                        leave one {to?, subject, body?, by?, room?}
//	GET  /_hub/reports/<id>                   one
//	POST /_hub/reports/<id>                   {do:"read", by?}
//
// WHO MAY WRITE is what the deps routes allow: the cross-origin check, and the machine the hub runs on. A room's control
// server asks from there, and says who it speaks for in `by` and `room`, as `atrium_deps` does. Reads are open to the
// board like the change request list. The words are data, bounded and free of control characters, and are never run.
//
// The `backlog` and `report` events go to every board when something is accepted.

// SetBacklog wires the backlog and the reports. Without it /_hub/backlog and /_hub/reports answer 404.
func (p *Proxy) SetBacklog(st *hubstore.Store) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.backlog = st
}

func (p *Proxy) backlogStore() *hubstore.Store {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.backlog
}

var (
	backlogIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,79}$`)
	reportIDRe  = regexp.MustCompile(`^rp_[1-9][0-9]{0,17}$`)
)

func (p *Proxy) serveBacklog(w http.ResponseWriter, r *http.Request, sub string) {
	st := p.backlogStore()
	if st == nil {
		http.NotFound(w, r)
		return
	}
	isReports := sub == "reports" || strings.HasPrefix(sub, "reports/")
	rest := strings.Trim(strings.TrimPrefix(strings.TrimPrefix(sub, "backlog"), "reports"), "/")
	write := r.Method != http.MethodGet && r.Method != http.MethodHead
	if write {
		switch {
		case r.Method != http.MethodPost:
			crFail(w, http.StatusMethodNotAllowed, "that has to be a GET or a POST")
			return
		case docsCrossOrigin.Check(r) != nil:
			crFail(w, http.StatusForbidden, "a page on another origin cannot write here")
			return
		case !edge.LocalOperator(r):
			crFail(w, http.StatusForbidden, "the backlog and reports are written only from the machine the hub runs on"+
				edge.ProxyNote(r))
			return
		}
	}
	switch {
	case isReports && rest == "":
		if write {
			p.reportAdd(w, r, st)
		} else {
			p.reportList(w, r, st)
		}
	case isReports && reportIDRe.MatchString(rest):
		if write {
			p.reportDo(w, r, st, rest)
			return
		}
		rep, err := st.ReportGet(rest)
		if err != nil {
			bfail(w, err)
			return
		}
		crJSON(w, http.StatusOK, rep)
	case !isReports && rest == "":
		if write {
			p.itemFile(w, r, st)
		} else {
			p.itemList(w, r, st)
		}
	case !isReports && write && rest == "follow":
		p.itemFollow(w, r, st)
	case !isReports && backlogIDRe.MatchString(rest):
		if write {
			p.itemDo(w, r, st, rest)
			return
		}
		b, err := st.ItemGet(rest)
		if err != nil {
			bfail(w, err)
			return
		}
		crJSON(w, http.StatusOK, b)
	default:
		http.NotFound(w, r)
	}
}

// bfail answers a store error: a refusal is the caller's to fix, anything else is the hub's.
func bfail(w http.ResponseWriter, err error) {
	code := http.StatusServiceUnavailable
	switch {
	case errors.Is(err, hubstore.ErrItemNotFound), errors.Is(err, hubstore.ErrReportNotFound):
		code = http.StatusNotFound
	case hubstore.IsRefusal(err):
		code = http.StatusBadRequest
	}
	crFail(w, code, err.Error())
}

func (p *Proxy) itemList(w http.ResponseWriter, r *http.Request, st *hubstore.Store) {
	q := r.URL.Query()
	f := hubstore.ItemFilter{Dept: q.Get("dept"), Open: q.Get("open") == "1"}
	if s := q.Get("status"); s != "" {
		if !hubstore.ValidItemStatus(s) {
			crFail(w, http.StatusBadRequest, "status is open, held, in-progress, built, blocked, incomplete, done or dropped")
			return
		}
		f.Status = s
	}
	items, err := st.ItemList(f)
	if err != nil {
		bfail(w, err)
		return
	}
	// THE BODY IS LEFT OUT OF A LIST. A list is for finding an item, and GET /_hub/backlog/<id> reads one whole.
	for i := range items {
		items[i].Body = ""
	}
	crJSON(w, http.StatusOK, map[string]any{"items": items})
}

type itemFileBody struct {
	ID       string `json:"id"`
	Dept     string `json:"dept"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	Priority string `json:"priority"`
	By       string `json:"by"`
	Room     string `json:"room"`
	Upsert   bool   `json:"upsert"`
	Status   string `json:"status"`
}

func (p *Proxy) itemFile(w http.ResponseWriter, r *http.Request, st *hubstore.Store) {
	var in itemFileBody
	if err := crDecode(r, &in); err != nil {
		crFail(w, http.StatusBadRequest, "could not read that: "+err.Error())
		return
	}
	if in.Room != "" && !validRoomName(in.Room) {
		crFail(w, http.StatusBadRequest, "room is not a room name")
		return
	}
	n := hubstore.ItemNew{ID: in.ID, Dept: in.Dept, Title: in.Title, Body: in.Body,
		Priority: in.Priority, FiledBy: in.By, FiledRoom: in.Room}
	if in.Upsert {
		b, how, err := st.ItemUpsert(n, in.Status)
		if err != nil {
			bfail(w, err)
			return
		}
		if how != "unchanged" {
			p.RecordAudit(b.FiledRoom, "backlog-import", b.ID+" "+how)
			p.backlogEvent("backlog", map[string]string{"id": b.ID, "dept": b.Dept, "status": b.Status, "title": b.Title})
		}
		crJSON(w, http.StatusOK, map[string]any{"item": b, "result": how})
		return
	}
	if in.Status != "" {
		crFail(w, http.StatusBadRequest, "a new item is open. status is for an import")
		return
	}
	b, err := st.ItemFile(n)
	if errors.Is(err, hubstore.ErrItemExists) {
		crJSON(w, http.StatusConflict, b)
		return
	}
	if err != nil {
		bfail(w, err)
		return
	}
	p.RecordAudit(b.FiledRoom, "backlog-file", b.ID)
	p.backlogEvent("backlog", map[string]string{"id": b.ID, "dept": b.Dept, "status": b.Status, "title": b.Title})
	crJSON(w, http.StatusCreated, b)
}

func (p *Proxy) itemDo(w http.ResponseWriter, r *http.Request, st *hubstore.Store, id string) {
	var in struct {
		Status string `json:"status"`
		Card   string `json:"card"`
		By     string `json:"by"`
	}
	if err := crDecode(r, &in); err != nil {
		crFail(w, http.StatusBadRequest, "could not read that: "+err.Error())
		return
	}
	var b hubstore.BacklogItem
	var err error
	if in.Card != "" {
		b, err = st.ItemLink(id, in.Card, in.By)
	} else {
		b, err = st.ItemSetStatus(id, in.Status, in.By)
	}
	if err != nil {
		bfail(w, err)
		return
	}
	p.RecordAudit("", "backlog-status", id+" "+b.Status)
	p.backlogEvent("backlog", map[string]string{"id": b.ID, "dept": b.Dept, "status": b.Status, "title": b.Title})
	crJSON(w, http.StatusOK, b)
}

// itemFollow is a card's report arriving: the item linked to that card takes the status the report brings. Most cards
// have no item, and that is a 200 with followed false and nothing recorded.
func (p *Proxy) itemFollow(w http.ResponseWriter, r *http.Request, st *hubstore.Store) {
	var in struct {
		Card   string `json:"card"`
		Status string `json:"status"`
		By     string `json:"by"`
	}
	if err := crDecode(r, &in); err != nil {
		crFail(w, http.StatusBadRequest, "could not read that: "+err.Error())
		return
	}
	b, followed, err := st.ItemFollow(in.Card, in.Status, in.By)
	if err != nil {
		bfail(w, err)
		return
	}
	if followed {
		p.RecordAudit("", "backlog-status", b.ID+" "+b.Status)
		p.backlogEvent("backlog", map[string]string{"id": b.ID, "dept": b.Dept, "status": b.Status, "title": b.Title})
	}
	crJSON(w, http.StatusOK, map[string]any{"followed": followed, "item": b})
}

func (p *Proxy) reportList(w http.ResponseWriter, r *http.Request, st *hubstore.Store) {
	q := r.URL.Query()
	f := hubstore.ReportFilter{ToDept: q.Get("to"), Unread: q.Get("unread") == "1"}
	if l := q.Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n < 1 {
			crFail(w, http.StatusBadRequest, "limit is a positive number")
			return
		}
		f.Limit = n
	}
	reps, err := st.ReportList(f)
	if err != nil {
		bfail(w, err)
		return
	}
	crJSON(w, http.StatusOK, map[string]any{"reports": reps})
}

func (p *Proxy) reportAdd(w http.ResponseWriter, r *http.Request, st *hubstore.Store) {
	var in struct {
		To      string `json:"to"`
		Subject string `json:"subject"`
		Body    string `json:"body"`
		By      string `json:"by"`
		Room    string `json:"room"`
	}
	if err := crDecode(r, &in); err != nil {
		crFail(w, http.StatusBadRequest, "could not read that: "+err.Error())
		return
	}
	if in.Room != "" && !validRoomName(in.Room) {
		crFail(w, http.StatusBadRequest, "room is not a room name")
		return
	}
	rep, err := st.ReportAdd(hubstore.ReportNew{ToDept: in.To, FromBy: in.By, FromRoom: in.Room,
		Subject: in.Subject, Body: in.Body})
	if err != nil {
		bfail(w, err)
		return
	}
	p.RecordAudit(rep.FromRoom, "report-add", rep.ID)
	p.backlogEvent("report", map[string]string{"id": rep.ID, "to": rep.ToDept, "from": rep.FromBy, "subject": rep.Subject})
	crJSON(w, http.StatusCreated, rep)
}

func (p *Proxy) reportDo(w http.ResponseWriter, r *http.Request, st *hubstore.Store, id string) {
	var in struct {
		Do string `json:"do"`
		By string `json:"by"`
	}
	if err := crDecode(r, &in); err != nil {
		crFail(w, http.StatusBadRequest, "could not read that: "+err.Error())
		return
	}
	if in.Do != "read" {
		crFail(w, http.StatusBadRequest, `"do" is read`)
		return
	}
	rep, err := st.ReportRead(id, in.By)
	if err != nil {
		bfail(w, err)
		return
	}
	crJSON(w, http.StatusOK, rep)
}

func (p *Proxy) backlogEvent(kind string, v any) {
	if data, err := json.Marshal(v); err == nil {
		p.feeds.broadcast(Event{Kind: kind, Data: data})
	}
}
