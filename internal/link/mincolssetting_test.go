package link

import (
	"net/http"
	"strings"
	"testing"
)

// THE WIDTH FLOOR SAVED IN THE ALL VIEW REACHES EVERY ROOM, rather than being
// refused for want of a room to land in.
func TestTheWidthFloorReachesEveryRoom(t *testing.T) {
	a, b := &lagRoom{name: "alpha"}, &lagRoom{name: "beta"}
	front, _, done := two(t, a, b)
	defer done()

	res, err := http.Post(front.URL+"/v1/settings", "application/json",
		strings.NewReader(`{"terminal_min_cols":"150"}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("the floor answered %d in the ALL view", res.StatusCode)
	}
	for _, room := range []*lagRoom{a, b} {
		got := room.writes()
		if len(got) != 1 || !strings.Contains(got[0], `"terminal_min_cols":"150"`) {
			t.Errorf("room %s was sent %v, wanted the floor", room.name, got)
		}
	}
}
