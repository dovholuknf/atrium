//go:build integration

package link

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func sentPolicy(room *lagRoom) int {
	n := 0
	for _, w := range room.writes() {
		if strings.Contains(w, `"worker_policy"`) {
			n++
		}
	}
	return n
}

// In the ALL view the hub keeps the policy and passes it to every room, and a bad one reaches nobody.
func TestTheWorkerPolicyReachesEveryRoomFromTheAllView(t *testing.T) {
	a, b := &lagRoom{name: "alpha"}, &lagRoom{name: "beta"}
	front, _, done := two(t, a, b)
	defer done()
	st := withLimitsStock(t, front.Config.Handler.(*Proxy))

	if code := postLimits(t, front.URL, "", `{"worker_policy":{"model":"sonnet","budget_usd":20}}`); code != http.StatusOK {
		t.Fatalf("the ALL view answered %d", code)
	}
	if got := st.m[hubWorkerPolicy]; got != `{"model":"sonnet","budget_usd":20}` {
		t.Fatalf("the hub stored %q", got)
	}
	for _, room := range []*lagRoom{a, b} {
		if got := room.writes(); len(got) != 1 || !strings.Contains(got[0], `"model":"sonnet"`) {
			t.Errorf("room %s was sent %v", room.name, got)
		}
	}
	if code := postLimits(t, front.URL, "", `{"worker_policy":{"budget_usd":-5}}`); code != http.StatusBadRequest {
		t.Fatalf("a bad policy answered %d", code)
	}
	if sentPolicy(a) != 1 || st.m[hubWorkerPolicy] != `{"model":"sonnet","budget_usd":20}` {
		t.Fatal("a bad policy was passed on or stored")
	}
	// Both off is kept as off, so a room that attaches later is still told.
	if code := postLimits(t, front.URL, "", `{"worker_policy":{"model":"","budget_usd":0}}`); code != http.StatusOK {
		t.Fatalf("turning it off answered %d", code)
	}
	if st.m[hubWorkerPolicy] != workerPolicyOff {
		t.Fatalf("off was stored as %q", st.m[hubWorkerPolicy])
	}
}

// In one room's view that room gets the write, and the hub passes it to the others.
func TestTheWorkerPolicyInOneRoomsViewReachesTheOthers(t *testing.T) {
	a, b := &lagRoom{name: "alpha"}, &lagRoom{name: "beta"}
	front, _, done := two(t, a, b)
	defer done()
	withLimitsStock(t, front.Config.Handler.(*Proxy))

	if code := postLimits(t, front.URL, "alpha", `{"worker_policy":{"budget_usd":5}}`); code != http.StatusOK {
		t.Fatalf("answered %d", code)
	}
	waitFor(t, 5*time.Second, func() bool { return sentPolicy(b) == 1 })
	if sentPolicy(a) != 1 {
		t.Fatalf("the named room got %v, wanted the one write", a.writes())
	}
}

// A room that attaches later is sent the hub's policy, and a hub never told sends nothing.
func TestARoomThatAttachesLaterIsSentTheWorkerPolicy(t *testing.T) {
	a, b := &lagRoom{name: "alpha"}, &lagRoom{name: "beta"}
	front, _, done := two(t, a, b)
	defer done()
	p := front.Config.Handler.(*Proxy)
	st := withLimitsStock(t, p)

	p.PushWorkerPolicy("alpha")
	if sentPolicy(a) != 0 {
		t.Fatal("a hub never told sent a policy")
	}
	st.m[hubWorkerPolicy] = `{"model":"haiku","budget_usd":3}`
	p.PushWorkerPolicy("alpha")
	if got := a.writes(); len(got) != 1 || !strings.Contains(got[0], `"model":"haiku"`) {
		t.Fatalf("the attaching room was sent %v", got)
	}
	st.m[hubWorkerPolicy] = workerPolicyOff
	p.PushWorkerPolicy("beta")
	if got := b.writes(); len(got) != 1 || !strings.Contains(got[0], `"budget_usd":0`) {
		t.Fatalf("off was sent as %v", got)
	}
}
