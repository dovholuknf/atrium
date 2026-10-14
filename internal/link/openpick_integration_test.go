//go:build integration

package link

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// holderVia is GET /_hub/pr-claims/holder through the hub, as the launch dialog asks it before "go".
func (x *claimHub) holderVia(t *testing.T, link string) map[string]string {
	t.Helper()
	res, err := http.Get(x.front.URL + "/_hub/pr-claims/holder?url=" + link)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	var out map[string]string
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("holder answered %s: %v", b, err)
	}
	return out
}

const issueKey = "github.com/microsoft/terminal/i20674"

// A LINK ANOTHER ROOM HOLDS GOES ON IN THE ROOM PICKED, and its claim moves there. With a live card on the link in the
// holder, nothing moves and the card is the answer, whichever room was picked.
func TestAPickedRoomTakesAHeldLinkUnlessTheHolderHasALiveCard(t *testing.T) {
	x := newClaimHub(t, map[string]int{"alpha": 0, "beta": 0})
	defer x.done()
	ask := func(room string, take bool) PRClaimAnswer {
		t.Helper()
		ans, err := x.rooms[room].room.ClaimPR(context.Background(), map[string]any{"key": issueKey, "held": true, "take": take, "source": "open"})
		if err != nil {
			t.Fatal(err)
		}
		return ans
	}
	if _, _, err := x.st.ClaimPR(issueKey, "alpha", "open"); err != nil {
		t.Fatal(err)
	}

	// without a pick, as before: the holder's name, nothing moves
	if ans := ask("beta", false); ans.Owner != "alpha" || ans.Live != "" {
		t.Fatalf("an ask with no pick = %+v", ans)
	}

	// a pick with a live card at the holder: that card, and the claim stays
	x.rooms["alpha"].live = map[string]string{issueKey: "c9"}
	if ans := ask("beta", true); ans.Owner != "alpha" || ans.Live != "c9" {
		t.Fatalf("a pick against a live card = %+v", ans)
	}
	if c, _ := x.st.PRClaimOf(issueKey); c.Room != "alpha" {
		t.Fatalf("the claim moved to %q with a live card on it", c.Room)
	}

	// no live card: the claim moves to the room picked
	x.rooms["alpha"].live = nil
	if ans := ask("beta", true); ans.Owner != "beta" || ans.Live != "" {
		t.Fatalf("a pick with no live card = %+v", ans)
	}
	if c, _ := x.st.PRClaimOf(issueKey); c.Room != "beta" {
		t.Fatalf("the claim is %q, want beta", c.Room)
	}
	// and a second ask from either room says beta
	if ans := ask("alpha", false); ans.Owner != "beta" {
		t.Fatalf("alpha asking after the move = %+v", ans)
	}
}

// THE DIALOG ASKS WHO HOLDS A LINK before "go": the room, and the card it has live on it.
func TestTheHolderOfALinkIsSaidBeforeGo(t *testing.T) {
	issue := seedRow("github-issue", 20,
		`^https?://(?P<host>github\.com)/(?P<org>[A-Za-z0-9_.-]+)/(?P<repo>[A-Za-z0-9_.-]+)/issues/(?P<num>\d+)(?:[/?#].*)?$`)
	issue.Tags, issue.Branch, issue.Kind = "issue", "issue-{num}", "github"
	x := newClaimHub(t, map[string]int{"alpha": 0, "beta": 0})
	defer x.done()
	x.proxy.SetForge(&memSettings{m: map[string]string{}}, nil)
	seedHub(x, issue)
	link := "https://github.com/microsoft/terminal/issues/20674"

	if h := x.holderVia(t, link); h["owner"] != "" || h["card"] != "" {
		t.Fatalf("an unheld link said %v", h)
	}
	if _, _, err := x.st.ClaimPR(issueKey, "alpha", "open"); err != nil {
		t.Fatal(err)
	}
	if h := x.holderVia(t, link); h["owner"] != "alpha" || h["card"] != "" {
		t.Fatalf("a held link said %v", h)
	}
	x.rooms["alpha"].live = map[string]string{issueKey: "c9"}
	if h := x.holderVia(t, link); h["owner"] != "alpha" || h["card"] != "c9" {
		t.Fatalf("a held link with a live card said %v", h)
	}
}
