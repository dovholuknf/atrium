package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The leaf's half of a permission request crossing machines.
//
// The design point every test here defends is that THE CHANNEL DOES NOT MOVE.
// `HandlePermission` blocks on a channel in this machine's daemon, so a
// decision made on somebody else's board has to arrive here and be posted to
// that daemon. What breaks is the two ends of the wire disagreeing: about which
// clock the wait was measured on, about how big a report may be, or about which
// endpoint releases the agent.

// A local daemon that answers `/v1/permissions` with whatever it is given, and
// records what was posted to a decide endpoint.
type fakeLocal struct {
	perms []map[string]any
	// decided is the body of the last decision posted, and forWhich is the id
	// it was posted against.
	decided  map[string]any
	forWhich string
	status   int
}

func (f *fakeLocal) start(t *testing.T) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/permissions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"permissions": f.perms})
	})
	mux.HandleFunc("POST /v1/permissions/{id}/decide", func(w http.ResponseWriter, r *http.Request) {
		f.forWhich = r.PathValue("id")
		body, _ := io.ReadAll(r.Body)
		f.decided = map[string]any{}
		_ = json.Unmarshal(body, &f.decided)
		if f.status != 0 {
			w.WriteHeader(f.status)
			_, _ = io.WriteString(w, `{"error":"already blocked by you"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

// THE WAIT IS SENT AS SECONDS, measured against the clock that recorded the
// request. A timestamp would be read on the hub against the hub's clock, and
// two machines that disagree by a minute would put a request on the board as
// frozen a minute before it was made.
func TestAReportedRequestCarriesSecondsNotATime(t *testing.T) {
	f := &fakeLocal{perms: []map[string]any{{
		"id": "p1", "task_id": "t1", "tool": "Bash", "command": "go build",
		"agent":        "a card",
		"requested_at": time.Now().Add(-4 * time.Minute).UTC().Format(time.RFC3339),
	}}}
	got, err := localPerms(context.Background(), f.start(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("%d requests reported", len(got))
	}
	if got[0].Waiting < 235 || got[0].Waiting > 245 {
		t.Fatalf("four minutes of waiting was reported as %d seconds", got[0].Waiting)
	}
	if got[0].Agent != "a card" {
		t.Fatalf("the asking card's name did not travel: %+v", got[0])
	}
}

// A request the daemon reports with a timestamp this cannot read is still
// REPORTED, with a wait of zero. Dropping it would hide a frozen agent because
// of a formatting disagreement, which is the worst possible trade.
func TestARequestWithAnUnreadableTimeIsStillReported(t *testing.T) {
	f := &fakeLocal{perms: []map[string]any{{
		"id": "p1", "tool": "Bash", "command": "go build", "requested_at": "yesterday",
	}}}
	got, err := localPerms(context.Background(), f.start(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Waiting != 0 {
		t.Fatalf("wanted one request with no measured wait, got %+v", got)
	}
}

// A report is BOUNDED HERE. It has to fit in one POST, the hub reads at most a
// megabyte of it, and a report refused for being too big takes this machine's
// CARDS off the board with it. A diff that ends early is worth more than a
// machine that looks gone.
func TestAReportIsCutToFitBeforeItIsSent(t *testing.T) {
	f := &fakeLocal{}
	for i := 0; i < roomMaxPerms+10; i++ {
		f.perms = append(f.perms, map[string]any{
			"id": string(rune('a'+i%26)) + strings.Repeat("x", i), "tool": "Edit",
			"command": "main.go", "details": strings.Repeat("d", roomMaxDetails*3),
			"requested_at": time.Now().UTC().Format(time.RFC3339),
		})
	}
	got, err := localPerms(context.Background(), f.start(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != roomMaxPerms {
		t.Fatalf("%d requests were reported past a cap of %d", len(got), roomMaxPerms)
	}
	for _, p := range got {
		if len(p.Details) > roomMaxDetails {
			t.Fatalf("a diff of %d bytes was reported past a cap of %d",
				len(p.Details), roomMaxDetails)
		}
	}
}

// A diff is arbitrary text, so cutting it at a byte boundary can cut a
// character in half and put a replacement character on the wire.
func TestClipDoesNotCutACharacterInHalf(t *testing.T) {
	// Three bytes each, so a cut at 4 lands inside the second one.
	s := strings.Repeat("世", 3)
	got := clip(s, 4)
	if got != "世" {
		t.Fatalf("clip produced %q, which is not whole characters", got)
	}
	if clip("short", 100) != "short" {
		t.Fatal("clip changed a string that already fitted")
	}
}

// THE DECISION IS POSTED TO THIS MACHINE'S OWN DAEMON, which is the hop that
// releases the agent and the only one that can. The hub queued it and knows
// nothing about whether it worked.
func TestADecisionIsAppliedToTheLocalDaemon(t *testing.T) {
	f := &fakeLocal{}
	local := f.start(t)
	applyDecision(context.Background(), local, roomDecision{
		Perm: "p1", Decision: "block", Reason: "use a temp directory",
		Forever: true, Prefix: "rm ", Kind: "command",
	})
	if f.forWhich != "p1" {
		t.Fatalf("the decision was posted against %q", f.forWhich)
	}
	if f.decided["decision"] != "block" || f.decided["reason"] != "use a temp directory" {
		t.Fatalf("the decision did not arrive intact: %+v", f.decided)
	}
	// Forever and its scope travel, because the rule belongs on THIS machine:
	// this is the daemon that will be asked again, and the only one whose rule
	// table is consulted when it is.
	if f.decided["forever"] != true || f.decided["prefix"] != "rm " {
		t.Fatalf("a standing answer did not travel: %+v", f.decided)
	}
}

// A decision the local daemon refuses is NOT RETRIED and is not fatal. The
// interesting refusal is the correct one: somebody answered the same request on
// this machine's own board first. Retrying would race the hub, which offers the
// request again on its own once its decision expires.
func TestARefusedDecisionIsNotFatal(t *testing.T) {
	f := &fakeLocal{status: http.StatusConflict}
	applyDecision(context.Background(), f.start(t), roomDecision{
		Perm: "p1", Decision: "approve",
	})
	if f.forWhich != "p1" {
		t.Fatal("the decision was never posted")
	}
}

// An empty decision is ignored rather than posted. A POST to
// `/v1/permissions//decide` is a different route, and finding out what it does
// is not the way to handle a hub that sent nonsense.
func TestAnEmptyDecisionIsIgnored(t *testing.T) {
	f := &fakeLocal{}
	local := f.start(t)
	applyDecision(context.Background(), local, roomDecision{Decision: "approve"})
	applyDecision(context.Background(), local, roomDecision{Perm: "p1"})
	if f.forWhich != "" {
		t.Fatalf("a decision with nothing in it was posted against %q", f.forWhich)
	}
}

// The whole round trip: the report carries the pending request, and the answer
// to it carries the decision, which lands on this machine's own decide
// endpoint. One POST both ways, because a second connection would be a second
// thing to keep working over an overlay.
func TestACheckInReportsRequestsAndCollectsDecisions(t *testing.T) {
	f := &fakeLocal{perms: []map[string]any{{
		"id": "p1", "tool": "Bash", "command": "go build",
		"requested_at": time.Now().Add(-time.Minute).UTC().Format(time.RFC3339),
	}}}
	local := f.start(t)

	var reported struct {
		Name  string     `json:"name"`
		Perms []roomPerm `json:"permissions"`
	}
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &reported)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true,"decisions":[`+
			`{"permission_id":"p1","decision":"approve"}]}`)
	}))
	defer hub.Close()

	res, err := checkIn(context.Background(), hub.Client(), hub.URL, local,
		roomOpts{Name: "leaf"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.perms != 1 {
		t.Fatalf("the check-in counted %d pending request(s)", res.perms)
	}
	if len(reported.Perms) != 1 || reported.Perms[0].ID != "p1" {
		t.Fatalf("the request was not reported to the hub: %+v", reported)
	}
	if f.forWhich != "p1" || f.decided["decision"] != "approve" {
		t.Fatalf("the hub's decision never reached this machine's daemon: %q %+v",
			f.forWhich, f.decided)
	}
}

// A hub whose answer cannot be read is a hub NO DECISION CAN ARRIVE FROM, so
// the check-in reports a failure and the caller backs off. The report itself
// landed, so backing off costs nothing: the cards are already on that board.
func TestAnUnreadableAnswerIsAFailedCheckIn(t *testing.T) {
	f := &fakeLocal{}
	local := f.start(t)
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "not json")
	}))
	defer hub.Close()

	if _, err := checkIn(context.Background(), hub.Client(), hub.URL, local,
		roomOpts{Name: "leaf"}, true); err == nil {
		t.Fatal("a hub that answered gibberish was treated as a successful check-in")
	}
}

// A room whose own daemon is down still REPORTS. That daemon being unreachable
// is exactly what the hub should be showing, and going silent makes it look
// like the machine is gone instead.
func TestARoomWithNoLocalDaemonStillReports(t *testing.T) {
	var reached bool
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer hub.Close()

	// A port nothing is listening on.
	res, err := checkIn(context.Background(), hub.Client(), hub.URL,
		"http://127.0.0.1:1", roomOpts{Name: "leaf"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reached {
		t.Fatal("a room whose own daemon is down did not check in at all")
	}
	if res.cards != 0 || res.perms != 0 {
		t.Fatalf("it reported something it could not have read: %+v", res)
	}
}
