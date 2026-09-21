package hubstore

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Snapshots of the hub's store, taken while it runs.
//
// ── why this exists at all ──────────────────────────────
//
// The store halts on a corrupt database and refuses to start on one. That is
// the right posture and it is only tolerable if there is something to go back
// to. Without this, "it halts" means "it is gone".
//
// ── why tiers rather than a count ───────────────────────
//
// Keeping the last fifty snapshots of a thing written every ten minutes is
// eight hours of history, all of it from today. The two questions people
// actually ask are "put it back to twenty minutes ago" and "what did this look
// like last week", and a flat list answers only the first.
//
// So: everything from the last hour, one an hour for a day, one a day for a
// week, one a week for a month. Roughly thirty files covering a month, and the
// oldest is as easy to find as the newest.
//
// ── and why VACUUM INTO ─────────────────────────────────
//
// It is SQLite's own answer for copying a live database, and it writes ONE
// file: no journal, no write-ahead log, nothing to carry alongside. Copying
// `hub.db` with the operating system while the hub is running is the obvious
// alternative and it is wrong, because the rows committed since the last
// checkpoint are in `hub.db-wal` and a copy of the first file without the
// second is a database missing whatever happened most recently.
//
// It is also a real read of every page, so a snapshot that succeeds is a
// statement that the database was readable at that moment.

// backupStamp is the filename's time, chosen so an ordinary alphabetical sort
// is a sort by age and so the name survives a filesystem that dislikes colons.
//
// IN UTC, AND SHOWN IN LOCAL TIME. A name in wall-clock time repeats itself for
// an hour when the clocks go back, and a snapshot whose name is already there
// is one that does not get taken: an hour of history lost, once a year, in the
// season nobody is looking. The listing converts on the way out, so what
// anybody reads is still their own clock.
const backupStamp = "2006-01-02T15-04-05Z"

const backupPrefix = "hub-"
const backupSuffix = ".db"

// BackupEvery is how often a running hub takes one.
//
// Ten minutes because that is the shortest tier, and the shortest tier is what
// decides how much work a restore can cost: at worst, ten minutes of names,
// marks and cached cards. Nothing here is work, which is why ten minutes is
// generous rather than reckless.
const BackupEvery = 10 * time.Minute

// Backup is one snapshot on disk.
type Backup struct {
	Path string    `json:"path"`
	At   time.Time `json:"at"`
	Size int64     `json:"size"`
}

// Age is how long ago it was taken.
func (b Backup) Age() time.Duration { return time.Since(b.At) }

// Snapshot writes one, and answers where it went.
func (s *Store) Snapshot(dir string) (Backup, error) {
	var b Backup
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return b, err
	}
	at := now()
	path := filepath.Join(dir, backupPrefix+at.UTC().Format(backupStamp)+backupSuffix)
	// A name that is already there means one was taken this second. Nothing is
	// lost by leaving it: it is the same database.
	if _, err := os.Stat(path); err == nil {
		return Backup{Path: path, At: at}, nil
	}

	// VACUUM INTO refuses to overwrite, which is the behaviour wanted, and
	// takes a literal rather than a placeholder. The path is built here from a
	// timestamp and a directory the caller chose, so the only quoting needed is
	// SQLite's own for a single quote in a directory name.
	q := "VACUUM INTO '" + strings.ReplaceAll(path, "'", "''") + "'"
	if err := s.guard(func() error {
		_, err := s.db.Exec(q)
		return err
	}); err != nil {
		return b, fmt.Errorf("could not write %s: %w", path, err)
	}
	st, err := os.Stat(path)
	if err != nil {
		return b, err
	}
	return Backup{Path: path, At: at, Size: st.Size()}, nil
}

// Backups lists what is there, newest first.
func Backups(dir string) ([]Backup, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Backup
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, backupPrefix) || !strings.HasSuffix(name, backupSuffix) {
			continue
		}
		stamp := strings.TrimSuffix(strings.TrimPrefix(name, backupPrefix), backupSuffix)
		at, err := time.Parse(backupStamp, stamp)
		if err != nil {
			// A FILE THIS DID NOT WRITE IS LEFT ALONE. Somebody's own copy
			// sitting in this directory is not something to parse, and it is
			// certainly not something to delete.
			continue
		}
		b := Backup{Path: filepath.Join(dir, name), At: at}
		if info, err := e.Info(); err == nil {
			b.Size = info.Size()
		}
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out, nil
}

// tier is one band of history and how much of it to keep.
type tier struct {
	// within is how far back this tier reaches.
	within time.Duration
	// every is the spacing inside it. Zero means keep everything.
	every time.Duration
}

// tiers, newest band first. Each snapshot is kept by the FIRST band it falls
// in, so the bands do not have to be disjoint and reading them is reading the
// sentence: everything from the last hour, one an hour for a day, one a day for
// a week, one a week for a month.
var tiers = []tier{
	{within: time.Hour},
	{within: 24 * time.Hour, every: time.Hour},
	{within: 7 * 24 * time.Hour, every: 24 * time.Hour},
	{within: 31 * 24 * time.Hour, every: 7 * 24 * time.Hour},
}

// Prune deletes the snapshots the tiers do not call for, and answers how many
// went.
//
// KEEPS THE OLDEST IN EACH SLOT, not the newest. An hourly slot holding six
// snapshots is one hour of history, and the one that best represents the hour
// is the one at its start: keeping the newest would drift every slot toward the
// next tier's boundary and leave a gap behind it.
//
// THE NEWEST IS ALWAYS KEPT whatever the arithmetic says, because a pruner that
// can delete the most recent snapshot is a pruner that can leave nothing.
func Prune(dir string) (int, error) {
	all, err := Backups(dir)
	if err != nil {
		return 0, err
	}
	if len(all) == 0 {
		return 0, nil
	}
	keep := map[string]bool{all[0].Path: true}
	// Oldest first inside each slot, so the first one seen for a slot is the
	// one that stays.
	byAge := make([]Backup, len(all))
	copy(byAge, all)
	sort.Slice(byAge, func(i, j int) bool { return byAge[i].At.Before(byAge[j].At) })

	taken := map[string]bool{}
	for _, b := range byAge {
		age := time.Since(b.At)
		for i, t := range tiers {
			if age > t.within {
				continue
			}
			if t.every == 0 {
				keep[b.Path] = true
				break
			}
			slot := fmt.Sprintf("%d:%d", i, b.At.Unix()/int64(t.every.Seconds()))
			if !taken[slot] {
				taken[slot] = true
				keep[b.Path] = true
			}
			break
		}
	}

	var gone int
	for _, b := range all {
		if keep[b.Path] {
			continue
		}
		if err := os.Remove(b.Path); err != nil {
			// Best effort. A file that will not delete is not a reason to stop
			// keeping backups, and it will be tried again on the next pass.
			log.Printf("[hub] could not remove the old backup %s: %v", b.Path, err)
			continue
		}
		gone++
	}
	return gone, nil
}

// BackUp takes snapshots for as long as the context lives.
//
// ONE IMMEDIATELY, and then on the tick. A hub that is restarted every few
// minutes while somebody changes a stylesheet would otherwise never reach its
// first tick, so the backups would be a feature that only works on a hub nobody
// is working on.
//
// Nothing here can fail the hub. A snapshot that cannot be written is logged
// and tried again on the next tick, because a hub that stopped serving because
// its backup directory was full would be trading the thing that matters for the
// thing that protects it.
func (s *Store) BackUp(ctx context.Context, dir string) {
	take := func() {
		b, err := s.Snapshot(dir)
		if err != nil {
			log.Printf("[hub] could not back up its store: %v", err)
			return
		}
		if n, err := Prune(dir); err != nil {
			log.Printf("[hub] could not tidy old backups: %v", err)
		} else if n > 0 {
			log.Printf("[hub] backed up to %s, and removed %d older one(s)", b.Path, n)
		}
	}
	take()

	t := time.NewTicker(BackupEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			take()
		}
	}
}

// Restore puts a snapshot back, and is deliberately awkward in one specific way.
//
// IT MOVES THE CURRENT DATABASE ASIDE RATHER THAN DELETING IT. Restoring is
// done under pressure, from a list of timestamps, and the wrong one is one
// keypress away. What it replaces is kept beside it with the moment of the
// restore in its name, so undoing a restore is another restore.
//
// It refuses to run against a database something else has open, which on
// Windows is the operating system's answer and on other systems is this
// function's: a hub running while its database is swapped underneath would
// carry on serving the old one and write it back over the new.
func Restore(dbPath, from string) (aside string, err error) {
	if _, err := os.Stat(from); err != nil {
		return "", fmt.Errorf("there is no backup at %s", from)
	}
	// READABLE AS A DATABASE BEFORE ANYTHING IS MOVED. Restoring a damaged file
	// over a working one would turn a bad afternoon into a lost hub.
	probe, err := Open(from)
	if err != nil {
		return "", fmt.Errorf("that backup cannot be opened, so it is not being restored: %w", err)
	}
	probe.Close()

	if _, err := os.Stat(dbPath); err == nil {
		// FOLDED INTO ONE FILE BEFORE IT IS MOVED, and this is the whole of what
		// was wrong here.
		//
		// The store runs in write-ahead mode, so pages committed since the last
		// checkpoint live in `hub.db-wal` and not in `hub.db`. Renaming the one
		// file and calling it "what was there" produced an aside copy missing
		// exactly the recent work somebody would be undoing a restore to get
		// back. Checkpointing first puts everything in the main file, so what is
		// kept is the whole database.
		//
		// It also fails early and for the right reason: a hub still running
		// holds this open, so this is where "stop it first" is discovered,
		// before anything has been moved.
		if err := checkpoint(dbPath); err != nil {
			return "", fmt.Errorf("could not settle the current store, which usually means "+
				"a hub is running on it. stop it first: %w", err)
		}
		aside = dbPath + ".replaced-" + now().UTC().Format(backupStamp)
		if err := os.Rename(dbPath, aside); err != nil {
			return "", fmt.Errorf("could not move the current store aside, which usually means "+
				"a hub is running on it. stop it first: %w", err)
		}
	}
	// AND THE LEFTOVERS GO, OR NOTHING IS RESTORED.
	//
	// A `-wal` from the old database sitting beside a restored one is how a
	// restore silently does nothing: SQLite replays it on open and the pages
	// nobody asked for come back. The checkpoint above should have emptied it,
	// so failing to remove it means something still has the database open, and
	// carrying on from there would write a file that is quietly wrong.
	for _, tail := range []string{"-wal", "-shm"} {
		if err := os.Remove(dbPath + tail); err != nil && !errors.Is(err, os.ErrNotExist) {
			return aside, fmt.Errorf("could not clear %s, so the restore was stopped rather "+
				"than left half done. what was there is at %s: %w", dbPath+tail, aside, err)
		}
	}

	raw, err := os.ReadFile(from)
	if err != nil {
		return aside, err
	}
	if err := os.WriteFile(dbPath, raw, 0o600); err != nil {
		return aside, err
	}
	return aside, nil
}

// checkpoint folds a database's write-ahead log back into the file itself.
//
// TRUNCATE rather than PASSIVE, because the point is to leave nothing behind:
// passive gives up when a reader is mid-page and reports success, which is the
// one outcome that would be worse than an error here.
func checkpoint(path string) error {
	s, err := Open(path)
	if err != nil {
		return err
	}
	defer s.Close()
	_, err = s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	return err
}
