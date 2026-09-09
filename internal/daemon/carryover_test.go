package daemon

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// A runner with a ring and no terminal. Everything about carrying scrollback
// across a restart happens in the buffer, so none of these need a pty.
func carryRunner(id string, size, cols int) *runner {
	return &runner{
		taskID:   id,
		buf:      newRing(size, cols),
		watchers: map[chan []byte]struct{}{},
		done:     make(chan struct{}),
	}
}

// WHAT WAS ON SCREEN BEFORE THE RESTART SURVIVES ON DISK, and is not put back
// into the terminal on its own.
//
// This asserted the replay for one afternoon. Joining it onto the front of
// every attach was confusing in a way that took a while to name: a resumed
// session REPRINTS its own recent history, so the carried bytes ended mid
// conversation and the same conversation started again below the divider. Two
// copies of the last hour with nothing on screen to say why.
//
// A terminal running claude shows one session's output. So does this now, and
// the older bytes are fetched by somebody who asks for them.
func TestCarryoverIsKeptOnDiskAndNotReplayedIntoTheTerminal(t *testing.T) {
	dir := t.TempDir()
	before := carryRunner("card1", 4096, 100)
	before.buf.Write([]byte("what I was doing before the restart\r\n"))

	if err := writeCarry(dir, "card1", before.carryFrom(4096)); err != nil {
		t.Fatal(err)
	}

	// It is still read at spawn, which is what makes the file cumulative: the
	// next stop folds this generation into the one it writes, so asking for
	// the older scrollback reaches back further than one restart.
	after := carryRunner("card1", 4096, 100)
	after.carried = readCarry(dir, "card1", 4096)
	if after.carried == nil {
		t.Fatal("nothing came back off disk, so the next save has nothing to fold in")
	}
	after.buf.Write([]byte("and what the new process says\r\n"))

	backlog, _, _, _, ch := after.subscribe()
	after.unsubscribe(ch)
	if bytes.Contains(backlog, []byte("before the restart")) {
		t.Fatalf("pushed the previous session at somebody who did not ask: %q", backlog)
	}
	if !bytes.Contains(backlog, []byte("the new process")) {
		t.Fatalf("lost this session's own output: %q", backlog)
	}

	// And the next save carries both, so nothing is lost by not showing it.
	next := after.carryFrom(4096)
	if next == nil {
		t.Fatal("saved nothing")
	}
	if !bytes.Contains(next.bytes, []byte("before the restart")) {
		t.Fatalf("the older generation was dropped rather than carried: %q", next.bytes)
	}
	if !bytes.Contains(next.bytes, []byte("the new process")) {
		t.Fatalf("this generation was not added: %q", next.bytes)
	}
	if !bytes.Contains(next.bytes, carryDivider) {
		t.Fatal("no restart boundary between them, so the join reads as a repeat")
	}
}

// AND IT IS SERVED AS PLAIN TEXT, because it opens in a browser tab and a tab
// is not a terminal. Colour codes would render as `[38;5;244m` throughout.
func TestTheOlderScrollbackIsServedWithoutEscapes(t *testing.T) {
	d := testDaemon(t)
	r := carryRunner("card1", 4096, 100)
	r.buf.Write([]byte("\x1b[38;5;244mcoloured output\x1b[0m\r\n\x1b[Hand a redraw over it\r\n"))
	if err := writeCarry(d.carryDir(), "card1", r.carryFrom(4096)); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/tasks/card1/scrollback/older", nil)
	req.SetPathValue("id", "card1")
	rec := httptest.NewRecorder()
	d.handleOlderScrollback(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("answered %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "\x1b") {
		t.Fatalf("left escape sequences in a plain text answer: %q", body)
	}
	for _, want := range []string{"coloured output", "and a redraw over it"} {
		if !strings.Contains(body, want) {
			t.Fatalf("lost %q: %q", want, body)
		}
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("served it as %q", ct)
	}
}

// A CARD WITH NOTHING SAVED SAYS SO, and says why, because "it is written when
// atrium is stopped" is the whole answer and nobody can guess it.
func TestTheOlderScrollbackSaysWhenThereIsNone(t *testing.T) {
	d := testDaemon(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/tasks/card1/scrollback/older", nil)
	req.SetPathValue("id", "card1")
	rec := httptest.NewRecorder()
	d.handleOlderScrollback(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("answered %d for a card with no saved scrollback", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "clean stop") {
		t.Fatalf("did not say why there is none: %q", rec.Body.String())
	}
}

// A WIDTH CHANGE NO LONGER LOSES THE SAVE, which was the refusal this file was
// written with and the reason a resized session came back empty.
//
// The argument for refusing was that a file holds one width and cannot carry a
// warning to whoever opens it tomorrow. What it actually did was throw away
// every restart's scrollback for any session that had been resized, which is
// most of them.
func TestCarryoverIsSavedWhateverWidthItWasDrawnAt(t *testing.T) {
	dir := t.TempDir()
	before := carryRunner("card1", 4096, 80)
	before.buf.Write([]byte("eighty column output\r\n"))
	before.buf.SetWidth(200)
	before.buf.Write([]byte("two hundred column output\r\n"))

	c := before.carryFrom(4096)
	if c == nil {
		t.Fatal("saved nothing for a session that had been resized")
	}
	for _, want := range []string{"eighty column output", "two hundred column output"} {
		if !bytes.Contains(c.bytes, []byte(want)) {
			t.Fatalf("lost %q: %q", want, c.bytes)
		}
	}
	if err := writeCarry(dir, "card1", c); err != nil {
		t.Fatal(err)
	}
	if got := readCarry(dir, "card1", 4096); got == nil {
		t.Fatal("what was written could not be read back")
	}
}

// A RESTART AFTER A RESIZE STILL SAVES THE SCROLLBACK.
//
// This asserted that nothing was saved, which followed from `carryFrom` asking
// for the run composed at the width in force: a resize with no output after it
// makes that run empty. So the shape that was already the ring's worst bug
// (pop a window out, close it, restart) also emptied the file. Everything
// retained is saved now.
func TestCarryoverAfterAResizeStillSavesEverything(t *testing.T) {
	r := carryRunner("card1", 4096, 80)
	r.buf.Write([]byte("eighty column output\r\n"))
	r.buf.SetWidth(200)

	c := r.carryFrom(4096)
	if c == nil {
		t.Fatal("a resize with no output after it emptied the file")
	}
	if !bytes.Contains(c.bytes, []byte("eighty column output")) {
		t.Fatalf("saved something other than the scrollback: %q", c.bytes)
	}
	if c.cols != 200 {
		t.Fatalf("recorded %d columns, not the width in force at the save", c.cols)
	}
}

// And a runner that has produced nothing writes no file at all, because an
// empty file is one the next daemon has to decide about.
func TestCarryoverOfAnEmptyRingIsNotSaved(t *testing.T) {
	r := carryRunner("card1", 4096, 80)
	if c := r.carryFrom(4096); c != nil {
		t.Fatalf("saved %d bytes for a runner that never wrote anything", len(c.bytes))
	}
}

// THE WIDTH A REOPENED TERMINAL COMES UP AT.
//
// Every terminal used to open at a fixed hundred and twenty columns and get
// resized a second later by the first browser to attach. So every restart put
// a stretch of narrow output into the scrollback, with hard line breaks a
// third of the way across a wide window, and flattening cannot undo a line
// break that is already in the bytes. The operator saw one note per restart
// saying the history was drawn for another terminal, and was right that the
// thing producing it was the problem.
func TestATerminalReopensAtTheWidthItWasLastLookedAt(t *testing.T) {
	d := testDaemon(t)
	task, _, err := d.st.Register(store.Observed{
		WireName: "wide-window", Worktree: t.TempDir(), Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}

	// A card that has never had a terminal has no opinion, so the default
	// stands rather than something being invented.
	if got := d.launchWidthFor(task.ID); got != launchCols {
		t.Fatalf("invented a width for a card with no terminal history: %d", got)
	}

	if err := d.st.SetLastCols(task.ID, 247); err != nil {
		t.Fatal(err)
	}
	if got := d.launchWidthFor(task.ID); got != 247 {
		t.Fatalf("would have opened at %d columns, not the 247 it was last at", got)
	}

	// A width of nothing is not an answer and must not replace a real one. The
	// case is a runner that exited before any viewer said how big it was.
	if err := d.st.SetLastCols(task.ID, 0); err != nil {
		t.Fatal(err)
	}
	if got := d.launchWidthFor(task.ID); got != 247 {
		t.Fatalf("a width of zero overwrote the real one: %d", got)
	}

	// And a card nothing knows about falls back rather than failing a launch.
	if got := d.launchWidthFor("no-such-card"); got != launchCols {
		t.Fatalf("invented a width for a card that does not exist: %d", got)
	}
}

// A daemon killed mid-write, a file from a format that no longer exists, or
// anything else unreadable means "there is no scrollback", never an error.
// Halting on storage failure is the DATABASE's rule and this is not it.
func TestCarryoverFromACorruptFileIsDiscarded(t *testing.T) {
	dir := t.TempDir()
	good := carryRunner("card1", 4096, 100)
	good.buf.Write([]byte("real output\r\n"))
	if err := writeCarry(dir, "card1", good.carryFrom(4096)); err != nil {
		t.Fatal(err)
	}
	whole, err := os.ReadFile(carryPath(dir, "card1"))
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string][]byte{
		"truncated payload":  whole[:len(whole)-4],
		"header only":        whole[:bytes.IndexByte(whole, '\n')+1],
		"no header at all":   []byte("just some bytes with no newline"),
		"wrong magic":        bytes.Replace(whole, []byte(carryMagic), []byte("something-else"), 1),
		"a future version":   bytes.Replace(whole, []byte(" 1 "), []byte(" 99 "), 1),
		"another card's id":  bytes.Replace(whole, []byte("card1\n"), []byte("card2\n"), 1),
		"empty file":         nil,
		"a width of nothing": bytes.Replace(whole, []byte(" 100 "), []byte(" 0 "), 1),
	}
	for name, raw := range cases {
		if err := os.WriteFile(carryPath(dir, "card1"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if c := readCarry(dir, "card1", 4096); c != nil {
			t.Fatalf("%s was accepted: %q", name, c.bytes)
		}
	}
}

// The ordinary case on a machine that has never run this, and the case for
// every card that had no runner when the daemon stopped.
func TestCarryoverMissingFileIsNormal(t *testing.T) {
	dir := t.TempDir()
	if c := readCarry(dir, "card1", 4096); c != nil {
		t.Fatal("invented scrollback for a card that has none")
	}
	if c := readCarry(filepath.Join(dir, "not-there"), "card1", 4096); c != nil {
		t.Fatal("invented scrollback out of a directory that does not exist")
	}
	// And a name that could name something other than a card's own file.
	if c := readCarry(dir, "../../etc/passwd", 4096); c != nil {
		t.Fatal("read a file outside the scrollback directory")
	}
}

// Bounded by construction, in both directions. A ring is bounded, so what is
// written from one is too, and a file larger than a ring did not come from
// one.
func TestCarryoverIsBounded(t *testing.T) {
	dir := t.TempDir()
	r := carryRunner("card1", 4096, 100)
	for i := 0; i < 500; i++ {
		r.buf.Write([]byte(strings.Repeat("x", 60) + "\r\n"))
	}
	c := r.carryFrom(1024)
	if c == nil {
		t.Fatal("saved nothing at all")
	}
	if len(c.bytes) > 1024 {
		t.Fatalf("wrote %d bytes for a bound of 1024", len(c.bytes))
	}
	if err := writeCarry(dir, "card1", c); err != nil {
		t.Fatal(err)
	}
	// And the same bound applied on the way back in.
	if got := readCarry(dir, "card1", 16); got != nil {
		t.Fatal("read a file far larger than the ring it would go into")
	}
}

// Saving must never be what makes a shutdown hang. The budget covers every
// card together, and running out of it stops rather than finishing.
func TestSavingCarryoverIsBoundedInTime(t *testing.T) {
	d := testDaemon(t)
	var live []*runner
	for i := 0; i < 40; i++ {
		r := carryRunner("card"+string(rune('a'+i%26))+string(rune('a'+i/26)), 1<<20, 100)
		r.buf.Write([]byte(strings.Repeat("x", 1<<19) + "\r\n"))
		live = append(live, r)
	}
	start := time.Now()
	d.saveCarryover(live)
	if took := time.Since(start); took > carrySaveBudget+2*time.Second {
		t.Fatalf("saving scrollback took %s, which is not bounded by %s", took, carrySaveBudget)
	}

	// And the budget really is what stops it: with none left, nothing is
	// written at all rather than the job being finished anyway.
	spent := t.TempDir()
	d.opts.DBPath = filepath.ToSlash(filepath.Join(spent, "atrium.db"))
	d.saveCarryoverBy(live, time.Now().Add(-time.Second))
	if _, err := os.Stat(d.carryDir()); !os.IsNotExist(err) {
		t.Fatal("wrote scrollback after the shutdown budget was already spent")
	}
}

// The offer is made once per card. A card relaunched by hand hours later is
// new work, and this morning's output does not belong on top of it.
func TestCarryoverIsOfferedOncePerCard(t *testing.T) {
	d := testDaemon(t)
	before := carryRunner("card1", 4096, 100)
	before.buf.Write([]byte("earlier output\r\n"))
	if err := writeCarry(d.carryDir(), "card1", before.carryFrom(4096)); err != nil {
		t.Fatal(err)
	}

	first := carryRunner("card1", 4096, 100)
	d.adoptCarryover(first)
	if first.carried == nil {
		t.Fatal("the first runner on the card was given nothing")
	}
	second := carryRunner("card1", 4096, 100)
	d.adoptCarryover(second)
	if second.carried != nil {
		t.Fatal("a later runner on the same card was handed the same scrollback again")
	}
}

// A file nothing will ever read again is swept, and a card that is still there
// keeps its own.
func TestSweepDropsCarryoverForCardsThatAreGone(t *testing.T) {
	d := testDaemon(t)
	task, _, err := d.st.Register(store.Observed{
		WireName: "still-here", Worktree: "/tmp/atrium-still-here", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	r := carryRunner(task.ID, 4096, 100)
	r.buf.Write([]byte("output\r\n"))
	if err := writeCarry(d.carryDir(), task.ID, r.carryFrom(4096)); err != nil {
		t.Fatal(err)
	}
	gone := carryRunner("cardthatisgone", 4096, 100)
	gone.buf.Write([]byte("output\r\n"))
	if err := writeCarry(d.carryDir(), "cardthatisgone", gone.carryFrom(4096)); err != nil {
		t.Fatal(err)
	}
	// And something no daemon would have written, old enough to be sure
	// nothing is holding it.
	stale := filepath.Join(d.carryDir(), "card1.scrollback.tmp")
	if err := os.WriteFile(stale, []byte("half a file"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * carryKeep)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	d.sweepCarryover()

	if _, err := os.Stat(carryPath(d.carryDir(), task.ID)); err != nil {
		t.Fatalf("swept the scrollback of a card that still exists: %v", err)
	}
	if _, err := os.Stat(carryPath(d.carryDir(), "cardthatisgone")); !os.IsNotExist(err) {
		t.Fatal("kept scrollback for a card that is gone")
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("kept a half-written file nothing will ever read")
	}
}
