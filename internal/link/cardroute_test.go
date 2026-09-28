package link

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// holds is a room that holds some cards, answers a card read only for those,
// and on every other request says which room served it, the path it saw, and
// the launch's task_id. A room that does not hold the card answers the way the
// real one did in item 63.
func holds(room string, ids ...string) http.Handler {
	has := map[string]bool{}
	for _, id := range ids {
		has[id] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var body struct {
			TaskID string `json:"task_id"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		id := cardIDIn(r.URL.Path)
		if id == "" {
			id = body.TaskID
		}
		if id != "" && !notCards[id] && !has[id] {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintf(w, `{"error":"no card %s to start onto: sql: no rows in result set"}`, id)
			return
		}
		// `saw_task_id`, not `task_id`, which the hub retags on a tagged answer.
		fmt.Fprintf(w, `{"served_by":%q,"path":%q,"saw_task_id":%q}`, room, r.URL.Path, body.TaskID)
	})
}

type served struct {
	By     string `json:"served_by"`
	Path   string `json:"path"`
	TaskID string `json:"saw_task_id"`
	Error  string `json:"error"`
}

func send(t *testing.T, method, url, header, body string) (int, served) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if header != "" {
		req.Header.Set(RoomHeader, header)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var got served
	raw, _ := io.ReadAll(res.Body)
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("%s %s answered %d %s", method, url, res.StatusCode, raw)
	}
	return res.StatusCode, got
}

// THE START IN ITEM 63. The board posts a launch onto an existing card with the
// card in the BODY and a stale `writeRoom` header naming another room. The card
// decides.
func TestALaunchOntoACardGoesToTheRoomHoldingIt(t *testing.T) {
	front, _, done := two(t, holds("alpha", "acard"), holds("beta", "bcard"))
	defer done()

	for _, header := range []string{"", "alpha"} {
		code, got := send(t, http.MethodPost, front.URL+"/v1/launch", header,
			`{"harness":"claude","task_id":"bcard","title":""}`)
		if code != http.StatusOK || got.By != "beta" {
			t.Fatalf("header %q: the start landed on %q (%d %s), not beta",
				header, got.By, code, got.Error)
		}
		if got.TaskID != "bcard" {
			t.Fatalf("header %q: the room was given task_id %q", header, got.TaskID)
		}
	}
}

// A room-tagged id in the body names its room, and reaches the room bare.
func TestALaunchWithATaggedCardGoesToItsRoomBare(t *testing.T) {
	front, _, done := two(t, holds("alpha", "acard"), holds("beta", "bcard"))
	defer done()

	code, got := send(t, http.MethodPost, front.URL+"/v1/launch", "alpha",
		`{"harness":"claude","task_id":"beta~bcard"}`)
	if code != http.StatusOK || got.By != "beta" {
		t.Fatalf("the start landed on %q (%d %s), not beta", got.By, code, got.Error)
	}
	if got.TaskID != "bcard" {
		t.Fatalf("the room was given task_id %q, want it bare", got.TaskID)
	}
}

// The answer to a launch onto a tagged card names its room, so the board does
// not end up holding a bare id for a card it listed tagged.
func TestALaunchOntoATaggedCardAnswersTagged(t *testing.T) {
	room := func(name string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			var body struct {
				TaskID string `json:"task_id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			fmt.Fprintf(w, `{"id":%q,"supervised":true}`, body.TaskID)
		})
	}
	front, _, done := two(t, room("alpha"), room("beta"))
	defer done()

	res, err := http.Post(front.URL+"/v1/launch", "application/json",
		strings.NewReader(`{"harness":"claude","task_id":"beta~bcard"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var got struct {
		ID   string `json:"id"`
		Room string `json:"room"`
	}
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.ID != "beta~bcard" || got.Room != "beta" {
		t.Fatalf("the launch answered id %q room %q, want beta~bcard on beta", got.ID, got.Room)
	}
}

// A launch that makes a new card names none, so the header still decides.
func TestALaunchOfANewCardStillFollowsTheHeader(t *testing.T) {
	front, _, done := two(t, holds("alpha", "acard"), holds("beta", "bcard"))
	defer done()

	code, got := send(t, http.MethodPost, front.URL+"/v1/launch", "alpha", `{"harness":"claude"}`)
	if code != http.StatusOK || got.By != "alpha" {
		t.Fatalf("a new launch landed on %q (%d), not the header's alpha", got.By, code)
	}
}

// THE DRAG IN ITEM 63. A group change is `PATCH /v1/tasks/<plain id>`, which the
// board sends with the stale header too, because it has no verb after the id.
func TestAPatchOfAPlainCardIgnoresAWrongHeader(t *testing.T) {
	front, _, done := two(t, holds("alpha", "acard"), holds("beta", "bcard"))
	defer done()

	code, got := send(t, http.MethodPatch, front.URL+"/v1/tasks/bcard", "alpha", `{"group":"g"}`)
	if code != http.StatusOK || got.By != "beta" {
		t.Fatalf("the patch landed on %q (%d %s), not beta", got.By, code, got.Error)
	}
	// And a per-card verb with the same wrong header.
	code, got = send(t, http.MethodPost, front.URL+"/v1/tasks/acard/message", "beta", `{"text":"hi"}`)
	if code != http.StatusOK || got.By != "alpha" {
		t.Fatalf("the message landed on %q (%d %s), not alpha", got.By, code, got.Error)
	}
}

// A tag in the path beats a header naming another room.
func TestATaggedPathBeatsAWrongHeader(t *testing.T) {
	front, _, done := two(t, holds("alpha", "acard"), holds("beta", "bcard"))
	defer done()

	code, got := send(t, http.MethodPatch, front.URL+"/v1/tasks/beta~bcard", "alpha", `{}`)
	if code != http.StatusOK || got.By != "beta" || got.Path != "/v1/tasks/bcard" {
		t.Fatalf("the patch landed on %q at %q (%d %s), want beta at the bare path",
			got.By, got.Path, code, got.Error)
	}
}

// A card no attached room holds is a 404 from the hub naming the card and the
// rooms asked, never a guess and never the room's "no rows".
func TestACardNoRoomHoldsIsA404NamingIt(t *testing.T) {
	front, _, done := two(t, holds("alpha", "acard"), holds("beta", "bcard"))
	defer done()

	for _, c := range []struct{ method, path, body string }{
		{http.MethodPost, "/v1/launch", `{"harness":"claude","task_id":"ghost"}`},
		{http.MethodPatch, "/v1/tasks/ghost", `{}`},
	} {
		code, got := send(t, c.method, front.URL+c.path, "alpha", c.body)
		if code != http.StatusNotFound {
			t.Fatalf("%s %s answered %d, want 404", c.method, c.path, code)
		}
		if !strings.Contains(got.Error, "ghost") || !strings.Contains(got.Error, "alpha") ||
			!strings.Contains(got.Error, "beta") || strings.Contains(got.Error, "sql") {
			t.Fatalf("%s %s said %q, want the card and the rooms named", c.method, c.path, got.Error)
		}
	}
}

// `/v1/tasks/prune` and `/v1/tasks/pin-order` are not cards, so the header
// still decides where they go.
func TestTaskRoutesThatAreNotCardsFollowTheHeader(t *testing.T) {
	front, _, done := two(t, holds("alpha", "acard"), holds("beta", "bcard"))
	defer done()

	for _, path := range []string{"/v1/tasks/prune", "/v1/tasks/pin-order"} {
		code, got := send(t, http.MethodPost, front.URL+path, "beta", `{}`)
		if got.By != "beta" {
			t.Fatalf("%s landed on %q (%d %s), not the header's beta", path, got.By, code, got.Error)
		}
	}
}
