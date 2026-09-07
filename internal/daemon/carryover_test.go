package daemon

import (
	"bytes"
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

// THE BUG, end to end: a daemon stops, its ring goes with it, and the runner
// that comes back on the same card must still be able to show what came
// before.
func TestCarryoverRoundTripsAndReplaysAtTheSameWidth(t *testing.T) {
	dir := t.TempDir()
	before := carryRunner("card1", 4096, 100)
	before.buf.Write([]byte("what I was doing before the restart\r\n"))

	if err := writeCarry(dir, "card1", before.carryFrom(4096)); err != nil {
		t.Fatal(err)
	}

	after := carryRunner("card1", 4096, 100)
	after.carried = readCarry(dir, "card1", 4096)
	if after.carried == nil {
		t.Fatal("nothing came back off disk")
	}
	after.buf.Write([]byte("and what the new process says\r\n"))

	backlog, _, ch := after.subscribe()
	after.unsubscribe(ch)
	if !bytes.Contains(backlog, []byte("before the restart")) {
		t.Fatalf("the pre-restart output was not replayed: %q", backlog)
	}
	if !bytes.Contains(backlog, []byte("the new process")) {
		t.Fatalf("the live output was lost: %q", backlog)
	}
	if !bytes.Contains(backlog, carryDivider) {
		t.Fatal("the restart boundary was not drawn, so the join reads as a repeat")
	}
	if bytes.Index(backlog, []byte("before the restart")) >
		bytes.Index(backlog, []byte("the new process")) {
		t.Fatal("the carried output was replayed after the live output")
	}
}

// The rule the ring learned the hard way, applied to bytes that outlived the
// process: output composed for another width cannot be redrawn here.
func TestCarryoverWrittenAtOneWidthIsNotReplayedIntoAnother(t *testing.T) {
	dir := t.TempDir()
	before := carryRunner("card1", 4096, 80)
	before.buf.Write([]byte("eighty column output\r\n"))
	if err := writeCarry(dir, "card1", before.carryFrom(4096)); err != nil {
		t.Fatal(err)
	}

	after := carryRunner("card1", 4096, 200)
	after.carried = readCarry(dir, "card1", 4096)
	if after.carried == nil {
		t.Fatal("nothing came back off disk")
	}
	after.buf.Write([]byte("two hundred column output\r\n"))

	backlog, _, ch := after.subscribe()
	after.unsubscribe(ch)
	if bytes.Contains(backlog, []byte("eighty column")) {
		t.Fatal("replayed output composed for a terminal of another width")
	}
	if !bytes.Contains(backlog, []byte("two hundred column")) {
		t.Fatalf("dropped the live output too: %q", backlog)
	}
}

// A width nothing was written at describes nothing, so there is nothing to
// save. The empty file that would otherwise be written is a file the next
// daemon has to decide about.
func TestCarryoverOfAWidthNothingWasWrittenAtIsNotSaved(t *testing.T) {
	r := carryRunner("card1", 4096, 80)
	r.buf.Write([]byte("eighty column output\r\n"))
	r.buf.SetWidth(200)
	if c := r.carryFrom(4096); c != nil {
		t.Fatalf("saved %d bytes for a width nothing was composed at", len(c.bytes))
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
