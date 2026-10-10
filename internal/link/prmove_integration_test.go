//go:build integration

package link

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const moveKey = "github.com/openziti/tlsuv/378"

func moveVia(t *testing.T, x *claimHub, body string) (int, string) {
	t.Helper()
	res, err := http.Post(x.front.URL+"/_hub/pr-claims/move", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

// claimed puts the key on a room the way a first paste does.
func claimed(t *testing.T, x *claimHub, room string) {
	t.Helper()
	if _, _, err := x.st.ClaimPR(moveKey, room, "test"); err != nil {
		t.Fatal(err)
	}
}

func TestTheMoveCopiesTheReviewThenMovesTheClaimThenArchivesTheOldRow(t *testing.T) {
	x := newClaimHub(t, map[string]int{"alpha": 0, "beta": 0})
	defer x.done()
	claimed(t, x, "beta")
	x.rooms["beta"].review = []byte("the archive bytes \x00\x01\x02 and more")
	x.rooms["beta"].reviewID = "pr_old"

	code, body := moveVia(t, x, `{"key":"`+moveKey+`","to":"alpha","walker":"card-9"}`)
	if code != 200 || !strings.Contains(body, `"room":"alpha"`) || !strings.Contains(body, `"review":"moved"`) ||
		!strings.Contains(body, `"pr_id":"new-alpha"`) {
		t.Fatalf("move = %d %s", code, body)
	}
	if c, _ := x.st.PRClaimOf(moveKey); c.Room != "alpha" {
		t.Fatalf("claim = %+v", c)
	}
	got, _, walkers := x.rooms["alpha"].reviewCalls()
	if string(got) != string(x.rooms["beta"].review) {
		t.Fatalf("alpha imported %q", got)
	}
	_, archived, _ := x.rooms["beta"].reviewCalls()
	if len(archived) != 1 || archived[0] != "pr_old" {
		t.Fatalf("beta archived %v", archived)
	}
	if len(walkers) != 1 || walkers[0] != `/v1/prs/new-alpha/walker {"action":"set","task":"card-9"}` {
		t.Fatalf("walkers %v", walkers)
	}
}

func TestAClaimWithNoReviewJustMoves(t *testing.T) {
	x := newClaimHub(t, map[string]int{"alpha": 0, "beta": 0})
	defer x.done()
	claimed(t, x, "beta")
	code, body := moveVia(t, x, `{"key":"`+moveKey+`","to":"alpha","walker":"card-9"}`)
	if code != 200 || !strings.Contains(body, `"review":"none"`) {
		t.Fatalf("move = %d %s", code, body)
	}
	if c, _ := x.st.PRClaimOf(moveKey); c.Room != "alpha" {
		t.Fatalf("claim = %+v", c)
	}
	got, _, walkers := x.rooms["alpha"].reviewCalls()
	_, archived, _ := x.rooms["beta"].reviewCalls()
	if got != nil || len(archived) != 0 || len(walkers) != 0 {
		t.Fatalf("something was done: %q %v %v", got, archived, walkers)
	}
}

func TestTheMoveIsRefusedWhileEitherRoomIsOffline(t *testing.T) {
	x := newClaimHub(t, map[string]int{"alpha": 0, "beta": 0})
	defer x.done()
	claimed(t, x, "beta")
	x.rooms["beta"].review = []byte("archive")
	x.rooms["beta"].reviewID = "pr_old"

	// The old room is offline.
	x.rooms["beta"].stop()
	waitFor(t, 5*time.Second, func() bool { return !x.hub.Has("beta") })
	code, body := moveVia(t, x, `{"key":"`+moveKey+`","to":"alpha"}`)
	if code != http.StatusConflict || !strings.Contains(body, "the review is on beta, which is offline. bring it back or move it later") {
		t.Fatalf("old offline = %d %s", code, body)
	}
	if c, _ := x.st.PRClaimOf(moveKey); c.Room != "beta" {
		t.Fatalf("claim = %+v", c)
	}

	// The new room is offline, the old one is not.
	y := newClaimHub(t, map[string]int{"alpha": 0, "beta": 0})
	defer y.done()
	claimed(t, y, "beta")
	y.rooms["beta"].review = []byte("archive")
	y.rooms["beta"].reviewID = "pr_old"
	y.rooms["alpha"].stop()
	waitFor(t, 5*time.Second, func() bool { return !y.hub.Has("alpha") })
	code, body = moveVia(t, y, `{"key":"`+moveKey+`","to":"alpha"}`)
	if code != http.StatusConflict || !strings.Contains(body, "alpha is offline") {
		t.Fatalf("new offline = %d %s", code, body)
	}
	if c, _ := y.st.PRClaimOf(moveKey); c.Room != "beta" {
		t.Fatalf("claim = %+v", c)
	}
	if _, archived, _ := y.rooms["beta"].reviewCalls(); len(archived) != 0 {
		t.Fatalf("archived %v", archived)
	}
}

func TestAFailedImportLeavesTheClaimAndTheOldRow(t *testing.T) {
	x := newClaimHub(t, map[string]int{"alpha": 0, "beta": 0})
	defer x.done()
	claimed(t, x, "beta")
	x.rooms["beta"].review = []byte("archive")
	x.rooms["beta"].reviewID = "pr_old"
	x.rooms["alpha"].importStatus = http.StatusRequestEntityTooLarge

	code, body := moveVia(t, x, `{"key":"`+moveKey+`","to":"alpha","walker":"card-9"}`)
	if code != http.StatusBadGateway || !strings.Contains(body, "alpha refused the review: the archive is over the cap") ||
		!strings.Contains(body, "stay on beta") {
		t.Fatalf("move = %d %s", code, body)
	}
	if c, _ := x.st.PRClaimOf(moveKey); c.Room != "beta" {
		t.Fatalf("claim = %+v", c)
	}
	_, archived, _ := x.rooms["beta"].reviewCalls()
	_, _, walkers := x.rooms["alpha"].reviewCalls()
	if len(archived) != 0 || len(walkers) != 0 {
		t.Fatalf("archived %v walkers %v", archived, walkers)
	}
}

func TestMovingAClaimToItsOwnRoomDoesNothing(t *testing.T) {
	x := newClaimHub(t, map[string]int{"alpha": 0, "beta": 0})
	defer x.done()
	claimed(t, x, "beta")
	x.rooms["beta"].review = []byte("archive")
	x.rooms["beta"].reviewID = "pr_old"
	code, _ := moveVia(t, x, `{"key":"`+moveKey+`","to":"beta"}`)
	_, archived, _ := x.rooms["beta"].reviewCalls()
	if code != 200 || len(archived) != 0 {
		t.Fatalf("%d %v", code, archived)
	}
}
