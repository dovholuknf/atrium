package shepherd

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeStore is the hub row, in memory. phases is every phase it was saved with, in order.
type fakeStore struct {
	mu     sync.Mutex
	row    State
	has    bool
	phases []Phase
}

func (f *fakeStore) Load(_ context.Context, _ string) (State, bool, error) { return f.row, f.has, nil }
func (f *fakeStore) Save(_ context.Context, st State) error {
	f.row, f.has = st, true
	f.phases = append(f.phases, st.Phase)
	return nil
}

// fakeRunner answers each command from a handler and records every one.
type fakeRunner struct {
	handle func(dir, line string) (Result, error)
	calls  []string
}

func (f *fakeRunner) Run(_ context.Context, dir, name string, args ...string) (Result, error) {
	line := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, line)
	return f.handle(dir, line)
}

func (f *fakeRunner) count(prefix string) int {
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

type fakeHub struct {
	item   BacklogItem
	states []string // the item's state on each read, the last repeating
	reads  int
	card   string
}

func (h *fakeHub) Backlog(context.Context, string) (BacklogItem, error) { return h.item, nil }
func (h *fakeHub) ItemForCard(context.Context, string) (WorkItem, bool, error) {
	i := h.reads
	if i >= len(h.states) {
		i = len(h.states) - 1
	}
	h.reads++
	return WorkItem{ID: "it-1", Card: h.card, State: h.states[i], Note: "need a key"}, true, nil
}
func (h *fakeHub) Profile(context.Context) (Profile, error) {
	return Profile{Name: "Op", Email: "op@x", Display: "Op"}, nil
}
func (h *fakeHub) CardStatus(context.Context, string) (string, error) { return "running", nil }

type fakeFiles struct{ files map[string][]byte }

func (f *fakeFiles) Read(p string) ([]byte, error) {
	if b, ok := f.files[p]; ok {
		return b, nil
	}
	return nil, errors.New("no file " + p)
}
func (f *fakeFiles) Write(p string, b []byte) error { f.files[p] = b; return nil }

type rig struct {
	store  *fakeStore
	runner *fakeRunner
	hub    *fakeHub
	files  *fakeFiles
	env    *Env
	slept  int
}

// newRig is a repo where everything works: the worker finished after two reads, with one commit.
func newRig() *rig {
	r := &rig{store: &fakeStore{}, files: &fakeFiles{files: map[string][]byte{}},
		hub: &fakeHub{item: BacklogItem{ID: "bug-x", Title: "fix x", Status: "open", Body: "fix `DoThing` in `parse`"},
			states: []string{"running", "finished"}, card: "card-1"}}
	r.runner = &fakeRunner{handle: func(dir, line string) (Result, error) {
		switch {
		case strings.HasPrefix(line, "git rev-parse claude/main"):
			return Result{Stdout: "base111\n"}, nil
		case strings.HasPrefix(line, "git worktree list"):
			return Result{}, nil
		case strings.HasPrefix(line, "git rev-parse --verify --quiet refs/heads/claude/bug-x"):
			// the branch exists once the worktree was added, or once a run before this one added it
			if r.store.has && r.store.row.Phase != PhaseCheck && r.store.row.Phase != PhaseWorktree {
				return Result{Stdout: "base111\n"}, nil
			}
			return Result{Code: 1}, nil
		case line == "git rev-parse HEAD" && strings.HasSuffix(dir, "/land-bug-x"):
			return Result{Stdout: "land222\n"}, nil
		case line == "git rev-parse HEAD":
			return Result{Stdout: "base111\n"}, nil
		case strings.HasPrefix(line, "atrium launch"):
			return Result{Stdout: "card-1\n"}, nil
		case strings.HasPrefix(line, "git log --oneline claude/main.."):
			return Result{Stdout: "abc1234 fix x\n"}, nil
		case strings.HasPrefix(line, "git log --oneline claude/main..HEAD"):
			return Result{Stdout: "abc1234 fix x\n"}, nil
		case strings.HasPrefix(line, "git diff --name-only"):
			return Result{Stdout: "docs/changes/bug-x.md\n"}, nil
		case strings.HasPrefix(line, "git diff --stat"):
			return Result{Stdout: " a.go | 2 +-\n"}, nil
		case strings.HasPrefix(line, "git grep"):
			return Result{Stdout: "internal/a.go:1\n"}, nil
		}
		return Result{}, nil
	}}
	r.files.files["/wt/land-bug-x/docs/changes/bug-x.md"] = []byte("\xef\xbb\xbfhello")
	r.env = &Env{Store: r.store, Runner: r.runner, Hub: r.hub, Files: r.files, Out: nil,
		Cfg:   Config{Repo: "/repo", Worktrees: "/wt", Atrium: "atrium", Rules: "RULES"},
		Sleep: func(ctx context.Context, _ time.Duration) error { r.slept++; return ctx.Err() }}
	return r
}

func TestABugItemGoesFromCheckToAHeldPacket(t *testing.T) {
	r := newRig()
	st, err := New(r.env).Run(context.Background(), "bug-x")
	if err != nil || st.Phase != PhaseHeld {
		t.Fatalf("phase %s err %v", st.Phase, err)
	}
	for _, want := range []string{`"commits"`, "abc1234 fix x", `"DoThing": 1`, `"name": "test-touched"`, `"land": "land222"`} {
		if !strings.Contains(st.Packet, want) {
			t.Errorf("packet lacks %s:\n%s", want, st.Packet)
		}
	}
	if string(r.files.files["/wt/land-bug-x/docs/changes/bug-x.md"]) != "hello" {
		t.Error("the BOM was not stripped")
	}
	if !strings.Contains(string(r.files.files["/wt/bug-x/BRIEF.md"]), "fix `DoThing`") {
		t.Error("the brief does not carry the item body")
	}
	if r.runner.count("atrium launch --isolated --item bug-x --item-owner human") != 1 {
		t.Errorf("launch calls: %v", r.runner.calls)
	}
	if r.slept != 1 {
		t.Errorf("waited %d times for a worker that finished on the second read", r.slept)
	}
}

// THE ROW IS WRITTEN BEFORE EACH PHASE: the first save of each phase is the phase itself.
func TestTheRowNamesEachPhaseBeforeItRuns(t *testing.T) {
	r := newRig()
	if _, err := New(r.env).Run(context.Background(), "bug-x"); err != nil {
		t.Fatal(err)
	}
	var order []Phase
	for _, p := range r.store.phases {
		if len(order) == 0 || order[len(order)-1] != p {
			order = append(order, p)
		}
	}
	want := []Phase{PhaseCheck, PhaseWorktree, PhaseLaunch, PhaseWait, PhaseCollect, PhaseStage, PhaseHold, PhaseHeld}
	if len(order) != len(want) {
		t.Fatalf("saved %v", order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("saved %v, want %v", order, want)
		}
	}
}

// A KILLED SHEPHERD RESUMES: the stage phase fails once, and the second run neither launches again nor redoes the
// worktree.
func TestAShepherdThatDiedInStageResumesThereAndLaunchesOnce(t *testing.T) {
	r := newRig()
	inner := r.runner.handle
	boom := true
	r.runner.handle = func(dir, line string) (Result, error) {
		if boom && strings.HasPrefix(line, "gofmt") {
			boom = false
			return Result{}, errors.New("killed")
		}
		return inner(dir, line)
	}
	st, err := New(r.env).Run(context.Background(), "bug-x")
	if err == nil || st.Phase != PhaseStage || r.store.row.Phase != PhaseStage {
		t.Fatalf("first run: phase %s row %s err %v", st.Phase, r.store.row.Phase, err)
	}
	if r.store.row.LastError == "" {
		t.Error("the error is not on the row")
	}
	// a new process: nothing but the row
	r.runner.calls = nil
	st, err = New(r.env).Run(context.Background(), "bug-x")
	if err != nil || st.Phase != PhaseHeld {
		t.Fatalf("second run: phase %s err %v", st.Phase, err)
	}
	if n := r.runner.count("atrium launch"); n != 0 {
		t.Errorf("the resumed shepherd launched %d more workers", n)
	}
	if n := r.runner.count("git worktree add -b claude/bug-x"); n != 0 {
		t.Errorf("the resumed shepherd made the worker's worktree again")
	}
	if r.hub.reads != 2 {
		t.Errorf("the resumed shepherd read the hub again: %d reads", r.hub.reads)
	}
}

// A SHEPHERD CANCELLED MID-WAIT keeps its row at wait and goes on from there.
func TestAShepherdCancelledWhileWaitingResumesInWait(t *testing.T) {
	r := newRig()
	r.hub.states = []string{"running", "running", "finished"}
	ctx, cancel := context.WithCancel(context.Background())
	r.env.Sleep = func(ctx context.Context, _ time.Duration) error { cancel(); return ctx.Err() }
	_, err := New(r.env).Run(ctx, "bug-x")
	if !errors.Is(err, context.Canceled) || r.store.row.Phase != PhaseWait || r.store.row.Card != "card-1" {
		t.Fatalf("row %+v err %v", r.store.row, err)
	}
	r.env.Sleep = func(ctx context.Context, _ time.Duration) error { return nil }
	st, err := New(r.env).Run(context.Background(), "bug-x")
	if err != nil || st.Phase != PhaseHeld {
		t.Fatalf("phase %s err %v", st.Phase, err)
	}
}

func TestABlockedWorkerHoldsWithItsAskAndResumesInWait(t *testing.T) {
	r := newRig()
	r.hub.states = []string{"running", "blocked"}
	st, err := New(r.env).Run(context.Background(), "bug-x")
	if err != nil || st.Phase != PhaseHeldBlocked || st.Packet != "need a key" {
		t.Fatalf("phase %s packet %q err %v", st.Phase, st.Packet, err)
	}
	r.hub.states = []string{"finished"}
	st, err = New(r.env).Run(context.Background(), "bug-x")
	if err != nil || st.Phase != PhaseHeld {
		t.Fatalf("phase %s err %v", st.Phase, err)
	}
}

func TestAHeldShepherdIsNotRun(t *testing.T) {
	r := newRig()
	r.store.row, r.store.has = State{ItemID: "bug-x", Phase: PhaseHeld}, true
	st, err := New(r.env).Run(context.Background(), "bug-x")
	if err != nil || st.Phase != PhaseHeld || len(r.runner.calls) != 0 {
		t.Fatalf("phase %s calls %v err %v", st.Phase, r.runner.calls, err)
	}
}

func TestAnItemAlreadyDoneIsRefused(t *testing.T) {
	r := newRig()
	r.hub.item.Status = "done"
	st, _ := New(r.env).Run(context.Background(), "bug-x")
	if st.Phase != PhaseRefused || r.runner.count("atrium launch") != 0 {
		t.Fatalf("phase %s", st.Phase)
	}
}

func TestAnEmptyRangeIsAnExceptionNotALanding(t *testing.T) {
	r := newRig()
	inner := r.runner.handle
	r.runner.handle = func(dir, line string) (Result, error) {
		if strings.HasPrefix(line, "git log --oneline claude/main..refs/heads") {
			return Result{}, nil
		}
		return inner(dir, line)
	}
	st, _ := New(r.env).Run(context.Background(), "bug-x")
	if st.Phase != PhaseException || !strings.Contains(st.LastError, "no commit") {
		t.Fatalf("phase %s %s", st.Phase, st.LastError)
	}
}

func TestAConflictAbortsThePickAndIsAnException(t *testing.T) {
	r := newRig()
	inner := r.runner.handle
	r.runner.handle = func(dir, line string) (Result, error) {
		if strings.HasPrefix(line, "git cherry-pick claude/main..") {
			return Result{Code: 1, Stderr: "CONFLICT in a.go"}, nil
		}
		return inner(dir, line)
	}
	st, _ := New(r.env).Run(context.Background(), "bug-x")
	if st.Phase != PhaseException || r.runner.count("git cherry-pick --abort") != 1 {
		t.Fatalf("phase %s calls %v", st.Phase, r.runner.calls)
	}
}

func TestAGofmtListingFailsTheStage(t *testing.T) {
	r := newRig()
	inner := r.runner.handle
	r.runner.handle = func(dir, line string) (Result, error) {
		if strings.HasPrefix(line, "gofmt") {
			return Result{Stdout: "internal/a.go\n"}, nil
		}
		return inner(dir, line)
	}
	st, _ := New(r.env).Run(context.Background(), "bug-x")
	if st.Phase != PhaseException || !strings.Contains(st.LastError, "gofmt failed") {
		t.Fatalf("phase %s %s", st.Phase, st.LastError)
	}
}

func TestABudgetMissIsRerunOnceAndRecorded(t *testing.T) {
	r := newRig()
	inner := r.runner.handle
	runs := 0
	r.runner.handle = func(dir, line string) (Result, error) {
		if strings.HasPrefix(line, "bash scripts/test-touched.sh") {
			if runs++; runs == 1 {
				return Result{Code: 1, Stdout: "FAIL: unit budget: TestDisk took 25ms"}, nil
			}
		}
		return inner(dir, line)
	}
	st, _ := New(r.env).Run(context.Background(), "bug-x")
	if st.Phase != PhaseHeld || runs != 2 || !strings.Contains(st.Packet, `"rerun": true`) {
		t.Fatalf("phase %s runs %d\n%s", st.Phase, runs, st.Packet)
	}
}

func TestAKnownFailingTestDoesNotFailTheStageButAnUnknownOneDoes(t *testing.T) {
	for name, tc := range map[string]struct {
		test string
		want Phase
	}{"known": {"TestRealSessionsKeepTheirText", PhaseHeld}, "unknown": {"TestMine", PhaseException}} {
		t.Run(name, func(t *testing.T) {
			r := newRig()
			r.files.files["/repo/docs/known-failing-tests.md"] = []byte("- `TestRealSessionsKeepTheirText`\n")
			inner := r.runner.handle
			r.runner.handle = func(dir, line string) (Result, error) {
				if strings.HasPrefix(line, "bash scripts/test-touched.sh") {
					return Result{Code: 1, Stdout: "--- FAIL: " + tc.test + " (0.00s)"}, nil
				}
				return inner(dir, line)
			}
			if st, _ := New(r.env).Run(context.Background(), "bug-x"); st.Phase != tc.want {
				t.Fatalf("phase %s", st.Phase)
			}
		})
	}
}
