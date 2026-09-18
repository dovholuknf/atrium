package hubstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Snapshots, and the two things they have to be right about: that one taken
// while the hub is running is complete, and that keeping them does not mean
// keeping all of them.

// A SNAPSHOT OF A RUNNING STORE HAS EVERYTHING IN IT.
//
// The obvious way to back up a SQLite database is to copy the file, and it is
// wrong: everything committed since the last checkpoint is in the write-ahead
// log beside it, so the copy is missing whatever happened most recently. Which
// is to say it is missing the reason somebody is restoring.
func TestASnapshotHasWhatWasJustWritten(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	added(t, s, "sparta")
	added(t, s, "athens")

	b, err := s.Snapshot(filepath.Join(dir, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	if b.Size == 0 {
		t.Fatal("the snapshot is empty")
	}

	// Opened as a database of its own, which is also what makes a snapshot
	// worth having: it is a file you can read without the hub.
	back, err := Open(b.Path)
	if err != nil {
		t.Fatalf("the snapshot does not open: %v", err)
	}
	defer back.Close()
	rooms, err := back.Rooms()
	if err != nil {
		t.Fatal(err)
	}
	if len(rooms) != 2 {
		t.Fatalf("the snapshot holds %d room(s), wanted the two that were just added", len(rooms))
	}
}

// THE TIERS KEEP A MONTH IN ABOUT THIRTY FILES, and the point is the shape
// rather than the number: everything from the last hour, one an hour for a day,
// one a day for a week, one a week for a month. A flat list of the most recent
// fifty is eight hours, all of it from today.
func TestOldSnapshotsThinOutRatherThanPilingUp(t *testing.T) {
	dir := t.TempDir()
	// Two months of snapshots, one every ten minutes, as empty files: nothing
	// here reads them, and writing eight thousand real databases to test a
	// retention rule would be a test nobody runs.
	made := 0
	for age := time.Duration(0); age < 60*24*time.Hour; age += 10 * time.Minute {
		at := time.Now().Add(-age)
		name := backupPrefix + at.UTC().Format(backupStamp) + backupSuffix
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		made++
	}

	if _, err := Prune(dir); err != nil {
		t.Fatal(err)
	}
	left, err := Backups(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) >= made {
		t.Fatalf("pruning kept %d of %d", len(left), made)
	}
	// Six an hour for an hour, then 23 hourly, then 6 daily, then about 3
	// weekly. Bounded rather than exact, because the arithmetic at a boundary
	// depends on what time the test runs.
	if len(left) < 20 || len(left) > 50 {
		t.Fatalf("%d snapshots survived a month of them, which is not a tiered history", len(left))
	}

	// THE NEWEST IS ALWAYS THERE. A pruner that can delete the most recent
	// snapshot is a pruner that can leave nothing.
	if left[0].Age() > 20*time.Minute {
		t.Fatalf("the newest surviving snapshot is %s old", left[0].Age())
	}
	// AND SO IS SOMETHING OLD. The far end is what answers "what did this look
	// like last week", which is the question a flat list cannot.
	oldest := left[len(left)-1]
	if oldest.Age() < 20*24*time.Hour {
		t.Fatalf("the oldest surviving snapshot is only %s old, so a month is not covered",
			oldest.Age())
	}
}

// A FILE THIS DID NOT WRITE IS LEFT ALONE. Somebody's own copy sitting in the
// backup directory is not something to parse and certainly not something to
// delete.
func TestPruningLeavesFilesItDoesNotRecognise(t *testing.T) {
	dir := t.TempDir()
	mine := filepath.Join(dir, "before-the-upgrade.db")
	if err := os.WriteFile(mine, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(dir, backupPrefix+
		time.Now().Add(-40*24*time.Hour).UTC().Format(backupStamp)+backupSuffix)
	if err := os.WriteFile(old, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Prune(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(mine); err != nil {
		t.Fatal("pruning deleted a file it did not write")
	}
}

// RESTORING MOVES THE CURRENT STORE ASIDE RATHER THAN DELETING IT.
//
// It is done under pressure, from a list of timestamps, and the wrong one is
// one keypress away. Undoing a restore has to be another restore.
func TestRestoringKeepsWhatItReplaced(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "hub.db")
	s, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	added(t, s, "sparta")
	b, err := s.Snapshot(filepath.Join(dir, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	// Something that happened after the snapshot, which restoring undoes.
	added(t, s, "athens")
	s.Close()

	aside, err := Restore(db, b.Path)
	if err != nil {
		t.Fatal(err)
	}
	if aside == "" {
		t.Fatal("nothing was kept of what was replaced")
	}
	if _, err := os.Stat(aside); err != nil {
		t.Fatalf("what was replaced is not at %s", aside)
	}

	back, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer back.Close()
	rooms, err := back.Rooms()
	if err != nil {
		t.Fatal(err)
	}
	if len(rooms) != 1 || rooms[0].Name != "sparta" {
		t.Fatalf("the restored store holds %+v, wanted what the snapshot had", rooms)
	}

	// AND THE UNDO IS A RESTORE. Whatever was moved aside opens as a database,
	// which is the whole of the promise.
	undo, err := Open(aside)
	if err != nil {
		t.Fatalf("what was moved aside does not open: %v", err)
	}
	defer undo.Close()
	if rooms, err := undo.Rooms(); err != nil || len(rooms) != 2 {
		t.Fatalf("what was moved aside is not what was replaced: %+v, %v", rooms, err)
	}
}

// WHAT IS KEPT ASIDE IS THE WHOLE DATABASE, INCLUDING WHAT WAS NOT CHECKPOINTED.
//
// The store runs in write-ahead mode, so pages committed since the last
// checkpoint are in `hub.db-wal` and not in `hub.db`. Renaming the one file and
// calling it "what was there" keeps a copy missing exactly the recent work
// somebody would be undoing a restore to get back, which is the one thing the
// undo promise cannot be wrong about. Found by review.
func TestWhatIsKeptAsideIncludesWritesStillInTheLog(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "hub.db")
	s, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	added(t, s, "sparta")
	b, err := s.Snapshot(filepath.Join(dir, "backups"))
	if err != nil {
		t.Fatal(err)
	}

	// Written after the snapshot and deliberately NOT checkpointed: this is the
	// state a running hub is in most of the time.
	added(t, s, "athens")
	if _, err := os.Stat(db + "-wal"); err != nil {
		t.Skip("this build is not keeping a write-ahead log, so there is nothing to lose")
	}
	// Closed without a checkpoint of our own, the way a process that was killed
	// leaves it. `Close` on the driver may checkpoint, so the test does not
	// depend on whether it did: what matters is that the aside copy is whole
	// either way.
	s.Close()

	aside, err := Restore(db, b.Path)
	if err != nil {
		t.Fatal(err)
	}

	back, err := Open(aside)
	if err != nil {
		t.Fatalf("what was kept aside does not open: %v", err)
	}
	defer back.Close()
	rooms, err := back.Rooms()
	if err != nil {
		t.Fatal(err)
	}
	if len(rooms) != 2 {
		t.Fatalf("what was kept aside holds %d room(s), so the writes that were still "+
			"in the log were lost: %+v", len(rooms), rooms)
	}
}

// AND THE RESTORED DATABASE IS THE ONE SQLITE OPENS.
//
// A stale `-wal` beside a restored file is replayed on open, so the pages
// nobody asked for come back and the restore silently does nothing.
func TestARestoreIsNotUndoneByALeftoverLog(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "hub.db")
	s, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	added(t, s, "sparta")
	b, err := s.Snapshot(filepath.Join(dir, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	added(t, s, "athens")
	s.Close()

	if _, err := Restore(db, b.Path); err != nil {
		t.Fatal(err)
	}
	for _, tail := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(db + tail); err == nil {
			t.Fatalf("%s was left beside the restored store, which SQLite would replay", db+tail)
		}
	}
	back, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer back.Close()
	rooms, err := back.Rooms()
	if err != nil {
		t.Fatal(err)
	}
	if len(rooms) != 1 {
		t.Fatalf("the restored store holds %d room(s), so something came back that the "+
			"snapshot did not have: %+v", len(rooms), rooms)
	}
}

// A DAMAGED SNAPSHOT IS NOT RESTORED OVER A WORKING STORE, which would turn a
// bad afternoon into a lost hub.
func TestADamagedSnapshotIsRefusedBeforeAnythingMoves(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "hub.db")
	s, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	added(t, s, "sparta")
	s.Close()

	bad := filepath.Join(dir, "bad.db")
	if err := os.WriteFile(bad, []byte("this is not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	aside, err := Restore(db, bad)
	if err == nil {
		t.Fatal("a file that is not a database was restored")
	}
	if aside != "" {
		t.Fatal("the current store was moved aside before the snapshot was checked")
	}
	if !strings.Contains(err.Error(), "cannot be opened") {
		t.Fatalf("the refusal does not say what is wrong: %v", err)
	}

	// The store it refused to replace is untouched.
	back, err := Open(db)
	if err != nil {
		t.Fatalf("the store it refused to replace will not open: %v", err)
	}
	defer back.Close()
	if rooms, _ := back.Rooms(); len(rooms) != 1 {
		t.Fatalf("the store it refused to replace lost its rooms: %+v", rooms)
	}
}
