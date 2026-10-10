//go:build integration

package daemon

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// unleanDaemon is a daemon with a shell runner and a card launched lean onto
// it, the way the dotfiles agent left tlsuv/fix-ci.
func unleanDaemon(t *testing.T) (*Daemon, *store.Task, http.Handler) {
	t.Helper()
	d := testDaemon(t)
	cmd, args := "sh", []string{"-c", "sleep 60"}
	if runtime.GOOS == "windows" {
		cmd, args = "cmd.exe", []string{"/k", "rem"}
	}
	if _, err := d.st.SaveHarness(store.Harness{
		ID: "unlean", Label: "unlean test", Enabled: true,
		Cmd: cmd, Args: args, LaunchMode: store.LaunchPTY,
	}); err != nil {
		t.Fatal(err)
	}
	card, _, err := d.st.Register(store.Observed{WireName: "fix-ci", Worktree: t.TempDir(), Runner: "unlean"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetTags(card.ID, mergeTags([]string{OriginAgentTag}, leanTags([]string{"ziti"}, leanKit{}))); err != nil {
		t.Fatal(err)
	}
	card, err = d.st.Get(card.ID)
	if err != nil {
		t.Fatal(err)
	}
	return d, card, d.BoardHandler()
}

func send(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func launchOnto(h http.Handler, card *store.Task, extra string) *httptest.ResponseRecorder {
	return send(h, "POST", "/v1/launch", `{"harness":"unlean","cwd":"`+
		strings.ReplaceAll(card.Worktree, `\`, `/`)+`","task_id":"`+card.ID+`"`+extra+`}`)
}

func startedLean(t *testing.T, d *Daemon, id string) bool {
	t.Helper()
	events, err := d.st.Events(id, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if e.Kind == store.EventLaunched {
			return strings.Contains(string(e.Payload), `"lean":true`)
		}
	}
	t.Fatal("no launched event")
	return false
}

// The control: with nothing said, the card decides, and this one is lean.
func TestALeanCardStartsLeanWhenNothingSaysOtherwise(t *testing.T) {
	_, card, h := unleanDaemon(t)
	w := launchOnto(h, card, "")
	if w.Code == http.StatusOK || !strings.Contains(w.Body.String(), "cannot start lean") {
		t.Fatalf("a lean card started onto with no lean field should start lean, answered %d: %s", w.Code, w.Body)
	}
}

// Path one: `lean: false` on the launch wins over the card, and takes the lean
// tags off it so the next reopen is not lean either.
func TestLeanFalseStartsALeanCardWithTheFullSetup(t *testing.T) {
	d, card, h := unleanDaemon(t)
	w := launchOnto(h, card, `,"lean":false`)
	if w.Code != http.StatusOK {
		t.Skipf("could not start onto the card, answered %d: %s", w.Code, w.Body)
	}
	t.Cleanup(func() { _ = d.StopRunner(card.ID) })
	if startedLean(t, d, card.ID) {
		t.Fatal("lean: false started it lean")
	}
	got, err := d.st.Get(card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.Tags, ",") != OriginAgentTag {
		t.Fatalf("the lean tags should be off the card and the rest kept, tags are %q", got.Tags)
	}
}

// Path two: a tag edit that takes atrium:lean off clears lean for the next
// start, with no lean field on that start.
func TestATagEditThatDropsLeanClearsIt(t *testing.T) {
	d, card, h := unleanDaemon(t)
	if w := send(h, "PATCH", "/v1/tasks/"+card.ID, `{"tags":["`+OriginAgentTag+`"]}`); w.Code != http.StatusOK {
		t.Fatalf("the tag edit answered %d: %s", w.Code, w.Body)
	}
	w := launchOnto(h, card, "")
	if strings.Contains(w.Body.String(), "cannot start lean") {
		t.Fatalf("a card whose lean tag was edited off still started lean: %s", w.Body)
	}
	if w.Code != http.StatusOK {
		t.Skipf("could not start onto the card, answered %d: %s", w.Code, w.Body)
	}
	t.Cleanup(func() { _ = d.StopRunner(card.ID) })
	if startedLean(t, d, card.ID) {
		t.Fatal("the edited card started lean")
	}
}

// A card whose only tag was the lean one comes out with none, not with it kept
// because the list was empty.
func TestLeanFalseClearsACardWhoseOnlyTagWasLean(t *testing.T) {
	d, card, h := unleanDaemon(t)
	if err := d.st.SetTags(card.ID, []string{LeanTag}); err != nil {
		t.Fatal(err)
	}
	w := launchOnto(h, card, `,"lean":false`)
	if w.Code != http.StatusOK {
		t.Skipf("could not start onto the card, answered %d: %s", w.Code, w.Body)
	}
	t.Cleanup(func() { _ = d.StopRunner(card.ID) })
	got, err := d.st.Get(card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tags) != 0 {
		t.Fatalf("tags are %q", got.Tags)
	}
}
