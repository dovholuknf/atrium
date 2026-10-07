package link

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func (c *claimRoom) opened() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.opens...)
}

// openVia is a POST /v1/open through the hub, with a room named or not. It answers the status, the placed room
// header and the body.
func (x *claimHub) openVia(t *testing.T, room, url string) (int, string, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, x.front.URL+"/v1/open", strings.NewReader(`{"url":"`+url+`"}`))
	req.Header.Set("Content-Type", "application/json")
	if room != "" {
		req.Header.Set(RoomHeader, room)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return res.StatusCode, res.Header.Get(PlacedRoomHeader), out
}

// AN OPEN GOES TO THE ROOM THAT HOLDS THE LINK, whatever its load, else to the least busy room, and a named room
// wins over both. The answer carries the room, and the card and row ids tagged with it.
func TestOpenOnTheHubGoesToTheHolderElseTheLeastBusyRoom(t *testing.T) {
	withSeed(t, seedRow("github-pull-request", 10,
		`^https?://(?P<host>github\.com)/(?P<org>[A-Za-z0-9_.-]+)/(?P<repo>[A-Za-z0-9_.-]+)/pull/(?P<num>\d+)(?:[/?#].*)?$`))
	x := newClaimHub(t, map[string]int{"alpha": 3, "beta": 0})
	defer x.done()
	x.proxy.SetForge(&memSettings{m: map[string]string{}}, nil)

	// nobody holds it: the least busy room
	code, placed, out := x.openVia(t, "", "https://github.com/openziti/zrok/pull/11")
	if code != http.StatusCreated || placed != "beta" || out["room"] != "beta" || out["card"] != "beta~c1" ||
		out["pr"] != "beta~pr_1" {
		t.Fatalf("unheld = %d %q %v", code, placed, out)
	}
	if got := x.rooms["beta"].opened(); len(got) != 1 {
		t.Fatalf("beta opened %v", got)
	}

	// alpha holds it: alpha, busy as it is
	if _, _, err := x.st.ClaimPR("github.com/openziti/zrok/12", "alpha", "paste"); err != nil {
		t.Fatal(err)
	}
	code, placed, out = x.openVia(t, "", "https://github.com/openziti/zrok/pull/12/files")
	if code != http.StatusCreated || placed != "alpha" || out["card"] != "alpha~c1" {
		t.Fatalf("held = %d %q %v", code, placed, out)
	}

	// a named room wins
	code, _, out = x.openVia(t, "alpha", "https://github.com/openziti/zrok/pull/13")
	if code != http.StatusCreated || out["room"] != "alpha" || out["card"] != "alpha~c1" {
		t.Fatalf("named = %d %v", code, out)
	}
	if got := x.rooms["alpha"].opened(); len(got) != 2 {
		t.Fatalf("alpha opened %v", got)
	}
}
