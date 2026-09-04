package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// An agent saying it finished. The largest hole in what atrium does, so the
// tests are about the two claims it makes: the card moves, and what it said is
// kept.

func finish(t *testing.T, d *Daemon, body map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post("http://"+d.opts.AgentAddr+"/finish",
		"application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("finish answered unreadably: %v", err)
	}
	if resp.StatusCode >= 300 {
		t.Fatalf("finish answered %s: %v", resp.Status, out)
	}
	return out
}

func TestAnAgentCanSayItFinished(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task, _, err := d.st.Register(store.Observed{
		WireName: "fin-1", Worktree: "/tmp/atrium-test", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}

	out := finish(t, d, map[string]any{
		"agent": "fin-1",
		"recap": "bumped the vcpkg dep, ran the tests, opened a pull request.",
	})
	if out["recorded"] != true {
		t.Fatalf("it was not recorded: %v", out)
	}

	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.StatusDone {
		t.Fatalf("the card is %s, wanted done", got.Status)
	}
	if !got.Recapped() {
		t.Fatal("the card kept no account of what the session did")
	}
	if !strings.Contains(got.Recap, "vcpkg") {
		t.Fatalf("the recap is %q", got.Recap)
	}
	if got.RecapAt == nil {
		t.Fatal("nothing records when it said so")
	}
}

// Finishing with nothing to say is allowed, and the card says so. That
// distinction is the whole point of storing a recap: a session that ended with
// an account of itself and one that did not are different.
func TestFinishingWithNoRecapIsAllowedAndVisible(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task, _, err := d.st.Register(store.Observed{
		WireName: "fin-2", Worktree: "/tmp/atrium-test", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}

	finish(t, d, map[string]any{"agent": "fin-2"})

	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.StatusDone {
		t.Fatalf("the card is %s", got.Status)
	}
	if got.Recapped() {
		t.Fatal("a card with no recap claims to have one")
	}
	if got.RecapAt != nil {
		t.Fatal("a card with no recap has a time on it")
	}
}

// Handing the work back is a different claim from finishing it, and worth
// being able to say.
func TestHandingBackIsNotFinishing(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task, _, err := d.st.Register(store.Observed{
		WireName: "fin-3", Worktree: "/tmp/atrium-test", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}

	finish(t, d, map[string]any{
		"agent": "fin-3", "status": "needs-input",
		"recap": "got as far as the build failing, and I do not know why.",
	})

	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.StatusNeedsInput {
		t.Fatalf("handing back put the card in %s", got.Status)
	}
	if !got.Recapped() {
		t.Fatal("handing back lost what the session said")
	}
}

// A card put down by hand stays put. Shelving is an answer, and a session
// inside a shelved worktree announcing it is done does not overrule it.
func TestFinishingDoesNotUnshelveACard(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task, _, err := d.st.Register(store.Observed{
		WireName: "fin-4", Worktree: "/tmp/atrium-test", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetStatus(task.ID, store.StatusShelved); err != nil {
		t.Fatal(err)
	}

	finish(t, d, map[string]any{"agent": "fin-4", "recap": "done anyway"})

	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.StatusShelved {
		t.Fatalf("a shelved card was moved to %s by its session", got.Status)
	}
	// The recap is still kept. What it said is worth having even though the
	// card is not moving.
	if !got.Recapped() {
		t.Fatal("a shelved card threw away what its session said")
	}
}

// A session atrium has never heard of is not an error. Failing here would mean
// a session could fail at the moment it tried to end tidily.
func TestFinishingAnUnknownSessionIsNotAFailure(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	out := finish(t, d, map[string]any{"agent": "nobody-here", "recap": "hello"})
	if out["ok"] != true {
		t.Fatalf("an unknown session got an error: %v", out)
	}
	if out["recorded"] != false {
		t.Fatalf("an unknown session was recorded against something: %v", out)
	}
}

// Saying which session it is remains required. Guessing would attach an
// account of one piece of work to another.
func TestFinishingNeedsToSayWhichSession(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	resp, err := http.Post("http://"+d.opts.AgentAddr+"/finish",
		"application/json", strings.NewReader(`{"recap":"something"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("a nameless finish answered %s", resp.Status)
	}
}

// Done that an agent declared and done that a human dragged are the same
// column and not the same claim, so the history has to be able to tell them
// apart afterwards.
func TestFinishingIsRecordedAsComingFromTheAgent(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task, _, err := d.st.Register(store.Observed{
		WireName: "fin-5", Worktree: "/tmp/atrium-test", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	finish(t, d, map[string]any{"agent": "fin-5", "recap": "did the thing"})

	events, err := d.st.Events(task.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if e.Kind != store.EventSubmitted {
			continue
		}
		if strings.Contains(string(e.Payload), `"by":"agent"`) &&
			strings.Contains(string(e.Payload), `"kind":"finished"`) {
			return
		}
	}
	t.Fatal("nothing in the history says the agent declared this finished")
}
