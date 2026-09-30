package daemon

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// The failure this is written against: the orchestrator ended turns with an
// Open Questions block, the operator never read them, and the orchestrator
// kept saying they were still open. See docs/runtime/seen-design.md.

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
	if typed, _ := d.tellByTyping(target, "sg4/sa21", "report: done", false); !typed {
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
	d.noteTurnForSeen(task.ID, store.TurnQuestions{Known: true}, "")

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

// ── a worker whose launcher already got the report (backlog-2 item 43) ─────────

func reportToLauncher(t *testing.T, d *Daemon, worker *store.Task) {
	t.Helper()
	if err := d.st.MarkReported(worker.ID); err != nil {
		t.Fatal(err)
	}
}

// A worker that reported and then stopped is not waiting on a human, and says
// its launcher is the reason.
func TestAReportedWorkersTurnIsSeenByItsLauncher(t *testing.T) {
	d := testDaemon(t)
	_, worker := launchedPair(t, d)
	reportToLauncher(t, d, worker)
	stopTurn(t, d, "worker")

	s, err := d.st.GetSeen(worker.ID)
	if err != nil || s == nil || s.TurnEndedAt == nil {
		t.Fatalf("no turn recorded: %+v %v", s, err)
	}
	if s.Unseen() || s.SeenVia != store.SeenLauncher {
		t.Fatalf("reported turn: unseen %v via %q", s.Unseen(), s.SeenVia)
	}
	if _, ok := d.unseen.Load(worker.ID); ok {
		t.Fatal("the keystroke path thinks a launcher-seen turn is unseen")
	}
}

// Stopping without a report is the case the dot exists for, and the launcher is
// told.
func TestASilentWorkersTurnIsUnseenAndNoticed(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	stopTurn(t, d, "worker")

	if s, _ := d.st.GetSeen(worker.ID); !s.Unseen() || s.SeenVia != "" {
		t.Fatalf("a silent stop is not unseen: %+v", s)
	}
	if n := len(pendingFrom(t, d, launcher.ID)); n != 1 {
		t.Fatalf("the launcher has %d notices, want the silent stop", n)
	}
}

// A card a human launched keeps the dot whatever it said.
func TestAHumanLaunchedCardStillWearsTheDot(t *testing.T) {
	d := testDaemon(t)
	mine := peerCard(t, d, "mine")
	prompt(t, d, mine.ID)
	reportToLauncher(t, d, mine)
	stopTurn(t, d, "mine")
	if s, _ := d.st.GetSeen(mine.ID); !s.Unseen() {
		t.Fatalf("a human card's turn was hidden: %+v", s)
	}
}

// The launcher reading a report is not clint answering a question.
func TestLauncherSeenLeavesQuestionsOpen(t *testing.T) {
	d := testDaemon(t)
	_, worker := launchedPair(t, d)
	reportToLauncher(t, d, worker)
	raw, _ := json.Marshal(map[string]any{"agent": "worker",
		"questions": []string{"which base?"}, "questions_block": true, "questions_known": true})
	rec := httptest.NewRecorder()
	d.handleStop(rec, httptest.NewRequest(http.MethodPost, "/stop", bytes.NewReader(raw)))

	s, _ := d.st.GetSeen(worker.ID)
	if s.Unseen() || s.SeenVia != store.SeenLauncher {
		t.Fatalf("not launcher-seen: %+v", s)
	}
	if v := s.View(); v.OpenCount() != 1 || v.AnsweredAt != nil {
		t.Fatalf("the questions were answered: %+v", v)
	}
}

// The seen step publishes a reported worker once, and the card it publishes is
// already seen, so no reader is shown the dot on the way.
func TestAReportedWorkersStopPublishesOnce(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	launcher, worker := launchedPair(t, d)
	reportToLauncher(t, d, worker)

	resp, err := http.Get("http://" + d.opts.HumanAddr + "/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	lines := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()
	// The stream is subscribed once a publish of our own comes back down it.
	deadline := time.Now().Add(3 * time.Second)
	for ready := false; !ready && time.Now().Before(deadline); {
		d.publishTask(launcher.ID)
		select {
		case l := <-lines:
			ready = strings.HasPrefix(l, "event: task")
		case <-time.After(50 * time.Millisecond):
		}
	}
	time.Sleep(100 * time.Millisecond)
	for len(lines) > 0 {
		<-lines
	}

	seenStop(t, d, "worker", nil)

	got := 0
	quiet := time.After(500 * time.Millisecond)
loop:
	for {
		select {
		case l := <-lines:
			if strings.HasPrefix(l, "data: ") && strings.Contains(l, worker.ID) {
				got++
			}
		case <-quiet:
			break loop
		}
	}
	// One: turnEnded moves the card to needs-input and publishes, the seen step
	// publishes after the mark, and the two land inside one coalescing window, so
	// the single event is sent from the row as it stands, already seen.
	if got != 1 {
		t.Fatalf("the Stop published the worker %d times, want one coalesced publish", got)
	}
	if v := seenCard(t, d, worker.ID); v == nil || v.Unseen || v.SeenVia != store.SeenLauncher {
		t.Fatalf("card after the Stop: %+v", v)
	}
}
