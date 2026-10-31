//go:build integration

package link

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link/wire"
)

type eventRig struct {
	st    *hubstore.Store
	hook  *EventHook
	fake  *fakeCI
	clock *time.Time
	mu    sync.Mutex
	lines []string
}

func newEventRig(t *testing.T) *eventRig {
	t.Helper()
	st, err := hubstore.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	x := &eventRig{st: st, fake: &fakeCI{}}
	start := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	x.clock = &start
	now := func() time.Time { return *x.clock }
	st.SetClock(now)
	x.hook = NewEventHook(st, x.fake, func(kind, detail string) {
		x.mu.Lock()
		x.lines = append(x.lines, kind+": "+detail)
		x.mu.Unlock()
	})
	x.hook.Now = now
	x.hook.From = "land@testhub"
	return x
}

func (x *eventRig) audited(kind string) []string {
	x.mu.Lock()
	defer x.mu.Unlock()
	var out []string
	for _, l := range x.lines {
		if strings.HasPrefix(l, kind+":") {
			out = append(out, l)
		}
	}
	return out
}

func landBodyJSON(outcome, from, to string, commits int, reason string) string {
	b, _ := json.Marshal(map[string]any{"kind": "land", "repo": "o/r", "branch": "main", "outcome": outcome, "from": from,
		"to": to, "commits": commits, "reason": reason})
	return string(b)
}

func (x *eventRig) post(body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/_hub/events", strings.NewReader(body))
	w := httptest.NewRecorder()
	x.hook.Receive(w, r)
	return w
}

func (x *eventRig) subs(t *testing.T, body string) (int, string) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/_hub/events/subs", strings.NewReader(body))
	w := httptest.NewRecorder()
	x.hook.Subs(w, r)
	return w.Code, w.Body.String()
}

func (x *eventRig) subscribe(t *testing.T, card, repo, branch string, wake bool) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"action": "subscribe", "kind": "land", "repo": repo, "branch": branch, "card": card, "wake": wake})
	if c, b := x.subs(t, string(body)); c != http.StatusOK {
		t.Fatalf("subscribe %s: %d %s", card, c, b)
	}
}

func (x *eventRig) pendingFor(t *testing.T) int {
	t.Helper()
	rows, err := x.st.EventDeliveriesPending()
	if err != nil {
		t.Fatal(err)
	}
	return len(rows)
}

const landFrom, landTo = "3f1c0de0123456789abcdef0123456789abcdef", "9a0b1c20123456789abcdef0123456789abcdef"

func TestAGoodLandEventIs202WithTheSubscriberCount(t *testing.T) {
	x := newEventRig(t)
	x.subscribe(t, "sg4~s1", "o/r", "main", false)
	x.subscribe(t, "sg4~s2", "", "", false)
	w := x.post(landBodyJSON("pushed", landFrom, landTo, 14, ""))
	if w.Code != http.StatusAccepted {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
	var got struct {
		Event       string `json:"event"`
		Subscribers int    `json:"subscribers"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.Event == "" || got.Subscribers != 2 {
		t.Fatalf("answer %s", w.Body)
	}
	if lines := x.audited("event"); len(lines) != 1 || !strings.Contains(lines[0], "sg4~s1") {
		t.Fatalf("audit %v", lines)
	}
}

func TestABadLandEventIs422AndNothingIsRecorded(t *testing.T) {
	x := newEventRig(t)
	long := strings.Repeat("x", 201)
	for name, body := range map[string]string{
		"unknown kind":    `{"kind":"deploy","repo":"o/r","branch":"main","outcome":"pushed","to":"9a0b1c2"}`,
		"no kind":         `{}`,
		"not json":        `nope`,
		"bad sha":         landBodyJSON("pushed", "zzzzzzz", landTo, 1, ""),
		"short sha":       landBodyJSON("pushed", landFrom, "9a0b1", 1, ""),
		"bad repo":        `{"kind":"land","repo":"nope","branch":"main","outcome":"pushed","to":"9a0b1c2"}`,
		"bad branch":      `{"kind":"land","repo":"o/r","branch":"a..b","outcome":"pushed","to":"9a0b1c2"}`,
		"bad outcome":     landBodyJSON("landed", landFrom, landTo, 1, ""),
		"pushed no to":    landBodyJSON("pushed", landFrom, "", 1, ""),
		"long reason":     landBodyJSON("stopped", "", "", 0, long),
		"too many":        landBodyJSON("pushed", landFrom, landTo, 10001, ""),
		"unknown field":   `{"kind":"land","repo":"o/r","branch":"main","outcome":"stopped","extra":1}`,
		"negative number": landBodyJSON("pushed", landFrom, landTo, -1, ""),
	} {
		if w := x.post(body); w.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: got %d %s, want 422", name, w.Code, w.Body)
		}
	}
	if w := x.post(`{"kind":"nope"}`); !strings.Contains(w.Body.String(), "land") {
		t.Errorf("an unknown kind should name the kinds: %s", w.Body)
	}
	if rows, _ := x.st.EventSubsAll(); len(rows) != 0 {
		t.Fatal("a refused event left rows")
	}
}

func TestABodyOver64KiBIs413(t *testing.T) {
	x := newEventRig(t)
	body := landBodyJSON("stopped", "", "", 0, "") + strings.Repeat(" ", 65<<10)
	if w := x.post(body); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("got %d", w.Code)
	}
}

func TestEventsOverTheRateAre429WithRetryAfter(t *testing.T) {
	x := newEventRig(t)
	for i := 0; i < eventRateBurst; i++ {
		if w := x.post(landBodyJSON("stopped", "", fmt.Sprintf("%07x", i+1), i, "")); w.Code != http.StatusAccepted {
			t.Fatalf("event %d: %d", i, w.Code)
		}
	}
	w := x.post(landBodyJSON("stopped", "", "fffffff", 99, ""))
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatalf("over the burst: %d %v", w.Code, w.Header())
	}
	*x.clock = x.clock.Add(time.Minute)
	if w := x.post(landBodyJSON("stopped", "", "eeeeeee", 98, "")); w.Code != http.StatusAccepted {
		t.Fatalf("after a minute: %d", w.Code)
	}
}

func TestARepeatIs200DuplicateAndDeliversNothing(t *testing.T) {
	x := newEventRig(t)
	x.subscribe(t, "sg4~s1", "", "", false)
	body := landBodyJSON("pushed", landFrom, landTo, 3, "")
	if w := x.post(body); w.Code != http.StatusAccepted {
		t.Fatalf("first: %d", w.Code)
	}
	w := x.post(body)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"duplicate":true`) {
		t.Fatalf("repeat: %d %s", w.Code, w.Body)
	}
	x.hook.Pass(context.Background())
	if n := len(x.fake.sends()); n != 1 {
		t.Fatalf("%d sent, want 1", n)
	}
}

func TestEventsThroughAShareOrAProxyAre403(t *testing.T) {
	x := newEventRig(t)
	p := NewProxy(NewHub(Timings{}), nil, "", nil)
	p.SetEvents(x.hook)
	post := func(remote, host string, hdr map[string]string) int {
		r := httptest.NewRequest(http.MethodPost, "/_hub/events", strings.NewReader(landBodyJSON("stopped", "", "", 0, host+remote)))
		r.RemoteAddr, r.Host = remote, host
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		p.ServeHTTP(w, r)
		return w.Code
	}
	if c := post("127.0.0.1:5000", "127.0.0.1:7778", nil); c != http.StatusAccepted {
		t.Fatalf("local: %d", c)
	}
	if c := post("127.0.0.1:5000", "hub.example.com", nil); c != http.StatusForbidden {
		t.Fatalf("a proxied host: %d", c)
	}
	if c := post("127.0.0.1:5000", "127.0.0.1:7778", map[string]string{"X-Forwarded-For": "1.2.3.4"}); c != http.StatusForbidden {
		t.Fatalf("a forwarded request: %d", c)
	}
	if c := post("10.0.0.5:5000", "127.0.0.1:7778", nil); c != http.StatusForbidden {
		t.Fatalf("a remote caller: %d", c)
	}
	r := httptest.NewRequest(http.MethodPost, "/_hub/events/subs", strings.NewReader(`{"action":"list"}`))
	r.RemoteAddr, r.Host = "10.0.0.5:5000", "127.0.0.1:7778"
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("subscriptions from a remote caller: %d", w.Code)
	}
}

func TestEachOutcomesLineIsBuiltFromFields(t *testing.T) {
	for _, c := range []struct{ body, want string }{
		{landBodyJSON("pushed", landFrom, landTo, 14, "ignored"), "Landed: o/r main 3f1c0de..9a0b1c2, 14 commits. " + eventSignoff},
		{landBodyJSON("pushed", "", landTo, 1, ""), "Landed: o/r main 9a0b1c2, 1 commit. " + eventSignoff},
		{landBodyJSON("not-pushed", landFrom, landTo, 14, "origin moved"),
			"Land not pushed: o/r main, 14 commits ready at 9a0b1c2. origin moved " + eventSignoff},
		{landBodyJSON("stopped", "", "", 0, "dirty tree"), "Land stopped: o/r main. dirty tree " + eventSignoff},
		{landBodyJSON("stopped", "", "", 0, "a\x1b[31mb\nc\td"), "Land stopped: o/r main. a [31mb c d " + eventSignoff},
	} {
		_, line, err := parseLand([]byte(c.body))
		if err != nil || line != c.want {
			t.Errorf("got %q (%v), want %q", line, err, c.want)
		}
	}
}

func TestSubscribingTwiceIsOneRowAndAnswersAlready(t *testing.T) {
	x := newEventRig(t)
	body := `{"action":"subscribe","kind":"land","repo":"o/r","branch":"main","card":"sg4~s1"}`
	if c, b := x.subs(t, body); c != http.StatusOK || !strings.Contains(b, `"already":false`) {
		t.Fatalf("first: %d %s", c, b)
	}
	if c, b := x.subs(t, body); c != http.StatusOK || !strings.Contains(b, `"already":true`) {
		t.Fatalf("second: %d %s", c, b)
	}
	if rows, _ := x.st.EventSubsOf("sg4~s1"); len(rows) != 1 || rows[0].CreatedBy != "board" {
		t.Fatalf("rows %+v", rows)
	}
}

func TestARoomWritesOnlyItsOwnCardsAndTheBoardAny(t *testing.T) {
	x := newEventRig(t)
	if c, b := x.subs(t, `{"action":"subscribe","kind":"land","card":"s1","room":"sg4","by":"orch@sg4"}`); c != http.StatusOK {
		t.Fatalf("own bare card: %d %s", c, b)
	}
	rows, _ := x.st.EventSubsOf("sg4~s1")
	if len(rows) != 1 || rows[0].CreatedBy != "orch@sg4" {
		t.Fatalf("rows %+v", rows)
	}
	if c, _ := x.subs(t, `{"action":"subscribe","kind":"land","card":"m1mini~m1","room":"sg4"}`); c != http.StatusForbidden {
		t.Fatalf("another room's card: %d", c)
	}
	if c, _ := x.subs(t, `{"action":"subscribe","kind":"land","card":"sg4~s1~x","room":"sg4"}`); c != http.StatusUnprocessableEntity {
		t.Fatalf("a malformed card: %d", c)
	}
	x.subscribe(t, "m1mini~m1", "", "", false)
	rows, _ = x.st.EventSubsOf("m1mini~m1")
	if len(rows) != 1 || rows[0].CreatedBy != "board" {
		t.Fatalf("the board writes any: %+v", rows)
	}
	if c, _ := x.subs(t, fmt.Sprintf(`{"action":"unsubscribe","id":%q,"room":"sg4"}`, rows[0].ID)); c != http.StatusForbidden {
		t.Fatalf("removing another room's: %d", c)
	}
	if c, b := x.subs(t, fmt.Sprintf(`{"action":"unsubscribe","id":%q}`, rows[0].ID)); c != http.StatusOK || !strings.Contains(b, `"removed":1`) {
		t.Fatalf("the board removes any: %d %s", c, b)
	}
	if c, b := x.subs(t, `{"action":"unsubscribe","kind":"land","card":"s1","room":"sg4"}`); c != http.StatusOK || !strings.Contains(b, `"removed":1`) {
		t.Fatalf("a room removes its own by filter: %d %s", c, b)
	}
}

func TestARoomsRelayedSubscribeIsStampedFromTheLink(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	st, err := hubstore.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	x.proxy.SetEvents(NewEventHook(st, x.proxy.Deliverer(), nil))
	ask := func(path, body string) RelayAnswer {
		ans, err := x.miniR.Relay(relayCtx(t), wire.RelayRequest{Op: RelayHub, From: "sa1", Method: "POST", Path: path, Body: json.RawMessage(body)})
		if err != nil {
			t.Fatal(err)
		}
		return ans
	}
	if ans := ask(hubSubsPath, `{"action":"subscribe","kind":"land","card":"m1"}`); !ans.OK {
		t.Fatalf("own card: %+v", ans)
	}
	if rows, _ := st.EventSubsOf("m1mini~m1"); len(rows) != 1 || rows[0].CreatedBy != "sa1@m1mini" {
		t.Fatalf("rows %+v", rows)
	}
	if ans := ask(hubSubsPath, `{"action":"subscribe","kind":"land","card":"sg4~s1"}`); ans.OK || ans.Code != http.StatusForbidden {
		t.Fatalf("another room's card: %+v", ans)
	}
	if ans := ask(hubSubsPath, `{"action":"subscribe","kind":"land","card":"m1","room":"sg4"}`); !ans.OK {
		t.Fatalf("a room named in the body: %+v", ans)
	}
	if rows, _ := st.EventSubsOf("sg4~m1"); len(rows) != 0 {
		t.Fatalf("a room's own say of its name wrote %+v", rows)
	}
	if ans := ask("/_hub/events", landBodyJSON("stopped", "", "", 0, "")); ans.OK {
		t.Fatalf("a room posted an event: %+v", ans)
	}
}

func TestALiveCardGetsOneFyiFromLandAtTheHub(t *testing.T) {
	x := newEventRig(t)
	x.subscribe(t, "sg4~s1", "o/r", "main", true)
	x.post(landBodyJSON("pushed", landFrom, landTo, 14, ""))
	x.hook.Pass(context.Background())
	got := x.fake.sends()
	if len(got) != 1 || got[0].Card != "sg4~s1" || got[0].From != "land@testhub" || got[0].Kind != "fyi" || !got[0].Wake ||
		!strings.HasPrefix(got[0].Text, "Landed: o/r main 3f1c0de..9a0b1c2, 14 commits.") {
		t.Fatalf("sent %+v", got)
	}
	x.hook.Pass(context.Background())
	if n := len(x.fake.sends()); n != 1 {
		t.Fatalf("a delivered notice was sent again: %d", n)
	}
}

func TestAFilterOnAnotherBranchOrRepoDeliversNothing(t *testing.T) {
	x := newEventRig(t)
	x.subscribe(t, "sg4~s1", "o/r", "develop", false)
	x.subscribe(t, "sg4~s2", "o/other", "", false)
	w := x.post(landBodyJSON("pushed", landFrom, landTo, 2, ""))
	if !strings.Contains(w.Body.String(), `"subscribers":0`) {
		t.Fatalf("answer %s", w.Body)
	}
	x.hook.Pass(context.Background())
	if n := len(x.fake.sends()); n != 0 {
		t.Fatalf("%d sent", n)
	}
	x.subscribe(t, "sg4~s3", "O/R", "main", false)
	if w := x.post(landBodyJSON("pushed", landTo, landFrom, 1, "")); !strings.Contains(w.Body.String(), `"subscribers":1`) {
		t.Fatalf("a repo in another case: %s", w.Body)
	}
}

func TestAParkedCardWithoutWakeIsPendingThenExpiresAt24Hours(t *testing.T) {
	x := newEventRig(t)
	x.subscribe(t, "sg4~s1", "", "", false)
	x.fake.send = func(CITarget) CIAnswer {
		return CIAnswer{State: hubstore.CIPending, Detail: "the session is parked and the route does not wake it"}
	}
	x.post(landBodyJSON("pushed", landFrom, landTo, 2, ""))
	x.hook.Pass(context.Background())
	if x.pendingFor(t) != 1 {
		t.Fatal("a parked card should stay pending")
	}
	*x.clock = x.clock.Add(23 * time.Hour)
	x.hook.Pass(context.Background())
	if x.pendingFor(t) != 1 {
		t.Fatal("still inside the day")
	}
	*x.clock = x.clock.Add(2 * time.Hour)
	x.hook.Pass(context.Background())
	if x.pendingFor(t) != 0 {
		t.Fatal("expired deliveries are still pending")
	}
	if l := x.audited("event-delivery"); len(l) != 1 || !strings.Contains(l[0], "24 hours") {
		t.Fatalf("audit %v", l)
	}
}

func TestThreeLandsWhileParkedDeliverOnlyTheNewest(t *testing.T) {
	x := newEventRig(t)
	x.subscribe(t, "sg4~s1", "", "", false)
	parked := true
	x.fake.send = func(CITarget) CIAnswer {
		if parked {
			return CIAnswer{State: hubstore.CIPending, Detail: "parked"}
		}
		return CIAnswer{State: hubstore.CIDelivered, Detail: "queued"}
	}
	for i, to := range []string{"1111111", "2222222", "3333333"} {
		*x.clock = x.clock.Add(time.Minute)
		x.post(landBodyJSON("pushed", fmt.Sprintf("aaaaaa%d", i), to, i+1, ""))
	}
	x.hook.Pass(context.Background())
	parked = false
	*x.clock = x.clock.Add(time.Hour)
	x.hook.Pass(context.Background())
	var texts []string
	for _, s := range x.fake.sends() {
		texts = append(texts, s.Text)
	}
	last := texts[len(texts)-1]
	if !strings.Contains(last, "aaaaaa2..3333333") {
		t.Fatalf("the newest was not the one delivered: %q", texts)
	}
	for _, s := range texts {
		if strings.Contains(s, "1111111") || strings.Contains(s, "2222222") {
			t.Fatalf("a superseded land was sent: %q", texts)
		}
	}
	if x.pendingFor(t) != 0 {
		t.Fatal("older ones are still pending")
	}
	if l := x.audited("event-delivery"); len(l) != 2 || !strings.Contains(l[0], "superseded") {
		t.Fatalf("audit %v", l)
	}
}

func TestARoomAnsweringNoSuchCardDeletesTheSubscriptionWithOneAuditLine(t *testing.T) {
	x := newEventRig(t)
	x.subscribe(t, "sg4~s1", "", "main", false)
	x.subscribe(t, "sg4~s1", "", "develop", false)
	x.subscribe(t, "sg4~s2", "", "", false)
	x.fake.send = func(t CITarget) CIAnswer {
		if t.Card == "sg4~s1" {
			return CIAnswer{State: hubstore.CIRefused, Detail: "no such task", Gone: true}
		}
		return CIAnswer{State: hubstore.CIDelivered, Detail: "queued"}
	}
	x.post(landBodyJSON("pushed", landFrom, landTo, 2, ""))
	x.hook.Pass(context.Background())
	if rows, _ := x.st.EventSubsOf("sg4~s1"); len(rows) != 0 {
		t.Fatalf("the gone card still subscribes: %+v", rows)
	}
	if rows, _ := x.st.EventSubsOf("sg4~s2"); len(rows) != 1 {
		t.Fatalf("a live card lost its subscription: %+v", rows)
	}
	if l := x.audited("event-sub-gone"); len(l) != 1 || !strings.Contains(l[0], "sg4~s1") {
		t.Fatalf("audit %v", l)
	}
}

func TestAnUnconfirmedEventDeliveryIsNeverSentAgain(t *testing.T) {
	x := newEventRig(t)
	x.subscribe(t, "sg4~s1", "", "", false)
	x.fake.send = func(CITarget) CIAnswer {
		return CIAnswer{State: hubstore.CIUnconfirmed, Detail: "the room did not answer"}
	}
	x.post(landBodyJSON("pushed", landFrom, landTo, 2, ""))
	x.hook.Pass(context.Background())
	x.hook.Pass(context.Background())
	*x.clock = x.clock.Add(time.Hour)
	x.hook.Pass(context.Background())
	if n := len(x.fake.sends()); n != 1 {
		t.Fatalf("%d sends, want 1", n)
	}
}

func TestARefusedDeliveryIsOneAuditLine(t *testing.T) {
	x := newEventRig(t)
	x.subscribe(t, "sg4~s1", "", "", false)
	x.fake.send = func(CITarget) CIAnswer { return CIAnswer{State: hubstore.CIRefused, Detail: "not allowed"} }
	x.post(landBodyJSON("pushed", landFrom, landTo, 2, ""))
	x.hook.Pass(context.Background())
	if l := x.audited("event-delivery"); len(l) != 1 || !strings.Contains(l[0], "not allowed") {
		t.Fatalf("audit %v", l)
	}
	if rows, _ := x.st.EventSubsOf("sg4~s1"); len(rows) != 1 {
		t.Fatal("a refusal that is not 'no such card' removed the subscription")
	}
}

func TestTheDailyCheckDeletesGoneCardsAndLeavesADetachedRoomAlone(t *testing.T) {
	x := newEventRig(t)
	for _, c := range []string{"sg4~live", "sg4~dead", "away~a1", "bad~b1"} {
		x.subscribe(t, c, "", "", false)
	}
	var asked []string
	x.hook.Rooms = func() []string { return []string{"sg4", "bad"} }
	x.hook.Exist = func(_ context.Context, room string, ids []string) ([]string, error) {
		asked = append(asked, room)
		if room == "bad" {
			return nil, errors.New("the room did not answer")
		}
		return []string{"live"}, nil
	}
	x.hook.Pass(context.Background())
	got, _ := x.st.EventSubsAll()
	var left []string
	for _, s := range got {
		left = append(left, s.Card)
	}
	sort.Strings(left)
	if strings.Join(left, ",") != "away~a1,bad~b1,sg4~live" {
		t.Fatalf("left %v", left)
	}
	if len(x.audited("event-sub-gone")) != 1 {
		t.Fatalf("audit %v", x.audited("event-sub-gone"))
	}
	x.hook.Pass(context.Background())
	if len(asked) != 2 {
		t.Fatalf("the check ran again inside the day: %v", asked)
	}
	*x.clock = x.clock.Add(25 * time.Hour)
	x.hook.Pass(context.Background())
	if len(asked) != 4 {
		t.Fatalf("the check did not run after a day: %v", asked)
	}
}

func TestAnEventSweepDropsOldRowsWithTheCISweep(t *testing.T) {
	x := newEventRig(t)
	x.subscribe(t, "sg4~s1", "", "", false)
	x.post(landBodyJSON("pushed", landFrom, landTo, 2, ""))
	*x.clock = x.clock.Add(hubstore.CIKeep + time.Hour)
	x.hook.Pass(context.Background())
	if n, _ := x.st.EventSweep(); n != 0 {
		t.Fatalf("the pass left %d old events", n)
	}
	if x.pendingFor(t) != 0 {
		t.Fatal("old deliveries survived the sweep")
	}
}

func TestTheSharedLoopWorksBothTablesAndWakesOnAnEvent(t *testing.T) {
	x := newCIRig(t)
	ev := NewEventHook(x.st, x.fake, nil)
	ev.Now = x.hook.Now
	x.hook.SetEvents(ev)
	x.routes(t, qaRoute)
	x.fake.card("qa", "r~1")
	if _, _, err := x.st.EventSubAdd(hubstore.EventSub{Kind: "land", Card: "sg4~s1"}); err != nil {
		t.Fatal(err)
	}
	if w := x.send(ciBody("o/r", 1, 1, "CI", "main", "failure")); w.Code != http.StatusAccepted {
		t.Fatalf("ci: %d", w.Code)
	}
	select {
	case <-x.hook.kick:
	default:
	}
	r := httptest.NewRequest(http.MethodPost, "/_hub/events", strings.NewReader(landBodyJSON("pushed", landFrom, landTo, 2, "")))
	w := httptest.NewRecorder()
	ev.Receive(w, r)
	if w.Code != http.StatusAccepted {
		t.Fatalf("event: %d", w.Code)
	}
	select {
	case <-x.hook.kick:
	default:
		t.Fatal("an event did not wake the loop")
	}
	x.hook.Pass(context.Background())
	var from []string
	for _, s := range x.fake.sends() {
		from = append(from, s.From)
	}
	if len(from) != 2 || from[0] != "ci@hub" || from[1] != "land@hub" {
		t.Fatalf("sent from %v", from)
	}
}

func TestTheProxyDeliversAnEventToACardOnARoom(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	st, err := hubstore.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hook := x.proxy.NewEventHook(st, "land@testhub", nil)
	x.proxy.SetEvents(hook)
	post := func(path, body string) *http.Response {
		res, err := http.Post(x.board+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res
	}
	if res := post("/_hub/events/subs", `{"action":"subscribe","kind":"land","card":"sg4~s1","wake":true}`); res.StatusCode != http.StatusOK {
		t.Fatalf("subscribe: %d", res.StatusCode)
	}
	if res := post("/_hub/events", landBodyJSON("pushed", landFrom, landTo, 14, "")); res.StatusCode != http.StatusAccepted {
		t.Fatalf("event: %d", res.StatusCode)
	}
	hook.Pass(relayCtx(t))
	got := x.sg4.messages()
	if len(got) != 1 || got[0]["id"] != "s1" || got[0]["from"] != "land@testhub" || got[0]["kind"] != "fyi" ||
		!strings.HasPrefix(got[0]["text"], "Landed: o/r main") {
		t.Fatalf("sg4 got %+v", got)
	}
}
