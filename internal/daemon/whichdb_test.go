package daemon

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// Saying when this is not the database you were using last time.
//
// This cost the operator twenty minutes: the daemon opened
// `$WORKTREE_ROOT/hub/atrium.db` instead of `~/.atrium/atrium.db` because it
// was started from a shell that had the variable set, and every card looked
// lost. Both databases were real and populated, which is why the existing
// new-database warning never fired.

// writePrev records what a previous daemon opened.
//
// Deliberately through the daemon's own writer rather than by hand, so a test
// cannot pass against a file the real code would never have produced. That is
// how the first version of this passed while being silent in the one case it
// existed for.
func writePrev(t *testing.T, d *Daemon, db string) {
	t.Helper()
	prev := &Daemon{opts: d.opts}
	prev.opts.DBPath = db
	prev.rememberDB()
}

// THE case this exists for, end to end and through the real writers: a daemon
// runs, winds down CLEANLY, and the next one starts on a different database.
//
// The first version of this failed here and passed everything else, because it
// read the previous database out of daemon.json, and `clearLocation` deletes
// that on every clean shutdown. So the warning fired only when a daemon had
// been killed outright, which is the one case the codebase already treats as
// untrustworthy.
func TestACleanStopThenADifferentDatabaseStillWarns(t *testing.T) {
	dir := t.TempDir()
	location := filepath.Join(dir, "daemon.json")

	// A daemon that ran on one database and stopped tidily.
	first := &Daemon{opts: Options{
		DBPath:       filepath.ToSlash(filepath.Join(dir, "first.db")),
		LocationFile: location,
	}}
	first.warnIfDifferentDatabase()
	first.clearLocation()

	// The address file is gone, which is correct and is what broke this.
	if _, err := os.Stat(location); !os.IsNotExist(err) {
		t.Fatal("this test is not exercising a clean stop: the address file survived")
	}

	logs := &safeBuf{}
	old := log.Writer()
	log.SetOutput(logs)
	t.Cleanup(func() { log.SetOutput(old) })

	second := &Daemon{opts: Options{
		DBPath:       filepath.ToSlash(filepath.Join(dir, "second.db")),
		LocationFile: location,
	}}
	second.warnIfDifferentDatabase()

	if !strings.Contains(logs.String(), "DIFFERENT DATABASE") {
		t.Fatalf("a clean stop then a different database said nothing:\n%s", logs.String())
	}
}

func TestADifferentDatabaseIsAnnouncedLoudly(t *testing.T) {
	d, logs, cancel, errCh := startDaemon(t)
	defer func() { cancel(); <-errCh }()

	// A previous daemon that used somewhere else entirely.
	writePrev(t, d, filepath.Join(t.TempDir(), "elsewhere.db"))
	d.warnIfDifferentDatabase()

	out := logs.String()
	if !strings.Contains(out, "DIFFERENT DATABASE") {
		t.Fatalf("opening another database said nothing:\n%s", out)
	}
	if !strings.Contains(out, "--db") {
		t.Fatal("the warning does not say how to fix it")
	}
	// Both paths, because one alone leaves you working out what changed.
	if !strings.Contains(out, "now:") || !strings.Contains(out, "last:") {
		t.Fatal("the warning does not name both databases")
	}
}

// The same database must stay silent, or the warning becomes noise and stops
// being read, which is the only way it can fail.
func TestTheSameDatabaseSaysNothing(t *testing.T) {
	d, logs, cancel, errCh := startDaemon(t)
	defer func() { cancel(); <-errCh }()

	before := logs.String()
	writePrev(t, d, d.opts.DBPath)
	d.warnIfDifferentDatabase()

	if strings.Contains(strings.TrimPrefix(logs.String(), before), "DIFFERENT DATABASE") {
		t.Fatal("the same database was reported as different")
	}
}

// What the daemon remembers must survive the thing that deletes the address
// file, because that is the whole correction.
func TestWhatWasOpenedSurvivesAWindDown(t *testing.T) {
	dir := t.TempDir()
	d := &Daemon{opts: Options{
		DBPath:       filepath.ToSlash(filepath.Join(dir, "a.db")),
		LocationFile: filepath.Join(dir, "daemon.json"),
	}}
	d.writeLocation()
	d.rememberDB()
	d.clearLocation()

	got, ok := d.previousDB()
	if !ok {
		t.Fatal("winding down forgot which database was open")
	}
	if !sameDBPath(got, d.opts.DBPath) {
		t.Fatalf("remembered %q", got)
	}
}

// It lives beside the address file, so naming another isolates both and a test
// can never reach the machine's real record.
func TestTheRecordIsIsolatedByTheLocationOption(t *testing.T) {
	dir := t.TempDir()
	d := &Daemon{opts: Options{LocationFile: filepath.Join(dir, "daemon.json")}}
	got, err := d.lastDBPath()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(got) != dir {
		t.Fatalf("the record went to %q, outside the test's directory", got)
	}
}

// Slash direction and case are noise on Windows. Warning about them would
// train the reader to skip the warning.
func TestSpellingTheSamePathDifferentlyIsNotADifference(t *testing.T) {
	for _, tc := range [][2]string{
		{`C:\Users\x\.atrium\atrium.db`, `C:/Users/x/.atrium/atrium.db`},
		{`C:/Users/X/.atrium/atrium.db`, `c:/users/x/.atrium/atrium.db`},
		{`C:/a/b/../b/atrium.db`, `C:/a/b/atrium.db`},
		{` C:/a/atrium.db `, `C:/a/atrium.db`},
	} {
		if !sameDBPath(tc[0], tc[1]) {
			t.Fatalf("%q and %q were treated as different databases", tc[0], tc[1])
		}
	}
	if sameDBPath("C:/a/atrium.db", "C:/b/atrium.db") {
		t.Fatal("two genuinely different paths were treated as one")
	}
}

// No previous file is a first run, which is not a surprise and gets no
// warning.
func TestAFirstRunSaysNothing(t *testing.T) {
	d, logs, cancel, errCh := startDaemon(t)
	defer func() { cancel(); <-errCh }()

	before := logs.String()
	if err := os.Remove(d.opts.LocationFile); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	d.warnIfDifferentDatabase()

	if strings.Contains(strings.TrimPrefix(logs.String(), before), "DIFFERENT DATABASE") {
		t.Fatal("a first run was reported as a different database")
	}
}

// The count is what makes the warning act on you rather than be read past.
// It has to come from a READ-ONLY open: going through store.Open would run
// every migration against a database this daemon was not asked to touch.
func TestTheCountComesFromTheOtherDatabaseWithoutTouchingIt(t *testing.T) {
	dir := t.TempDir()
	other := filepath.ToSlash(filepath.Join(dir, "other.db"))

	st, err := store.Open(other)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"one", "two", "three"} {
		if _, _, err := st.Register(store.Observed{
			WireName: name, Worktree: "d:/w", Runner: "claude",
		}); err != nil {
			t.Fatal(err)
		}
	}
	st.Close()

	before, err := os.Stat(filepath.FromSlash(other))
	if err != nil {
		t.Fatal(err)
	}

	if got := countCards(other); got != 3 {
		t.Fatalf("counted %d cards, wanted 3", got)
	}

	after, err := os.Stat(filepath.FromSlash(other))
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("counting modified the other database")
	}
}

// Anything unreadable answers "could not say" rather than "empty". Zero would
// be a lie that reads as data loss.
func TestAnUncountableDatabaseSaysSoRatherThanZero(t *testing.T) {
	for _, path := range []string{
		"",
		"   ",
		filepath.Join(t.TempDir(), "does-not-exist.db"),
	} {
		if got := countCards(path); got != -1 {
			t.Fatalf("countCards(%q) = %d, wanted -1", path, got)
		}
	}

	// A file that exists and is not a database.
	junk := filepath.Join(t.TempDir(), "junk.db")
	if err := os.WriteFile(junk, []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := countCards(junk); got != -1 {
		t.Fatalf("a non-database counted %d", got)
	}
}

func TestCardCountReads(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want string
	}{
		{-1, ""},
		{0, " (0 cards)"},
		{1, " (1 card)"},
		{15, " (15 cards)"},
	} {
		if got := cardCount(tc.n); got != tc.want {
			t.Fatalf("cardCount(%d) = %q, wanted %q", tc.n, got, tc.want)
		}
	}
}
