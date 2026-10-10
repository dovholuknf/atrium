//go:build integration

package link

import (
	"net/http"
	"strings"
	"testing"
)

func (c *claimRoom) setNoSCM(v bool) {
	c.mu.Lock()
	c.noSCM = v
	c.mu.Unlock()
}

// post is a POST through the hub with no room named.
func (x *claimHub) post(t *testing.T, path, body string) (int, string, http.Header) {
	t.Helper()
	res, err := http.Post(x.front.URL+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := res.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return res.StatusCode, b.String(), res.Header
}

// THE LEAST BUSY ROOM HAS NO SCM FOLDER, SO THE PASTE LANDS ON THE OTHER ONE, for an open and for a PR worktree.
func TestPasteSkipsALeastBusyRoomWithNoSCMFolder(t *testing.T) {
	x := newClaimHub(t, map[string]int{"alpha": 0, "beta": 3})
	defer x.done()
	x.rooms["alpha"].setNoSCM(true)

	code, body, h := x.post(t, "/v1/open", `{"url":"`+prURL+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("open = %d %s, want it made on beta", code, body)
	}
	if got := h.Get(PlacedRoomHeader); got != "beta" {
		t.Fatalf("placed on %q, want beta", got)
	}
	if o := x.rooms["beta"].opens; len(o) != 1 {
		t.Fatalf("beta opens = %v", o)
	}

	code, body, h = x.post(t, "/v1/providers/github/pr-worktree", `{"host":"github.com","org":"o","repo":"r","number":9}`)
	if code != http.StatusOK || h.Get(PlacedRoomHeader) != "beta" || !strings.Contains(body, "/wt/beta") {
		t.Fatalf("worktree = %d %s placed %q, want beta", code, body, h.Get(PlacedRoomHeader))
	}
	// The claim followed the paste, so a second paste goes where the worktree is.
	if c, err := x.st.PRClaimOf("github.com/o/r/9"); err != nil || c.Room != "beta" {
		t.Fatalf("claim = %+v, %v, want beta", c, err)
	}
}

// NO ROOM CAN, so the answer is one message that names each room and why.
func TestNoRoomWithAnSCMFolderAnswersOnceNamingEachRoom(t *testing.T) {
	x := newClaimHub(t, map[string]int{"alpha": 0, "beta": 3})
	defer x.done()
	x.rooms["alpha"].setNoSCM(true)
	x.rooms["beta"].setNoSCM(true)

	code, body, _ := x.post(t, "/v1/open", `{"url":"`+prURL+`"}`)
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, `"code":"no_scm_root"`) {
		t.Fatalf("open = %d %s", code, body)
	}
	for _, want := range []string{"alpha: alpha has no scm folder", "beta: beta has no scm folder"} {
		if strings.Count(body, want) != 1 {
			t.Fatalf("%q is not in the answer exactly once: %s", want, body)
		}
	}
}
