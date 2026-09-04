package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// A source is a command, so testing one means running a command. This process
// stands in for it: re-executed with a marker in the environment, it prints
// what the test told it to print and exits with the status the test asked for.
//
// os.Exit before the testing package writes anything, because stdout is the
// contract here and "PASS" appended to a JSON array is not a JSON array.

func TestHelperSourceProcess(t *testing.T) {
	if os.Getenv("ATRIUM_TEST_SOURCE") == "" {
		t.Skip("not the helper")
	}
	fmt.Fprint(os.Stdout, os.Getenv("ATRIUM_TEST_SOURCE_OUT"))
	fmt.Fprint(os.Stderr, os.Getenv("ATRIUM_TEST_SOURCE_ERR"))
	if os.Getenv("ATRIUM_TEST_SOURCE_FAIL") != "" {
		os.Exit(3)
	}
	os.Exit(0)
}

// helperSource builds a Source that re-runs this test binary as the helper.
func helperSource(t *testing.T, id, out string) store.Source {
	t.Helper()
	t.Setenv("ATRIUM_TEST_SOURCE", "1")
	t.Setenv("ATRIUM_TEST_SOURCE_OUT", out)
	return store.Source{
		ID: id, Label: id, Enabled: true,
		Cmd:          os.Args[0],
		Args:         []string{"-test.run=TestHelperSourceProcess"},
		IntervalSecs: store.MinSourceInterval,
	}
}

// A daemon with a store and nothing listening. Everything under test here runs
// a child process and writes to the database, and none of it needs a port.
func testDaemon(t *testing.T) *Daemon {
	t.Helper()
	dir := t.TempDir()
	d, err := New(Options{
		AgentAddr:    freePort(t),
		HumanAddr:    freePort(t),
		DBPath:       filepath.ToSlash(filepath.Join(dir, "atrium.db")),
		LongPoll:     time.Second,
		LocationFile: filepath.Join(dir, "daemon.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// The happy path: a source prints items and they become cards.
func TestASourceFillsTheInbox(t *testing.T) {
	d := testDaemon(t)
	src, err := d.st.SaveSource(helperSource(t, "issues", `[
		{"source":"github","external_id":"4211","title":"dns drops on resume"},
		{"source":"github","external_id":"4212","title":"tunneler leaks a handle"}
	]`))
	if err != nil {
		t.Fatal(err)
	}

	created, err := d.runSource(context.Background(), src)
	if err != nil {
		t.Fatalf("the source failed: %v", err)
	}
	if created != 2 {
		t.Fatalf("offered %d items, wanted 2", created)
	}

	offered, err := d.st.Offered()
	if err != nil {
		t.Fatal(err)
	}
	if len(offered) != 2 {
		t.Fatalf("the inbox holds %d", len(offered))
	}
}

// A source posts everything it can see on every tick. The second tick must
// produce nothing new, or the board fills with the same work every interval.
func TestASecondTickOffersNothingNew(t *testing.T) {
	d := testDaemon(t)
	src, err := d.st.SaveSource(helperSource(t, "issues",
		`[{"source":"github","external_id":"4211","title":"x"}]`))
	if err != nil {
		t.Fatal(err)
	}

	if created, err := d.runSource(context.Background(), src); err != nil || created != 1 {
		t.Fatalf("first tick: created=%d err=%v", created, err)
	}
	created, err := d.runSource(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 {
		t.Fatalf("the second tick offered %d items again", created)
	}
}

// Nothing to report is a normal answer. A queue with nothing in it is the
// state you want, and treating it as a failure would switch off every source
// that is doing its job.
func TestAnEmptyRunIsNotAFailure(t *testing.T) {
	d := testDaemon(t)
	src, err := d.st.SaveSource(helperSource(t, "quiet", ""))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.runSource(context.Background(), src); err != nil {
		t.Fatalf("an empty run was treated as a failure: %v", err)
	}
	got, err := d.st.SourceByID("quiet")
	if err != nil {
		t.Fatal(err)
	}
	if got.Failures != 0 || got.LastError != "" {
		t.Fatalf("failures=%d error=%q", got.Failures, got.LastError)
	}
	if !got.Enabled {
		t.Fatal("a source that correctly found nothing was switched off")
	}
	if got.LastRunAt == nil {
		t.Fatal("the run was not recorded")
	}
}

// Three failures in a row switch it off, with the reason still on the row. A
// source retrying forever against a script somebody deleted is a process spawn
// every interval producing an error nobody reads.
func TestThreeFailuresSwitchASourceOff(t *testing.T) {
	d := testDaemon(t)
	src := helperSource(t, "broken", "")
	t.Setenv("ATRIUM_TEST_SOURCE_FAIL", "1")
	t.Setenv("ATRIUM_TEST_SOURCE_ERR", "gh: not logged in")
	saved, err := d.st.SaveSource(src)
	if err != nil {
		t.Fatal(err)
	}

	for i := 1; i <= store.MaxSourceFailures; i++ {
		if _, err := d.runSource(context.Background(), saved); err == nil {
			t.Fatalf("run %d did not fail", i)
		}
		got, err := d.st.SourceByID("broken")
		if err != nil {
			t.Fatal(err)
		}
		if got.Failures != i {
			t.Fatalf("after %d failures the row says %d", i, got.Failures)
		}
		wantOff := i >= store.MaxSourceFailures
		if got.Enabled == wantOff {
			t.Fatalf("after %d failures enabled=%v", i, got.Enabled)
		}
	}

	got, err := d.st.SourceByID("broken")
	if err != nil {
		t.Fatal(err)
	}
	if got.LastError == "" {
		t.Fatal("a source was switched off with no reason on the row")
	}
	if !strings.Contains(got.LastError, "not logged in") {
		t.Fatalf("the reason does not say what the command said: %q", got.LastError)
	}
}

// A run that works clears the count, so two bad afternoons a week apart do not
// add up to a source being switched off.
func TestASuccessfulRunClearsTheFailureCount(t *testing.T) {
	d := testDaemon(t)
	src := helperSource(t, "flaky", "")
	t.Setenv("ATRIUM_TEST_SOURCE_FAIL", "1")
	saved, err := d.st.SaveSource(src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.runSource(context.Background(), saved); err == nil {
		t.Fatal("the failing run did not fail")
	}

	t.Setenv("ATRIUM_TEST_SOURCE_FAIL", "")
	t.Setenv("ATRIUM_TEST_SOURCE_OUT", `[{"source":"github","external_id":"1"}]`)
	if _, err := d.runSource(context.Background(), saved); err != nil {
		t.Fatalf("the recovering run failed: %v", err)
	}
	got, err := d.st.SourceByID("flaky")
	if err != nil {
		t.Fatal(err)
	}
	if got.Failures != 0 || got.LastError != "" {
		t.Fatalf("a working run left failures=%d error=%q", got.Failures, got.LastError)
	}
}

// Output that is not items is a failure with a readable reason, not a panic
// and not a silent zero.
func TestGarbageOutputFailsWithAReason(t *testing.T) {
	d := testDaemon(t)
	src, err := d.st.SaveSource(helperSource(t, "garbage", "this is not json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.runSource(context.Background(), src); err == nil {
		t.Fatal("garbage was accepted")
	}
	got, err := d.st.SourceByID("garbage")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.LastError, "not an item") {
		t.Fatalf("the reason is unhelpful: %q", got.LastError)
	}
}

// A batch with one unkeyable item is a source to fix, not a partial import to
// reconcile afterwards. Nothing from that run lands.
func TestOneBadItemFailsTheWholeRun(t *testing.T) {
	d := testDaemon(t)
	src, err := d.st.SaveSource(helperSource(t, "mixed", `[
		{"source":"github","external_id":"1","title":"fine"},
		{"source":"github","title":"no identifier"}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.runSource(context.Background(), src); err == nil {
		t.Fatal("a batch with an unkeyable item was accepted")
	}
	offered, err := d.st.Offered()
	if err != nil {
		t.Fatal(err)
	}
	if len(offered) != 0 {
		t.Fatalf("%d items from a failed run reached the board", len(offered))
	}
}

// Only what is due runs. A source with a fifteen minute interval that ran a
// moment ago is not due, and one that has never run is.
func TestOnlyDueSourcesRun(t *testing.T) {
	d := testDaemon(t)
	fresh, err := d.st.SaveSource(store.Source{
		ID: "never-run", Cmd: "x", Enabled: true, IntervalSecs: 900,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.st.SaveSource(store.Source{
		ID: "off", Cmd: "x", Enabled: false, IntervalSecs: 30,
	}); err != nil {
		t.Fatal(err)
	}

	due, err := d.st.DueSources(time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].ID != fresh.ID {
		t.Fatalf("due sources are %v", due)
	}

	// Having just run, it is not due again.
	if _, err := d.st.SourceRan("never-run", 0, nil); err != nil {
		t.Fatal(err)
	}
	due, err = d.st.DueSources(time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 0 {
		t.Fatalf("a source that just ran is due again: %v", due)
	}

	// And is due once its interval has passed.
	due, err = d.st.DueSources(time.Now().UTC().Add(20 * time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 {
		t.Fatal("a source did not come due after its interval")
	}
}

// Turning a source back on is the operator saying they fixed it, so the count
// that switched it off goes with the switch.
func TestEnablingASourceClearsWhySwitchedOff(t *testing.T) {
	d := testDaemon(t)
	if _, err := d.st.SaveSource(store.Source{
		ID: "s", Cmd: "x", Enabled: true, IntervalSecs: 60,
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < store.MaxSourceFailures; i++ {
		if _, err := d.st.SourceRan("s", 0, fmt.Errorf("boom")); err != nil {
			t.Fatal(err)
		}
	}
	off, err := d.st.SourceByID("s")
	if err != nil {
		t.Fatal(err)
	}
	if off.Enabled {
		t.Fatal("it was not switched off")
	}

	back, err := d.st.SaveSource(store.Source{
		ID: "s", Cmd: "x", Enabled: true, IntervalSecs: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	if back.Failures != 0 || back.LastError != "" {
		t.Fatalf("turning it back on kept failures=%d error=%q", back.Failures, back.LastError)
	}
}

// There is nowhere in a source to put a credential, and an interval floor
// keeps a typo from becoming a fork bomb with a settings screen.
func TestASourceCannotRunEverySecond(t *testing.T) {
	d := testDaemon(t)
	got, err := d.st.SaveSource(store.Source{ID: "fast", Cmd: "x", IntervalSecs: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got.IntervalSecs < store.MinSourceInterval {
		t.Fatalf("interval came back as %d", got.IntervalSecs)
	}
}
