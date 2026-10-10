//go:build integration

package link

import (
	"net/http"
	"strings"
	"testing"
)

func (x *claimHub) worktreeVia(t *testing.T, room, body string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, x.front.URL+"/v1/providers/gh/pr-worktree", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if room != "" {
		req.Header.Set(RoomHeader, room)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var sb strings.Builder
	buf := make([]byte, 512)
	for {
		n, err := res.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return res, sb.String()
}

const wtBody = `{"host":"github.com","org":"openziti","repo":"tlsuv","number":378}`

// A PASTED PR'S WORKTREE GOES TO THE LEAST BUSY ROOM, and the answer says which, so the launch can follow it.
func TestPRWorktreeGoesToTheLeastBusyRoom(t *testing.T) {
	x := newClaimHub(t, map[string]int{"alpha": 4, "beta": 0})
	defer x.done()
	res, body := x.worktreeVia(t, "", wtBody)
	if res.StatusCode != 200 || res.Header.Get(PlacedRoomHeader) != "beta" || !strings.Contains(body, "/wt/beta") {
		t.Fatalf("answer = %d %q placed %q", res.StatusCode, body, res.Header.Get(PlacedRoomHeader))
	}
	if x.rooms["beta"].worktrees != 1 || x.rooms["alpha"].worktrees != 0 {
		t.Fatalf("beta %d alpha %d", x.rooms["beta"].worktrees, x.rooms["alpha"].worktrees)
	}
}

// A SECOND PASTE FOLDS INTO THE OWNER, even when the owner has since become the busier room.
func TestPRWorktreeSecondPasteFoldsIntoTheOwner(t *testing.T) {
	x := newClaimHub(t, map[string]int{"alpha": 4, "beta": 0})
	defer x.done()
	x.worktreeVia(t, "", wtBody)
	x.rooms["beta"].running = 9
	res, _ := x.worktreeVia(t, "", wtBody)
	if res.Header.Get(PlacedRoomHeader) != "beta" || x.rooms["beta"].worktrees != 2 || x.rooms["alpha"].worktrees != 0 {
		t.Fatalf("placed %q beta %d alpha %d", res.Header.Get(PlacedRoomHeader), x.rooms["beta"].worktrees,
			x.rooms["alpha"].worktrees)
	}
	if c, err := x.st.PRClaimOf("github.com/openziti/tlsuv/378"); err != nil || c.Room != "beta" {
		t.Fatalf("claim = %+v, %v", c, err)
	}
}

// A ROOM NAMED IS NOT PLACED: it is the board's scope or the operator's choice.
func TestPRWorktreeNamedRoomIsNotPlaced(t *testing.T) {
	x := newClaimHub(t, map[string]int{"alpha": 4, "beta": 0})
	defer x.done()
	res, _ := x.worktreeVia(t, "alpha", wtBody)
	if res.Header.Get(PlacedRoomHeader) != "" || x.rooms["alpha"].worktrees != 1 {
		t.Fatalf("placed %q alpha %d", res.Header.Get(PlacedRoomHeader), x.rooms["alpha"].worktrees)
	}
}

// A REFUSED WORKTREE RELEASES THE CLAIM IT MADE, so a retry places the key again instead of finding it owned by a
// room that has nothing for it.
func TestARefusedWorktreeReleasesTheClaim(t *testing.T) {
	x := newClaimHub(t, map[string]int{"alpha": 4, "beta": 0})
	defer x.done()
	key := "github.com/openziti/tlsuv/378"
	x.rooms["beta"].refuseWT = true
	if res, body := x.worktreeVia(t, "", wtBody); res.StatusCode != http.StatusBadGateway {
		t.Fatalf("answer = %d %s", res.StatusCode, body)
	}
	if c, err := x.st.PRClaimOf(key); err == nil {
		t.Fatalf("the claim is still there: %+v", c)
	}
	// The retry is placed afresh, here on alpha now that beta is the busy one.
	x.rooms["beta"].refuseWT = false
	x.rooms["beta"].running = 9
	res, _ := x.worktreeVia(t, "", wtBody)
	if res.StatusCode != 200 || res.Header.Get(PlacedRoomHeader) != "alpha" {
		t.Fatalf("retry = %d placed %q", res.StatusCode, res.Header.Get(PlacedRoomHeader))
	}
	if c, err := x.st.PRClaimOf(key); err != nil || c.Room != "alpha" {
		t.Fatalf("claim = %+v, %v", c, err)
	}
}

// A REFUSAL ON A CLAIM THIS REQUEST DID NOT MAKE leaves the claim alone: the owner made the worktree earlier.
func TestARefusalLeavesAnOlderClaimAlone(t *testing.T) {
	x := newClaimHub(t, map[string]int{"alpha": 4, "beta": 0})
	defer x.done()
	key := "github.com/openziti/tlsuv/378"
	if res, _ := x.worktreeVia(t, "", wtBody); res.StatusCode != 200 {
		t.Fatalf("first = %d", res.StatusCode)
	}
	x.rooms["beta"].refuseWT = true
	x.worktreeVia(t, "", wtBody)
	if c, err := x.st.PRClaimOf(key); err != nil || c.Room != "beta" {
		t.Fatalf("claim = %+v, %v", c, err)
	}
}
