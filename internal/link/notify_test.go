package link

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestMain doubles as the notify command: the test binary run with
// ATRIUM_NOTIFY_HELPER set is a program that records what it was given, which
// is portable where a shell script is not.
func TestMain(m *testing.M) {
	if mode := os.Getenv("ATRIUM_NOTIFY_HELPER"); mode != "" {
		os.Exit(notifyHelper(mode))
	}
	os.Exit(m.Run())
}

func notifyHelper(mode string) int {
	switch mode {
	case "record":
		stdin, _ := io.ReadAll(os.Stdin)
		rec := map[string]any{
			"name": os.Getenv("ATRIUM_NOTIFY_NAME"), "reason": os.Getenv("ATRIUM_NOTIFY_REASON"),
			"card": os.Getenv("ATRIUM_NOTIFY_CARD"), "room": os.Getenv("ATRIUM_NOTIFY_ROOM"),
			"stdin": string(stdin), "args": os.Args[1:],
		}
		line, _ := json.Marshal(rec)
		f, err := os.OpenFile(os.Getenv("ATRIUM_NOTIFY_OUT"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return 9
		}
		defer f.Close()
		fmt.Fprintln(f, string(line))
		return 0
	case "fail":
		fmt.Fprintln(os.Stderr, "the phone is unreachable")
		fmt.Fprintln(os.Stderr, "second line")
		return 3
	case "sleep":
		time.Sleep(time.Minute)
		return 0
	case "flood":
		chunk := bytes.Repeat([]byte("x"), 4096)
		for i := 0; i < 2048; i++ {
			if _, err := os.Stdout.Write(chunk); err != nil {
				return 0
			}
		}
		return 0
	}
	return 1
}

// fakeStore is NotifyStore with the semantics of hubstore.NotifyRecord.
type fakeStore struct {
	mu       sync.Mutex
	settings map[string]string
	rows     map[string]map[string]string
	cache    map[string][]CardState
	records  int
}

func newFakeStore() *fakeStore {
	return &fakeStore{settings: map[string]string{}, rows: map[string]map[string]string{},
		cache: map[string][]CardState{}}
}

func (f *fakeStore) Setting(n string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.settings[n], nil
}

func (f *fakeStore) SetSetting(n, v string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settings[n] = v
	return nil
}

func (f *fakeStore) Record(room string, ids map[string]string, _ []string, silent bool) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records++
	if f.rows[room] == nil {
		f.rows[room] = map[string]string{}
	}
	var fire []string
	for id, ident := range ids {
		if f.rows[room][id] == ident {
			continue
		}
		f.rows[room][id] = ident
		if !silent {
			fire = append(fire, id)
		}
	}
	return fire, nil
}

func (f *fakeStore) Rooms() ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for r := range f.cache {
		out = append(out, r)
	}
	return out, nil
}

func (f *fakeStore) Cards(room string) ([]CardState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cache[room], nil
}

func (f *fakeStore) Prune() error { return nil }

// recSink records what it is given, and can be made to fail or to block.
type recSink struct {
	mu    sync.Mutex
	got   []Notice
	fail  bool
	block chan struct{}
}

func (r *recSink) Send(ctx context.Context, n Notice) Result {
	if r.block != nil {
		select {
		case <-r.block:
		case <-ctx.Done():
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, n)
	if r.fail {
		return Result{ExitCode: 1, Err: "nope"}
	}
	return Result{OK: true}
}

func (r *recSink) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.got)
}

func until(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func settle() { time.Sleep(150 * time.Millisecond) }

func ncard(id, extra string) CardState {
	body := fmt.Sprintf(`{"id":%q,"title":"title-%s"%s}`, id, id, extra)
	return CardState{ID: id, Payload: json.RawMessage(body)}
}

func permCard(id, since string) CardState {
	return ncard(id, `,"status":"needs-permission","waiting_since":"`+since+`"`)
}

// armed is a notifier with a recording sink, switched on and seeded.
func armed(t *testing.T) (*Notifier, *fakeStore, *recSink) {
	t.Helper()
	st := newFakeStore()
	n := NewNotifier(st)
	rs := &recSink{}
	n.SetSink(rs)
	ctx, stop := context.WithCancel(context.Background())
	t.Cleanup(stop)
	n.Start(ctx)
	if err := n.Configure(true, []string{os.Args[0]}); err != nil {
		t.Fatal(err)
	}
	return n, st, rs
}

// ── identity ────────────────────────────────────────────

func TestNotifyIdentityPerReasonAndPriority(t *testing.T) {
	cases := []struct {
		name, payload, reason, ident string
	}{
		{"permission", `{"status":"needs-permission","waiting_since":"T1"}`, "permission", "a|permission|T1"},
		// No seen row at all is a room on an older build, which cannot say, so
		// input notifies as it always did.
		{"input", `{"status":"needs-input","waiting_since":"T2"}`, "input", "a|input|T2"},
		// A card that has never finished a turn is not news: whoever launched it
		// is already there or gave it its prompt.
		{"input before any turn", `{"status":"needs-input","waiting_since":"T2","seen":{"unseen":false}}`, "", ""},
		// A room that could not read its seen table sends null, which is "cannot
		// say", so input notifies rather than going quiet.
		{"input with seen null", `{"status":"needs-input","waiting_since":"T2","seen":null}`, "input", "a|input|T2"},
		{"input after a turn", `{"status":"needs-input","waiting_since":"T2","seen":{"turn_ended_at":"T1"}}`,
			"input", "a|input|T2"},
		// Permission is asked whatever the turns say.
		{"permission before any turn", `{"status":"needs-permission","waiting_since":"T1","seen":{}}`,
			"permission", "a|permission|T1"},
		{"question", `{"status":"running","seen":{"questions_at":"T3","open_questions":["q?"]}}`, "question", "a|question|T3"},
		// Questions whose text could not be read are still owed.
		{"unparsed questions", `{"status":"running","seen":{"questions_at":"T3","questions_unparsed":true}}`,
			"question", "a|question|T3"},
		{"finished", `{"status":"done","seen":{"turn_ended_at":"T4","unseen":true}}`, "finished", "a|finished|T4"},
		// A question with nothing open is not a question.
		{"empty questions", `{"status":"done","seen":{"questions_at":"T3","open_questions":[]}}`, "", ""},
		// A finished turn already seen is not news.
		{"seen", `{"status":"done","seen":{"turn_ended_at":"T4","unseen":false}}`, "", ""},
		{"running", `{"status":"running"}`, "", ""},
		// The shape this used to read: the fields at the top level, which no
		// payload carries. Nothing fires from them.
		{"top level is not read", `{"status":"done","turn_ended_at":"T4","unseen":true}`, "", ""},
		// Priority: permission over question over input over finished.
		{"all four", `{"status":"needs-permission","waiting_since":"T1","seen":{"questions_at":"T3",` +
			`"open_questions":["q"],"turn_ended_at":"T4","unseen":true}}`, "permission", "a|permission|T1"},
		{"question over input", `{"status":"needs-input","waiting_since":"T2","seen":{"questions_at":"T3",` +
			`"open_questions":["q"]}}`, "question", "a|question|T3"},
		{"input over finished", `{"status":"needs-input","waiting_since":"T2","seen":{"turn_ended_at":"T4",` +
			`"unseen":true}}`, "input", "a|input|T2"},
		// The item 44 rule.
		{"origin agent", `{"status":"needs-permission","waiting_since":"T1","tags":["dept:x","origin:agent"]}`, "", ""},
	}
	for _, c := range cases {
		got, ok := NotifyIdentity("a", json.RawMessage(c.payload))
		if c.reason == "" {
			if ok {
				t.Errorf("%s: wanted no reason, got %+v", c.name, got)
			}
			continue
		}
		if !ok || got.Reason != c.reason || got.Identity != c.ident {
			t.Errorf("%s: got %+v ok=%v, want %s %s", c.name, got, ok, c.reason, c.ident)
		}
	}
}

func TestNotifyNameIsTheAliasElseTheTitle(t *testing.T) {
	a, _ := NotifyIdentity("id1", json.RawMessage(`{"status":"needs-input","title":"T","alias":"sa89","waiting_since":"x"}`))
	b, _ := NotifyIdentity("id1", json.RawMessage(`{"status":"needs-input","title":"T","waiting_since":"x"}`))
	if a.Name != "sa89" || b.Name != "T" {
		t.Fatalf("names %q and %q", a.Name, b.Name)
	}
}

// ── the trigger ─────────────────────────────────────────

func TestNotifyFirstAnnouncementIsSeededSilently(t *testing.T) {
	n, _, rs := armed(t)
	n.Announced("sparta", []CardState{permCard("a", "T1")})
	settle()
	if rs.count() != 0 {
		t.Fatalf("a room's first announcement notified %d times", rs.count())
	}
	// The second, with a new card, does.
	n.Announced("sparta", []CardState{permCard("a", "T1"), permCard("b", "T2")})
	until(t, "the new card's notice", func() bool { return rs.count() == 1 })
	if got := rs.got[0]; got.Card != "sparta~b" || got.Reason != "permission" || got.Room != "sparta" {
		t.Fatalf("notice %+v", got)
	}
}

func TestNotifySameIdentityRunsOnce(t *testing.T) {
	n, _, rs := armed(t)
	n.Announced("sparta", nil)
	for i := 0; i < 5; i++ {
		n.Announced("sparta", []CardState{permCard("a", "T1")})
	}
	until(t, "one notice", func() bool { return rs.count() == 1 })
	settle()
	if rs.count() != 1 {
		t.Fatalf("the same identity notified %d times", rs.count())
	}
	// A second permission spell is a new identity.
	n.Announced("sparta", []CardState{permCard("a", "T9")})
	until(t, "the second spell", func() bool { return rs.count() == 2 })
}

func TestNotifyOriginAgentIsSkipped(t *testing.T) {
	n, _, rs := armed(t)
	n.Announced("sparta", nil)
	n.Announced("sparta", []CardState{ncard("a", `,"status":"needs-permission","waiting_since":"T","tags":["origin:agent"]`)})
	settle()
	if rs.count() != 0 {
		t.Fatalf("an origin:agent card notified")
	}
}

func TestNotifyCardLeavingAndReturningDoesNotRefire(t *testing.T) {
	n, _, rs := armed(t)
	n.Announced("sparta", nil)
	n.Announced("sparta", []CardState{permCard("a", "T1")})
	until(t, "the first notice", func() bool { return rs.count() == 1 })
	n.Announced("sparta", nil)
	n.Announced("sparta", []CardState{permCard("a", "T1")})
	settle()
	if rs.count() != 1 {
		t.Fatalf("a returning card notified again: %d", rs.count())
	}
}

func TestNotifyTurningItOnSeedsWhatIsAlreadyWaiting(t *testing.T) {
	st := newFakeStore()
	st.cache["sparta"] = []CardState{permCard("a", "T1")}
	n := NewNotifier(st)
	rs := &recSink{}
	n.SetSink(rs)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	n.Start(ctx)
	// Off: an announcement does nothing.
	n.Announced("sparta", []CardState{permCard("a", "T1")})
	if st.records != 0 {
		t.Fatal("a disabled notifier recorded")
	}
	if err := n.Configure(true, []string{os.Args[0]}); err != nil {
		t.Fatal(err)
	}
	n.Announced("sparta", []CardState{permCard("a", "T1")})
	settle()
	if rs.count() != 0 {
		t.Fatalf("turning it on announced what was already waiting: %d", rs.count())
	}
}

func TestNotifySuppressedWhileATabIsVisibleButStored(t *testing.T) {
	n, st, rs := armed(t)
	n.Announced("sparta", nil)
	n.Presence().Set("tab1", true)
	n.Announced("sparta", []CardState{permCard("a", "T1")})
	settle()
	if rs.count() != 0 {
		t.Fatal("notified while a tab was visible")
	}
	if st.rows["sparta"]["a"] == "" {
		t.Fatal("the identity was not stored while suppressed")
	}
	// Hidden again: the stored identity does not fire late.
	n.Presence().Set("tab1", false)
	n.Announced("sparta", []CardState{permCard("a", "T1")})
	settle()
	if rs.count() != 0 {
		t.Fatal("a suppressed identity fired when the tab hid")
	}
	if s := n.Status(); s.Suppressed || s.VisibleTabs != 0 {
		t.Fatalf("status %+v", s)
	}
}

// ── presence ────────────────────────────────────────────

func TestPresenceTabClearsWhenItsStreamCloses(t *testing.T) {
	p := newPresence()
	clock := time.Now()
	p.now = func() time.Time { return clock }
	end := p.Open("t1")
	p.Set("t1", true)
	if p.Count() != 1 {
		t.Fatal("a visible tab is not counted")
	}
	end()
	if p.Count() != 1 {
		t.Fatal("a tab lost its visibility the instant its stream closed, before a reconnect could hold it")
	}
	clock = clock.Add(streamGrace + time.Second)
	if p.Count() != 0 {
		t.Fatal("a tab whose stream closed is still visible after the grace")
	}
}

// A STREAM THAT RECONNECTS IS THE SAME TAB. The board does not post visible
// again after a reconnect, so the tab has to survive the gap (f-025).
func TestPresenceSurvivesAStreamReconnect(t *testing.T) {
	p := newPresence()
	clock := time.Now()
	p.now = func() time.Time { return clock }
	end := p.Open("t1")
	p.Set("t1", true)
	end()
	clock = clock.Add(2 * time.Second)
	p.Open("t1")
	clock = clock.Add(time.Hour)
	if p.Count() != 1 {
		t.Fatal("a tab whose stream reconnected was dropped")
	}
}

func TestPresenceBackstopExpiresATabWithNoStream(t *testing.T) {
	p := newPresence()
	clock := time.Now()
	p.now = func() time.Time { return clock }
	p.Set("t1", true)
	clock = clock.Add(9 * time.Minute)
	if p.Count() != 1 {
		t.Fatal("expired before ten minutes")
	}
	clock = clock.Add(2 * time.Minute)
	if p.Count() != 0 {
		t.Fatal("a tab with no stream outlived the ten minute backstop")
	}
}

func TestPresenceAStreamedTabDoesNotExpire(t *testing.T) {
	p := newPresence()
	clock := time.Now()
	p.now = func() time.Time { return clock }
	p.Open("t1")
	p.Set("t1", true)
	clock = clock.Add(3 * time.Hour)
	if p.Count() != 1 {
		t.Fatal("a tab with a live stream expired")
	}
}

func TestPresenceRefusesAJunkTabID(t *testing.T) {
	p := newPresence()
	p.Set("a b", true)
	p.Set(strings.Repeat("x", 500), true)
	p.Set("", true)
	if p.Count() != 0 {
		t.Fatal("a junk tab id was accepted")
	}
}

// ── the sink ────────────────────────────────────────────

func recordingCommand(t *testing.T) (CommandSink, string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out.jsonl")
	t.Setenv("ATRIUM_NOTIFY_HELPER", "record")
	t.Setenv("ATRIUM_NOTIFY_OUT", out)
	// THE HELPER IS THIS WHOLE TEST BINARY, started as a child, and on a loaded
	// machine that start alone can pass the sink's ten seconds. These tests are
	// about what the command is given, not how long it may take, so the bound is
	// taken out of the way. The timeout has its own test. Safe to change here
	// because t.Setenv above already rules out a parallel test.
	was := notifyTimeout
	notifyTimeout = 2 * time.Minute
	t.Cleanup(func() { notifyTimeout = was })
	return CommandSink{Argv: func() []string { return []string{os.Args[0]} }}, out
}

func TestCommandSinkGivesFourFieldsInEnvAndStdinAndNoneInArgv(t *testing.T) {
	sink, out := recordingCommand(t)
	nasty := "x; rm -rf / $(whoami) `id` \"q\""
	res := sink.Send(context.Background(), Notice{Name: nasty, Reason: "question", Card: "sparta~01a", Room: "sparta"})
	if !res.OK || res.ExitCode != 0 {
		t.Fatalf("result %+v", res)
	}
	raw, _ := os.ReadFile(out)
	var rec struct {
		Name, Reason, Card, Room, Stdin string
		Args                            []string
	}
	if err := json.Unmarshal(bytes.TrimSpace(raw), &rec); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	if rec.Name != nasty || rec.Reason != "question" || rec.Card != "sparta~01a" || rec.Room != "sparta" {
		t.Fatalf("env %+v", rec)
	}
	var line map[string]string
	if err := json.Unmarshal([]byte(rec.Stdin), &line); err != nil {
		t.Fatalf("stdin is not one JSON line: %q", rec.Stdin)
	}
	if len(line) != 4 || line["name"] != nasty || line["reason"] != "question" ||
		line["card"] != "sparta~01a" || line["room"] != "sparta" {
		t.Fatalf("stdin %v", line)
	}
	if len(rec.Args) != 0 {
		t.Fatalf("the card reached argv: %v", rec.Args)
	}
}

func TestCommandSinkTimeoutKillsTheProcess(t *testing.T) {
	t.Setenv("ATRIUM_NOTIFY_HELPER", "sleep")
	was := notifyTimeout
	notifyTimeout = 300 * time.Millisecond
	defer func() { notifyTimeout = was }()
	began := time.Now()
	res := CommandSink{Argv: func() []string { return []string{os.Args[0]} }}.Send(context.Background(), Notice{})
	if res.OK || !strings.Contains(res.Err, "longer than") {
		t.Fatalf("result %+v", res)
	}
	if took := time.Since(began); took > 10*time.Second {
		t.Fatalf("a timed out command held the sink for %s", took)
	}
}

func TestCommandSinkFailureCarriesTheFirstLineOfStderr(t *testing.T) {
	t.Setenv("ATRIUM_NOTIFY_HELPER", "fail")
	res := CommandSink{Argv: func() []string { return []string{os.Args[0]} }}.Send(context.Background(), Notice{})
	if res.OK || res.ExitCode != 3 || res.Err != "the phone is unreachable" {
		t.Fatalf("result %+v", res)
	}
}

func TestCommandSinkOutputIsCappedWhileReading(t *testing.T) {
	t.Setenv("ATRIUM_NOTIFY_HELPER", "flood")
	res := CommandSink{Argv: func() []string { return []string{os.Args[0]} }}.Send(context.Background(), Notice{})
	if !res.OK {
		t.Fatalf("result %+v", res)
	}
	if len(res.Output) > notifyStdoutLimit {
		t.Fatalf("kept %d bytes of output, limit %d", len(res.Output), notifyStdoutLimit)
	}
}

// ── failure ─────────────────────────────────────────────

func TestNotifyThreeFailuresSwitchItOffWithTheReason(t *testing.T) {
	n, st, rs := armed(t)
	rs.fail = true
	n.Announced("sparta", nil)
	for i := 1; i <= 3; i++ {
		n.Announced("sparta", []CardState{permCard(fmt.Sprintf("c%d", i), "T")})
		until(t, "a run", func() bool { return rs.count() == i })
	}
	until(t, "disabled", func() bool { return !n.Status().Enabled })
	s := n.Status()
	if !strings.Contains(s.DisabledReason, "nope") || s.Failures != 3 {
		t.Fatalf("status %+v", s)
	}
	if st.settings["notify.enabled"] != "off" || st.settings["notify.disabled_reason"] == "" {
		t.Fatalf("not persisted: %v", st.settings)
	}
	// And it survives a restart.
	again := NewNotifier(st)
	if again.Status().Enabled || again.Status().DisabledReason == "" {
		t.Fatalf("a restart forgot why: %+v", again.Status())
	}
	// Turning it on again clears the reason.
	if err := again.Configure(true, []string{os.Args[0]}); err != nil {
		t.Fatal(err)
	}
	if s := again.Status(); s.DisabledReason != "" || s.Failures != 0 || !s.Enabled {
		t.Fatalf("status %+v", s)
	}
}

func TestNotifyASuccessResetsTheFailureCount(t *testing.T) {
	n, _, rs := armed(t)
	n.Announced("sparta", nil)
	rs.fail = true
	n.Announced("sparta", []CardState{permCard("a", "T")})
	until(t, "a failure", func() bool { return n.Status().Failures == 1 })
	rs.mu.Lock()
	rs.fail = false
	rs.mu.Unlock()
	n.Announced("sparta", []CardState{permCard("b", "T")})
	until(t, "a success", func() bool { return n.Status().Failures == 0 && n.Status().Sent == 1 })
}

// ── never delaying anything ─────────────────────────────

func TestNotifyAQueueDropsTheOldestAndAnnounceNeverBlocks(t *testing.T) {
	n, _, rs := armed(t)
	rs.block = make(chan struct{})
	n.Announced("sparta", nil)
	began := time.Now()
	var cards []CardState
	for i := 0; i < notifyQueueMax*3; i++ {
		cards = append(cards, permCard(fmt.Sprintf("c%03d", i), "T"))
	}
	n.Announced("sparta", cards)
	if took := time.Since(began); took > 2*time.Second {
		t.Fatalf("announce took %s with the sink stuck", took)
	}
	s := n.Status()
	// One is in the worker's hands, the queue holds its bound.
	if s.Dropped < notifyQueueMax {
		t.Fatalf("dropped %d, want at least %d", s.Dropped, notifyQueueMax)
	}
	close(rs.block)
	until(t, "the queue to drain", func() bool { return rs.count() >= notifyQueueMax })
}

// ── the endpoints ───────────────────────────────────────

func notifyProxy(t *testing.T) (*httptest.Server, *Notifier, *fakeStore) {
	t.Helper()
	n, st, _ := armed(t)
	p := NewProxy(NewHub(Timings{}), nil, "", nil)
	p.SetNotify(n)
	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)
	return srv, n, st
}

func do(t *testing.T, method, url, ctype, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	raw, _ := io.ReadAll(res.Body)
	_ = json.Unmarshal(raw, &out)
	return res.StatusCode, out
}

func TestNotifyPutValidation(t *testing.T) {
	srv, _, _ := notifyProxy(t)
	url := srv.URL + "/_hub/notify"
	for name, body := range map[string]string{
		"empty array":      `{"enabled":true,"command":[]}`,
		"no command":       `{"enabled":true}`,
		"not an array":     `{"enabled":true,"command":"ntfy"}`,
		"numbers":          `{"enabled":true,"command":[1,2]}`,
		"unfindable":       `{"enabled":true,"command":["definitely-not-a-program-zzz"]}`,
		"absolute missing": `{"enabled":true,"command":["` + filepath.ToSlash(filepath.Join(t.TempDir(), "nope")) + `"]}`,
		"blank first":      `{"enabled":true,"command":[" "]}`,
	} {
		if code, _ := do(t, http.MethodPut, url, "application/json", body); code != http.StatusBadRequest {
			t.Errorf("%s: answered %d, want 400", name, code)
		}
	}
	exe, _ := json.Marshal(os.Args[0])
	code, got := do(t, http.MethodPut, url, "application/json", `{"enabled":true,"command":[`+string(exe)+`,"--flag"]}`)
	if code != http.StatusOK || got["enabled"] != true {
		t.Fatalf("a good command answered %d %v", code, got)
	}
	_, got = do(t, http.MethodGet, url, "", "")
	if cmd, _ := got["command"].([]any); len(cmd) != 2 {
		t.Fatalf("GET %v", got)
	}
	for _, k := range []string{"enabled", "command", "last_run_at", "last_ok_at", "last_error", "failures",
		"disabled_reason", "sent", "dropped", "suppressed", "visible_tabs"} {
		if _, ok := got[k]; !ok {
			t.Errorf("GET is missing %q", k)
		}
	}
}

func TestNotifyTestRunsOnceAndIsNotSuppressed(t *testing.T) {
	srv, n, _ := notifyProxy(t)
	_, out := recordingCommand(t)
	exe, _ := json.Marshal(os.Args[0])
	do(t, http.MethodPut, srv.URL+"/_hub/notify", "application/json", `{"enabled":true,"command":[`+string(exe)+`]}`)
	n.Presence().Set("tab1", true)
	n.SetSink(CommandSink{Argv: func() []string { return []string{os.Args[0]} }})
	code, got := do(t, http.MethodPost, srv.URL+"/_hub/notify/test", "", "")
	if code != http.StatusOK || got["ok"] != true || got["exit_code"] != float64(0) {
		t.Fatalf("test answered %d %v", code, got)
	}
	if _, ok := got["took_ms"]; !ok {
		t.Fatal("no took_ms")
	}
	raw, _ := os.ReadFile(out)
	if !strings.Contains(string(raw), `"reason":"test"`) || strings.Count(string(raw), "\n") != 1 {
		t.Fatalf("the test card was not run once: %s", raw)
	}
}

func TestNotifyTestWithNoCommandIsAConflict(t *testing.T) {
	srv, n, _ := notifyProxy(t)
	if err := n.Configure(false, nil); err != nil {
		t.Fatal(err)
	}
	if code, _ := do(t, http.MethodPost, srv.URL+"/_hub/notify/test", "", ""); code != http.StatusConflict {
		t.Fatalf("answered %d", code)
	}
}

func TestPresenceEndpointTakesTextPlainAndTheStreamClearsIt(t *testing.T) {
	srv, n, _ := notifyProxy(t)
	// The board opens its stream with the tab id.
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/v1/events/hub?tab=tab-9", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	until(t, "the stream to register", func() bool {
		n.pres.mu.Lock()
		defer n.pres.mu.Unlock()
		return n.pres.streams["tab-9"] == 1
	})
	// sendBeacon posts text/plain.
	if code, _ := do(t, http.MethodPost, srv.URL+"/_hub/presence", "text/plain;charset=UTF-8",
		`{"visible":true,"tab":"tab-9"}`); code != http.StatusOK {
		t.Fatalf("presence answered %d", code)
	}
	if s := n.Status(); !s.Suppressed || s.VisibleTabs != 1 {
		t.Fatalf("status %+v", s)
	}
	cancel()
	until(t, "the closed stream to end", func() bool {
		n.pres.mu.Lock()
		defer n.pres.mu.Unlock()
		return n.pres.streams["tab-9"] == 0
	})
	// Past the reconnect grace, the tab is gone.
	n.pres.mu.Lock()
	n.pres.now = func() time.Time { return time.Now().Add(streamGrace + time.Minute) }
	n.pres.mu.Unlock()
	until(t, "the closed stream to clear the tab", func() bool { return n.Status().VisibleTabs == 0 })
}

// A hub serves no guest listener, so nothing a lent session can dial ends at
// these routes. This pins that from the other side: without a notifier wired
// they do not exist, and presence is accepted and ignored.
func TestNotifyRoutesAreAbsentWithoutANotifier(t *testing.T) {
	srv := httptest.NewServer(NewProxy(NewHub(Timings{}), nil, "", nil))
	defer srv.Close()
	if code, _ := do(t, http.MethodGet, srv.URL+"/_hub/notify", "", ""); code != http.StatusNotFound {
		t.Fatalf("notify answered %d", code)
	}
	if code, _ := do(t, http.MethodPut, srv.URL+"/_hub/notify", "application/json",
		`{"enabled":true,"command":["x"]}`); code != http.StatusNotFound {
		t.Fatalf("PUT answered %d", code)
	}
	if code, _ := do(t, http.MethodPost, srv.URL+"/_hub/presence", "text/plain", `{"visible":true,"tab":"t"}`); code != http.StatusOK {
		t.Fatalf("presence answered %d", code)
	}
}

// ── /v1/tasks/<id>/replies passes through to the owning room ─

// @runtime adds GET /v1/tasks/{id}/replies?n= on the room. The hub already
// routes /v1/tasks/<id>/... to the card's room, and this locks in that the
// query string survives the hop, for a tagged id and for a bare one.
func TestRepliesReachTheOwningRoomWithTheQueryIntact(t *testing.T) {
	room := func(name string, id string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/v1/tasks/"+id {
				fmt.Fprint(w, `{"id":"`+id+`"}`)
				return
			}
			if r.URL.Path != "/v1/tasks/"+id+"/replies" {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"error":"no"}`)
				return
			}
			fmt.Fprintf(w, `{"served_by":%q,"path":%q,"query":%q}`, name, r.URL.Path, r.URL.RawQuery)
		})
	}
	front, _, done := two(t, room("alpha", "acard"), room("beta", "bcard"))
	defer done()
	for _, path := range []string{"/v1/tasks/beta~bcard/replies?n=3", "/v1/tasks/bcard/replies?n=3"} {
		res, err := http.Get(front.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(res.Body)
		res.Body.Close()
		var plain map[string]string
		_ = json.Unmarshal(raw, &plain)
		if res.StatusCode != http.StatusOK || plain["served_by"] != "beta" ||
			plain["path"] != "/v1/tasks/bcard/replies" || plain["query"] != "n=3" {
			t.Fatalf("%s answered %d %s", path, res.StatusCode, raw)
		}
	}
}
