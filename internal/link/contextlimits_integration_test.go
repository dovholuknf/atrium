//go:build integration

package link

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// limitsStock is a hub inventory with a settings table.
type limitsStock struct {
	*remembering
	*memSettings
}

func withLimitsStock(t *testing.T, p *Proxy) *memSettings {
	t.Helper()
	st := &memSettings{m: map[string]string{}}
	p.SetInventory(limitsStock{&remembering{}, st})
	return st
}

func postLimits(t *testing.T, url, room, body string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url+"/v1/settings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if room != "" {
		req.Header.Set(RoomHeader, room)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode
}

func sentLimits(room *lagRoom) int {
	n := 0
	for _, w := range room.writes() {
		if strings.Contains(w, `"context_limits"`) {
			n++
		}
	}
	return n
}

// In the ALL view the hub keeps the list and passes it to every room.
func TestTheContextLimitsReachEveryRoomFromTheAllView(t *testing.T) {
	a, b := &lagRoom{name: "alpha"}, &lagRoom{name: "beta"}
	front, _, done := two(t, a, b)
	defer done()
	st := withLimitsStock(t, front.Config.Handler.(*Proxy))

	if code := postLimits(t, front.URL, "", `{"context_limits":{"claude":250,"codex":300}}`); code != http.StatusOK {
		t.Fatalf("the ALL view answered %d", code)
	}
	if got := st.m[hubContextLimits]; got != `{"claude":250,"codex":300}` {
		t.Fatalf("the hub stored %q", got)
	}
	for _, room := range []*lagRoom{a, b} {
		if got := room.writes(); len(got) != 1 || !strings.Contains(got[0], `"claude":250`) {
			t.Errorf("room %s was sent %v", room.name, got)
		}
	}
	// A bad list is refused at the hub and reaches nobody.
	if code := postLimits(t, front.URL, "", `{"context_limits":{"claude":1}}`); code != http.StatusBadRequest {
		t.Fatalf("a bad list answered %d", code)
	}
	if sentLimits(a) != 1 || st.m[hubContextLimits] != `{"claude":250,"codex":300}` {
		t.Fatal("a bad list was passed on or stored")
	}
}

// In one room's view that room gets the write, and the hub passes it to the others.
func TestTheContextLimitsInOneRoomsViewReachTheOthers(t *testing.T) {
	a, b := &lagRoom{name: "alpha"}, &lagRoom{name: "beta"}
	front, _, done := two(t, a, b)
	defer done()
	withLimitsStock(t, front.Config.Handler.(*Proxy))

	if code := postLimits(t, front.URL, "alpha", `{"context_limits":{"claude":300}}`); code != http.StatusOK {
		t.Fatalf("answered %d", code)
	}
	waitFor(t, 5*time.Second, func() bool { return sentLimits(b) == 1 })
	if sentLimits(a) != 1 {
		t.Fatalf("the named room got %v, wanted the one write", a.writes())
	}
}

// A room that attaches later is sent the hub's list; a hub never told sends nothing.
func TestARoomThatAttachesLaterIsSentTheContextLimits(t *testing.T) {
	a, b := &lagRoom{name: "alpha"}, &lagRoom{name: "beta"}
	front, _, done := two(t, a, b)
	defer done()
	p := front.Config.Handler.(*Proxy)
	st := withLimitsStock(t, p)

	p.PushContextLimits("alpha")
	if sentLimits(a) != 0 {
		t.Fatal("a hub never told sent a list")
	}
	st.m[hubContextLimits] = `{"claude":400}`
	p.PushContextLimits("alpha")
	if got := a.writes(); len(got) != 1 || !strings.Contains(got[0], `"claude":400`) {
		t.Fatalf("the attaching room was sent %v", got)
	}
	// Back to the default is passed on too, as an empty list.
	st.m[hubContextLimits] = "default"
	p.PushContextLimits("beta")
	if got := b.writes(); len(got) != 1 || !strings.Contains(got[0], `"context_limits":{}`) {
		t.Fatalf("the default was sent as %v", got)
	}
}
