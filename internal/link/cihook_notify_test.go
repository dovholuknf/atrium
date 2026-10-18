//go:build integration

package link

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

type ciNote struct{ title, body string }

type ciNotes struct {
	mu  sync.Mutex
	got []ciNote
}

func (n *ciNotes) tell(title, body string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.got = append(n.got, ciNote{title, body})
}

func (n *ciNotes) all() []ciNote {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]ciNote(nil), n.got...)
}

func (x *ciRig) notes() *ciNotes {
	n := &ciNotes{}
	x.hook.Tell = n.tell
	return n
}

func (n *ciNotes) one(t *testing.T) ciNote {
	t.Helper()
	got := n.all()
	if len(got) != 1 {
		t.Fatalf("got %d notices, want 1: %+v", len(got), got)
	}
	return got[0]
}

func TestADeliveredRunRaisesOneNoticeNamingTheRunAndTheTargets(t *testing.T) {
	x := newCIRig(t)
	x.routes(t, `[{"repo":"o/r","to":"qa","kind":"needs"},{"repo":"o/r","to":"orch"}]`)
	n := x.notes()
	x.send(ciBody("o/r", 7, 2, "CI", "main", "failure"))
	got := n.one(t)
	for _, want := range []string{"CI failure", "o/r", "CI on main", "0123456", "attempt 2"} {
		if !strings.Contains(got.title, want) {
			t.Errorf("title %q lacks %q", got.title, want)
		}
	}
	for _, want := range []string{"qa (needs)", "orch (needs)", "https://github.com/o/r/actions/runs/7"} {
		if !strings.Contains(got.body, want) {
			t.Errorf("body %q lacks %q", got.body, want)
		}
	}
	if strings.Contains(got.title+got.body, "0123456789abcdef") {
		t.Errorf("the whole sha is in the notice: %+v", got)
	}
}

func TestARunNoRouteNamesRaisesANoticeSayingSo(t *testing.T) {
	x := newCIRig(t)
	x.routes(t, `[{"repo":"other/repo","to":"qa"}]`)
	n := x.notes()
	x.send(ciBody("o/r", 1, 1, "CI", "main", "success"))
	got := n.one(t)
	if !strings.HasPrefix(got.body, ciDropped+"no route names this repository") {
		t.Fatalf("body %q", got.body)
	}
}

func TestARunWithNoRoutesAtAllRaisesANoticeSayingNoRoute(t *testing.T) {
	x := newCIRig(t)
	n := x.notes()
	x.send(ciBody("o/r", 1, 1, "CI", "main", "success"))
	if got := n.one(t); !strings.Contains(got.body, "no route") {
		t.Fatalf("body %q", got.body)
	}
}

func TestAFilteredRunRaisesANoticeSayingWhichFilterDroppedIt(t *testing.T) {
	for _, c := range []struct{ rule, body, want string }{
		{`[{"repo":"o/r","to":"qa","conclusion":["failure"]}]`, ciBody("o/r", 1, 1, "CI", "main", "success"), "conclusion success"},
		{`[{"repo":"o/r","to":"qa","branch":"release"}]`, ciBody("o/r", 1, 1, "CI", "main", "failure"), "another branch"},
		{`[{"repo":"o/r","to":"qa","workflow":"docs"}]`, ciBody("o/r", 1, 1, "CI", "main", "failure"), "another workflow"},
	} {
		x := newCIRig(t)
		x.routes(t, c.rule)
		n := x.notes()
		x.send(c.body)
		got := n.one(t)
		if !strings.HasPrefix(got.body, ciDropped+"filtered") || !strings.Contains(got.body, c.want) {
			t.Errorf("rule %s: body %q, want filtered and %q", c.rule, got.body, c.want)
		}
	}
}

func TestAChangeRuleDropsARepeatedConclusionAndSaysSo(t *testing.T) {
	x := newCIRig(t)
	x.routes(t, `[{"repo":"o/r","to":"qa","change":true}]`)
	x.send(ciBody("o/r", 1, 1, "CI", "main", "failure"))
	n := x.notes()
	x.send(ciBody("o/r", 2, 1, "CI", "main", "failure"))
	if got := n.one(t); !strings.Contains(got.body, "asks for a change") {
		t.Fatalf("body %q", got.body)
	}
}

func TestABadOrMissingSecretRaisesANoticeWithTheReasonAndNothingFromTheBody(t *testing.T) {
	for _, c := range []struct{ secret, why string }{{"nope", "wrong secret"}, {"", "no secret"}} {
		x := newCIRig(t)
		n := x.notes()
		x.post(c.secret, ciBody("secret-repo/leak", 1, 1, "SecretFlow", "main", "success"))
		got := n.one(t)
		if got.body != ciDropped+c.why+"." {
			t.Errorf("%q: body %q", c.secret, got.body)
		}
		all := got.title + got.body
		for _, leak := range []string{"secret-repo", "SecretFlow"} {
			if strings.Contains(all, leak) {
				t.Errorf("%q: the notice carries %q", c.secret, leak)
			}
		}
	}
}

func TestAWrongSecretNoticeNeverCarriesTheRealSecret(t *testing.T) {
	x := newCIRig(t)
	n := x.notes()
	x.post("guess", ciBody("o/r", 1, 1, "CI", "main", "success"))
	x.send(ciBody("o/r", 2, 1, "CI", "main", "success"))
	for _, g := range n.all() {
		if strings.Contains(g.title+g.body, x.secret) || strings.Contains(g.title+g.body, "guess") {
			t.Fatalf("a secret is in %+v", g)
		}
	}
}

func TestMalformedBodiesRaiseANoticeEachWithNoFreeText(t *testing.T) {
	for _, c := range []struct{ name, body, want string }{
		{"not json", `not json at all, free text here`, "malformed, the body is not a workflow_run event"},
		{"missing fields", `{"workflow_run":{"name":"free text name"},"repository":{"full_name":"o/r"}}`, "needs workflow_run.id"},
		{"too big", `{"pad":"` + strings.Repeat("a", 600<<10) + `"}`, "over 512 KiB"},
	} {
		x := newCIRig(t)
		n := x.notes()
		x.send(c.body)
		got := n.one(t)
		if !strings.HasPrefix(got.body, ciDropped) || !strings.Contains(got.body, c.want) {
			t.Errorf("%s: body %q, want %q", c.name, got.body, c.want)
		}
		if strings.Contains(got.title+got.body, "free text") {
			t.Errorf("%s: free text from the body is in %+v", c.name, got)
		}
	}
}

func TestAnUnlistedRepositoryRaisesARefusalNotice(t *testing.T) {
	x := newCIRig(t)
	if err := x.st.SetCIHookRepos([]string{"o/r"}); err != nil {
		t.Fatal(err)
	}
	n := x.notes()
	x.send(ciBody("evil/repo", 1, 1, "CI", "main", "success"))
	got := n.one(t)
	if !strings.Contains(got.title, "evil/repo") || !strings.Contains(got.body, "not in the hook's list") {
		t.Fatalf("got %+v", got)
	}
}

func TestADuplicatePostRaisesANoticeSayingItWasARepeat(t *testing.T) {
	x := newCIRig(t)
	x.routes(t, qaRoute)
	x.send(ciBody("o/r", 1, 1, "CI", "main", "success"))
	n := x.notes()
	x.send(ciBody("o/r", 1, 1, "CI", "main", "success"))
	if got := n.one(t); !strings.Contains(got.body, "repeat") {
		t.Fatalf("body %q", got.body)
	}
}

func TestRefusedAndRateLimitedNoticesAreThrottledToOneAMinuteEach(t *testing.T) {
	x := newCIRig(t)
	n := x.notes()
	for i := 0; i < 5; i++ {
		x.post("nope", ciBody("o/r", int64(i+1), 1, "CI", "main", "success"))
	}
	if len(n.all()) != 1 {
		t.Fatalf("5 refused posts raised %d notices, want 1", len(n.all()))
	}
	*x.clock = x.clock.Add(61 * time.Second)
	x.post("nope", ciBody("o/r", 9, 1, "CI", "main", "success"))
	if len(n.all()) != 2 {
		t.Fatalf("a refused post a minute on raised %d notices in all, want 2", len(n.all()))
	}
	for i := 0; i < 40; i++ {
		x.send(ciBody("o/r", int64(100+i), 1, "CI", "main", "success"))
	}
	limited := 0
	for _, g := range n.all() {
		if strings.Contains(g.body, "too many notices") {
			limited++
		}
	}
	if limited != 1 {
		t.Fatalf("%d rate limit notices, want 1", limited)
	}
}

func TestEveryAcceptedPostRaisesExactlyOneNotice(t *testing.T) {
	x := newCIRig(t)
	x.routes(t, qaRoute)
	n := x.notes()
	for i := 1; i <= 3; i++ {
		if w := x.send(ciBody("o/r", int64(i), 1, "CI", "main", "success")); w.Code != http.StatusAccepted {
			t.Fatal(w.Code)
		}
	}
	if len(n.all()) != 3 {
		t.Fatalf("3 posts raised %d notices", len(n.all()))
	}
}

func TestANoticeBecomesAGrowlerOnTheBoard(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	st, err := hubstore.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, r := range []string{"sg4", "m1mini"} {
		if _, err := st.Add(r, hubstore.TransportDirect); err != nil {
			t.Fatal(err)
		}
	}
	x.proxy.SetGrowler(NewGrowler(testGrowlStore{st}))
	secret, _ := st.MintCIHookSecret(false)
	hook := NewCIHook(st, x.proxy.CIDeliverer(), nil)
	x.proxy.SetCIHook(hook)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(ciBody("o/r", 1, 1, "CI", "main", "failure")))
	req.Header.Set(ciSecretHeader, secret)
	hook.Receive(httptest.NewRecorder(), req)
	rows, err := st.GrowlLive()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].RoomName != "m1mini" || !strings.HasPrefix(rows[0].ID, "ci|") ||
		!strings.Contains(rows[0].Title, "CI failure: o/r CI on main") || !strings.Contains(rows[0].Body, "no route") {
		t.Fatalf("growlers = %+v", rows)
	}
}
