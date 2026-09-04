package daemon

import (
	"database/sql"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Saying when this is not the database you were using last time.
//
// Which database the daemon opens depends on the environment of the shell it
// was started from. `hub.HubDir` returns `$WORKTREE_ROOT/hub` when that
// variable is set and `~/.atrium` when it is not, so starting the daemon from
// a terminal that has it and from one that does not gives two different
// boards, both real, both populated, neither obviously wrong.
//
// The existing guard only covers a database that had to be CREATED. That is
// the easier half: an empty board is obviously empty. Opening a different
// EXISTING one is silent, and it is the worse case, because a populated
// stranger looks exactly like your own board after something ate most of it.
//
// So the daemon remembers. `daemon.json` already records which database the
// last one opened. Comparing is nearly free and the answer is the difference
// between twenty minutes of panic and reading one line.

// lastDB is what the previous daemon opened, and it is deliberately NOT
// daemon.json.
//
// daemon.json answers "where is a daemon RUNNING", so `clearLocation` deletes
// it on every clean wind-down. Reading the previous database out of it would
// therefore work only when the last daemon was killed outright, and would be
// silent after an ordinary `atrium stop` followed by a start from a different
// shell, which is precisely the case this exists for. A guard that has tests
// and is silent exactly when needed is worse than no guard.
//
// So this file answers a different question, "what did this machine open
// last", and nothing removes it.
type lastDB struct {
	DB string `json:"db"`
	At string `json:"at"`
}

// lastDBPath sits beside the address file, so naming another location file
// isolates both and a test cannot reach the machine's real one.
func (d *Daemon) lastDBPath() (string, error) {
	path, err := d.locationPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(path), "lastdb.json"), nil
}

// previousDB reads which database the last daemon on this machine opened.
func (d *Daemon) previousDB() (string, bool) {
	path, err := d.lastDBPath()
	if err != nil {
		return "", false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var prev lastDB
	if err := json.Unmarshal(raw, &prev); err != nil {
		return "", false
	}
	prev.DB = strings.TrimSpace(prev.DB)
	return prev.DB, prev.DB != ""
}

// rememberDB records what this daemon opened, for the next one to compare
// against. Written after the warning, and never deleted.
func (d *Daemon) rememberDB() {
	path, err := d.lastDBPath()
	if err != nil {
		return
	}
	body, err := json.MarshalIndent(lastDB{
		DB: filepath.ToSlash(d.opts.DBPath),
		At: time.Now().Format(time.RFC3339),
	}, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	// Failure is ignored. The worst case is one missing warning next time,
	// which is where this started.
	_ = os.WriteFile(path, append(body, '\n'), 0o600)
}

// sameDBPath compares two database paths the way the filesystem would.
//
// Slash direction and case are both noise here: `C:\Users\x\.atrium\atrium.db`
// and `C:/Users/X/.atrium/atrium.db` are one file, and warning about them
// would train the reader to skip the warning.
func sameDBPath(a, b string) bool {
	norm := func(s string) string {
		s = filepath.Clean(filepath.FromSlash(strings.TrimSpace(s)))
		if abs, err := filepath.Abs(s); err == nil {
			s = abs
		}
		return strings.ToLower(filepath.ToSlash(s))
	}
	return norm(a) == norm(b)
}

// countCards reads how many cards a database holds, without migrating it.
//
// Read-only and deliberately hand-rolled rather than going through the store:
// opening the OTHER database through `store.Open` would run every migration
// against it, which is a write, and writing to a database the operator did not
// ask this daemon to touch is exactly the wrong way to tell them they have two.
//
// Any failure answers -1, which reads as "could not say" rather than "empty".
// A missing file, a locked one and a schema that predates the task table all
// land here and none of them is worth a second warning.
func countCards(path string) int {
	if strings.TrimSpace(path) == "" {
		return -1
	}
	if _, err := os.Stat(filepath.FromSlash(path)); err != nil {
		return -1
	}
	// `mode=ro` so this cannot create, migrate or lock anything.
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if err != nil {
		return -1
	}
	defer db.Close()

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM task`).Scan(&n); err != nil {
		return -1
	}
	return n
}

// warnIfDifferentDatabase says, loudly, when this is not the database the last
// daemon used.
//
// Loud in the same shape as the new-database warning, because it is the same
// class of surprise and the operator has already learned to read that box.
func (d *Daemon) warnIfDifferentDatabase() {
	// Recorded whatever happens, so the next start has something to compare
	// against even when this one says nothing.
	defer d.rememberDB()

	prev, ok := d.previousDB()
	if !ok || sameDBPath(prev, d.opts.DBPath) {
		return
	}

	here := countCards(d.opts.DBPath)
	there := countCards(prev)

	log.Printf("[atrium] ---------------------------------------------------------------")
	log.Printf("[atrium] THIS IS A DIFFERENT DATABASE from the one used last time.")
	log.Printf("[atrium]   now:  %s%s", filepath.ToSlash(d.opts.DBPath), cardCount(here))
	log.Printf("[atrium]   last: %s%s", filepath.ToSlash(prev), cardCount(there))
	log.Printf("[atrium] Which one you get depends on WORKTREE_ROOT in the shell you")
	log.Printf("[atrium] started from. Pass --db to say which you meant.")
	log.Printf("[atrium] ---------------------------------------------------------------")
}

// cardCount formats a count that might not be knowable.
func cardCount(n int) string {
	if n < 0 {
		return ""
	}
	if n == 1 {
		return " (1 card)"
	}
	return " (" + strconv.Itoa(n) + " cards)"
}
