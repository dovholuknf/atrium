package link

import (
	"net/http"
	"testing"
)

// `POST /v1/tasks/<card>/model` is a per-card verb, so the hub sends it to the
// room holding the card the way it sends a message or an exit: by plain id past a
// wrong header, by tag, and by `name@room`. Nothing on the hub names the verb.
func TestAModelSwitchGoesToTheRoomHoldingTheCard(t *testing.T) {
	a := &named{room: "alpha", cards: []ctlCard{agentCard(idA, "alpha-rnd", "rnd", "running")}}
	b := &named{room: "beta", cards: []ctlCard{agentCard(idB, "beta-rnd", "rnd", "running")}}
	front, _, done := two(t, a, b)
	defer done()

	body := `{"model":"opus"}`
	// A plain id, with a header naming the other room.
	code, got := send(t, http.MethodPost, front.URL+"/v1/tasks/"+idB+"/model", "alpha", body)
	if code != http.StatusOK || got.By != "beta" || got.Path != "/v1/tasks/"+idB+"/model" {
		t.Fatalf("a plain id landed on %q at %q (%d %s), want beta", got.By, got.Path, code, got.Error)
	}
	// A tagged id reaches the room bare.
	code, got = send(t, http.MethodPost, front.URL+"/v1/tasks/alpha~"+idA+"/model", "beta", body)
	if code != http.StatusOK || got.By != "alpha" || got.Path != "/v1/tasks/"+idA+"/model" {
		t.Fatalf("a tagged id landed on %q at %q (%d %s), want alpha bare", got.By, got.Path, code, got.Error)
	}
	// name@room.
	code, _, m := ask(t, http.MethodPost, front.URL+"/v1/tasks/rnd@beta/model", body)
	if code != http.StatusOK || m["served_by"] != "beta" || m["path"] != "/v1/tasks/"+idB+"/model" {
		t.Fatalf("rnd@beta answered %d %v", code, m)
	}
}
