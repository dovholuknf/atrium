package daemon

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// The failure this is written against: the orchestrator ended turns with an
// Open Questions block, the operator never read them, and the orchestrator
// kept saying they were still open. See docs/seen-design.md.

// seenCard fetches a card through the board's API, which is where the board
// and the control tools both read it.
func seenCard(t *testing.T, d *Daemon, id string) *store.SeenView {
	t.Helper()
	resp, err := http.Get("http://" + d.opts.HumanAddr + "/v1/tasks/" + id)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var v struct {
		Seen *store.SeenView `json:"seen"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v.Seen
}

func waitSeen(t *testing.T, d *Daemon, id string, ok func(*store.SeenView) bool, what string) *store.SeenView {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var v *store.SeenView
	for time.Now().Before(deadline) {
		if v = seenCard(t, d, id); ok(v) {
			return v
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s: card shows %+v", what, v)
	return nil
}

func seenStop(t *testing.T, d *Daemon, agent string, questions []string) {
	t.Helper()
	resp := postJSON(t, "http://"+d.opts.AgentAddr+"/stop", map[string]any{
		"agent": agent, "cwd": "/tmp/atrium-seen",
		"questions": questions, "questions_block": len(questions) > 0, "questions_known": true,
	})
	resp.Body.Close()
}

// The whole loop: a turn ends with questions, it is unseen and owed, a board
// window reports seeing it, and a prompt answers the questions.
func TestATurnIsUnseenUntilTheBoardSeesItAndAnsweredByAPrompt(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task, _, err := d.st.Register(store.Observed{WireName: "orch", Worktree: "/tmp/atrium-seen", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if v := seenCard(t, d, task.ID); v != nil {
		t.Fatalf("a card no turn ended on carries seen: %+v", v)
	}

	seenStop(t, d, "orch", []string{"land sa21 first?", "build the tray?"})
	v := seenCard(t, d, task.ID)
	if v == nil || !v.Unseen || len(v.OpenQuestions) != 2 || v.Answered == nil || *v.Answered {
		t.Fatalf("after the turn: %+v", v)
	}

	// The board reports a window showed it.
	raw, _ := json.Marshal(map[string]any{"turn_ended_at": v.TurnEndedAt})
	resp, err := http.Post("http://"+d.opts.HumanAddr+"/v1/tasks/"+task.ID+"/seen",
		"application/json", strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	v = seenCard(t, d, task.ID)
	if v.Unseen || v.SeenVia != store.SeenViewed || len(v.OpenQuestions) != 2 {
		t.Fatalf("seeing a turn is not answering it: %+v", v)
	}

	// The operator types a prompt.
	resp = postJSON(t, "http://"+d.opts.AgentAddr+"/activity", ActivityEvent{Agent: "orch", Event: "prompt"})
	resp.Body.Close()
	waitSeen(t, d, task.ID, func(v *store.SeenView) bool {
		return v != nil && v.Answered != nil && *v.Answered && len(v.OpenQuestions) == 0
	}, "a prompt did not answer the questions")
}

// A board message from a PEER is not the operator reading anything.
func TestAPeerMessageDoesNotAnswer(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task, _, err := d.st.Register(store.Observed{WireName: "orch", Worktree: "/tmp/atrium-seen", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	seenStop(t, d, "orch", []string{"which base?"})

	url := "http://" + d.opts.HumanAddr + "/v1/tasks/" + task.ID + "/message"
	resp := postJSON(t, url, map[string]any{"text": "sa21 is done", "from": "sa21"})
	resp.Body.Close()
	if v := seenCard(t, d, task.ID); !v.Unseen || len(v.OpenQuestions) != 1 {
		t.Fatalf("a peer's message answered the operator's questions: %+v", v)
	}

	// The operator's own message does.
	resp = postJSON(t, url, map[string]any{"text": "main"})
	resp.Body.Close()
	if v := seenCard(t, d, task.ID); v.Unseen || len(v.OpenQuestions) != 0 || v.AnsweredVia != store.SeenMessage {
		t.Fatalf("the operator's message did not answer: %+v", v)
	}
}

// A turn with no block keeps the questions, as when a peer's report started it.
func TestATurnWithNoBlockKeepsTheQuestions(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task, _, err := d.st.Register(store.Observed{WireName: "orch", Worktree: "/tmp/atrium-seen", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	seenStop(t, d, "orch", []string{"a", "b", "c", "d"})
	seenStop(t, d, "orch", nil)
	if v := seenCard(t, d, task.ID); len(v.OpenQuestions) != 4 || !v.Unseen {
		t.Fatalf("a later turn with no block dropped the questions: %+v", v)
	}
}

// A prompt that is the peer message atrium just typed in is not the operator.
// One the operator typed after it is.
func TestAPeerTypedPromptIsNotTheOperator(t *testing.T) {
	d := testDaemon(t)
	target, r, _ := peerPair(t, d)
	if err := d.st.NoteTurnEnded(target.ID, store.TurnQuestions{Known: true, Block: true, List: []string{"q"}}); err != nil {
		t.Fatal(err)
	}
	if typed, _ := d.tellByTyping(target, "sg4/sa21", "report: done"); !typed {
		t.Fatal("the peer message was not typed")
	}
	if !r.promptWasPeer(time.Now()) {
		t.Fatal("the prompt right after a peer message was read as the operator")
	}
	d.seenPrompted(target.ID)
	if s, _ := d.st.GetSeen(target.ID); !s.Unseen() || s.Answered() {
		t.Fatalf("a peer's typed prompt answered the operator's questions: %+v", s)
	}

	// Long after, or once the operator has typed, it is the operator.
	if r.promptWasPeer(time.Now().Add(peerPromptWindow + time.Second)) {
		t.Fatal("a prompt long after a peer message is still read as the peer")
	}
	r.noteOperatorTyped([]byte("main\r"))
	if r.promptWasPeer(time.Now()) {
		t.Fatal("a prompt after the operator typed is read as the peer")
	}
	d.seenPrompted(target.ID)
	if s, _ := d.st.GetSeen(target.ID); s.Unseen() || !s.Answered() {
		t.Fatalf("the operator's prompt did not answer: %+v", s)
	}
}

// Typing into the terminal sees the turn, from memory on the keystroke path,
// and survives a restart through the store.
func TestTypingSeesTheTurn(t *testing.T) {
	d := testDaemon(t)
	task := cardFor(t, d, "typist")
	d.noteTurnForSeen(task.ID, store.TurnQuestions{Known: true})

	// A restart loses the in-memory set. loadUnseen puts it back.
	d.unseen.Delete(task.ID)
	d.loadUnseen()
	if _, ok := d.unseen.Load(task.ID); !ok {
		t.Fatal("an unseen turn was not reloaded from the store")
	}
	d.seenTyped(task.ID)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s, _ := d.st.GetSeen(task.ID); !s.Unseen() {
			if s.SeenVia != store.SeenTyped {
				t.Fatalf("seen via %q", s.SeenVia)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("a keystroke did not see the turn")
}
