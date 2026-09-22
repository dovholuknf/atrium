package link

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/dovholuknf/atrium/internal/inputlag"
)

// lagRoom records every settings write it is sent.
type lagRoom struct {
	mu   sync.Mutex
	got  []string
	name string
}

func (l *lagRoom) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/v1/settings" && r.Method == http.MethodPost {
		b, _ := io.ReadAll(r.Body)
		l.mu.Lock()
		l.got = append(l.got, string(b))
		l.mu.Unlock()
	}
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, `{}`)
}

func (l *lagRoom) writes() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.got...)
}

// ONE CHECKBOX IN THE ALL VIEW REACHES THE HUB AND EVERY ROOM.
//
// A settings write in the ALL view has no room to land in, and every other
// machine-shaped write is refused for that reason. This one is the hub's to
// pass on, so the hub's own hop and every room's hops are timed together.
func TestTheInputLagSwitchReachesTheHubAndEveryRoom(t *testing.T) {
	lagOnFor(t)
	inputlag.SetLive(false)
	a, b := &lagRoom{name: "alpha"}, &lagRoom{name: "beta"}
	front, _, done := two(t, a, b)
	defer done()

	res, err := http.Post(front.URL+"/v1/settings", "application/json",
		strings.NewReader(`{"input_lag_log":true}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("the switch answered %d in the ALL view", res.StatusCode)
	}
	if !inputlag.On() {
		t.Fatal("the hub did not switch its own logging on")
	}
	for _, room := range []*lagRoom{a, b} {
		got := room.writes()
		if len(got) != 1 || !strings.Contains(got[0], `"input_lag_log":true`) {
			t.Errorf("room %s was sent %v, wanted the switch", room.name, got)
		}
	}
}

// Scoped to one room, the write goes to that room as before, and the hub still
// reads it on the way past.
func TestTheInputLagSwitchScopedToARoomStillSwitchesTheHub(t *testing.T) {
	lagOnFor(t)
	inputlag.SetLive(false)
	a, b := &lagRoom{name: "alpha"}, &lagRoom{name: "beta"}
	front, _, done := two(t, a, b)
	defer done()

	req, _ := http.NewRequest(http.MethodPost, front.URL+"/v1/settings",
		strings.NewReader(`{"input_lag_log":true}`))
	req.Header.Set(RoomHeader, "alpha")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if !inputlag.On() {
		t.Fatal("the hub did not read the switch off a room-scoped write")
	}
	if got := a.writes(); len(got) != 1 || !strings.Contains(got[0], "input_lag_log") {
		t.Errorf("the scoped room lost the body on the way through: %v", got)
	}
	if got := b.writes(); len(got) != 0 {
		t.Errorf("a room-scoped write reached the other room: %v", got)
	}
}
