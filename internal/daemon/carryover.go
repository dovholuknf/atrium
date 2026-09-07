package daemon

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
)

// Scrollback that survives a restart.
//
// THE FAILURE THIS PREVENTS: atrium restarts itself constantly. Installing a
// new daemon from inside a session it is running is a documented workflow
// (`docs/reload-design.md`), and every restart destroys both copies of a
// runner's scrollback at once. The ring buffer goes with the process that held
// it, every pty is closed so every runner dies, and a fixture that comes back
// is a NEW process with a NEW ring holding nothing. The only remaining copy is
// the xterm buffer in whatever browser window lived through it, so closing
// that window is the moment the last copy is discarded. Attach afterwards and
// the terminal starts at "just now". A board whose whole premise is "what was
// I even doing" cannot answer that with an empty screen.
//
// So the rings are written out during the wind-down atrium already narrates,
// and read back on the way up. Cheap on purpose. This is NOT the per-agent
// transcript on disk that `CLAUDE.md` lists as a maybe: nothing is appended as
// it is produced, so nothing grows without bound, and what lands on disk is a
// ring's worth of bytes that were already resident in memory.
//
// ITS HONEST LIMIT: it does nothing for a crash. A daemon that is killed never
// runs its wind-down, so the newest thing on disk is whatever the last clean
// stop wrote. Saying so here beats pretending otherwise: the fix for a crash
// is writing as output arrives, which is the transcript, which is the thing
// deliberately not being built.

// KEYED BY CARD, not by pid and not by wire name.
//
// A card outlives the process it describes (`docs/architecture-v2.md`) and a
// pid is only a reconnect hint. A resumed fixture is a new process on the same
// card, which is exactly the case being fixed, so the pid changes across every
// restart and the card id does not. A wire name is an attribute a human may
// retype. The card id is the one thing that both survives the restart and
// still names the same session to the person looking at it.
const (
	carryMagic   = "atrium-scrollback"
	carryVersion = 1
	// carryDirName sits beside the database, the way icons and scrap already
	// do. NOT under any card's own directory: the file endpoints are scoped to
	// a card on purpose (`internal/safepath`), and one session's terminal
	// output is not a file that card owns.
	carryDirName = "scrollback"
	carrySuffix  = ".scrollback"
)

// carryKeep is how long a file nothing has claimed is left alone.
//
// A card that is deleted takes its file with it on the next sweep, so this is
// only for the ones nothing can answer for: a leftover from a daemon pointed
// at another database, or a card whose runner never came back. Long enough
// that a machine left off over a holiday still has its scrollback.
const carryKeep = 7 * 24 * time.Hour

// carrySaveBudget is the whole of what writing these may cost a shutdown.
//
// Shutdown is bounded and narrated, and supervised runners already have ten
// seconds of it. This must not extend that, so the budget covers every card
// together rather than each one, and running out logs and stops rather than
// finishing the job. Losing one card's scrollback is a smaller failure than a
// stop that hangs.
const carrySaveBudget = 2 * time.Second

// carryDivider marks where one daemon's output ends and the next one's begins.
//
// Said rather than hidden. The bytes on either side were composed by two
// different processes at two different times, and a join with nothing at it
// reads as the runner having repeated itself.
var carryDivider = []byte("\r\n\x1b[38;5;244m" +
	"[atrium] ---- atrium restarted here ----" +
	"\x1b[0m\r\n")

// carryover is one card's retained output and the width it was composed at.
//
// ONE WIDTH, not a list of marks, and that is the whole format decision. The
// ring records every width its output was ever composed at, but `SnapshotAt`
// only ever returns the TRAILING RUN composed at the width in force now, and
// nothing else could ever be replayed into a terminal. So what is saved is
// that run and that one number. The rule the ring learned the hard way comes
// with it for free: a width nothing was written at yields an empty run, and an
// empty run is not saved at all.
type carryover struct {
	cols  int
	bytes []byte
	at    time.Time
}

// The file is a header LINE followed by raw bytes.
//
//	atrium-scrollback <version> <cols> <length> <unix seconds> <card id>\n
//	....the bytes....
//
// Not JSON, and the payload is why: it is arbitrary terminal output including
// escape sequences and partial runes, so JSON would mean base64, which is a
// third more bytes and a decode pass over megabytes to buy nothing. A single
// space-separated line parses with one Cut and every field is a number or an
// id that cannot contain a space.
//
// The length is in the header so a truncated file is recognisable as one. A
// daemon killed mid-write is the case, and the payload is written to a
// temporary name and renamed so it usually cannot happen at all.

// carryNameOK says whether an id is safe to use as a file name.
//
// Card ids are ULID-ish text keys, so this is a whitelist rather than an
// escape: anything with a separator, a dot, or a space in it is not an id this
// daemon issued and has no business naming a file.
func carryNameOK(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, c := range id {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

func carryPath(dir, id string) string { return filepath.Join(dir, id+carrySuffix) }

// writeCarry puts one card's scrollback on disk.
//
// Temporary name then rename, so a daemon killed part way through leaves the
// previous file intact rather than a half one. The length check on the way
// back in covers what is left of the case.
func writeCarry(dir, id string, c *carryover) error {
	if !carryNameOK(id) || c == nil || len(c.bytes) == 0 {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	head := fmt.Sprintf("%s %d %d %d %d %s\n",
		carryMagic, carryVersion, c.cols, len(c.bytes), time.Now().Unix(), id)
	tmp := carryPath(dir, id) + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.WriteString(head)
	if err == nil {
		_, err = f.Write(c.bytes)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, carryPath(dir, id))
}

// readCarry returns what was saved for a card, or nil.
//
// NIL FOR EVERYTHING, and that is the posture the whole function is written
// in. Missing, empty, truncated, a header from a format that no longer exists,
// a length that disagrees with the file, a width that is not a width, a file
// far larger than any ring: all of them mean "there is no scrollback to
// carry", which is the state atrium was in before any of this existed. The
// halt-on-storage-failure rule is about the DATABASE, where a wrong answer
// silently loses work. This is a convenience over bytes that were always
// disposable, and refusing to start over one would be a self-inflicted outage.
func readCarry(dir, id string, max int) *carryover {
	if !carryNameOK(id) || max <= 0 {
		return nil
	}
	path := carryPath(dir, id)
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return nil
	}
	// Read nothing enormous. A ring is bounded by construction, so a file
	// bigger than one plus a header line did not come from a ring this daemon
	// would write.
	if st.Size() > int64(max)+512 {
		log.Printf("[atrium] ignoring %s: %d bytes is larger than a scrollback buffer",
			path, st.Size())
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	head, payload, ok := bytes.Cut(raw, []byte("\n"))
	if !ok {
		return nil
	}
	fields := strings.Fields(string(head))
	if len(fields) != 6 || fields[0] != carryMagic {
		return nil
	}
	ver, err1 := strconv.Atoi(fields[1])
	cols, err2 := strconv.Atoi(fields[2])
	n, err3 := strconv.Atoi(fields[3])
	secs, err4 := strconv.ParseInt(fields[4], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
		return nil
	}
	if ver != carryVersion || cols <= 0 || cols > 10000 || fields[5] != id {
		return nil
	}
	// The truncation test. A short file is the shape a kill leaves, and
	// replaying half a stream is worse than replaying none: the tail of it can
	// be half an escape sequence.
	if n <= 0 || n != len(payload) {
		return nil
	}
	return &carryover{cols: cols, bytes: payload, at: time.Unix(secs, 0)}
}

// carryDir is where this daemon keeps them.
func (d *Daemon) carryDir() string {
	return filepath.Join(filepath.Dir(d.opts.DBPath), carryDirName)
}

// adoptCarryover hands a freshly spawned runner whatever the last daemon left
// for its card.
//
// ONCE PER CARD PER DAEMON, which is what `claimCarry` enforces. The case
// being fixed is the restart, so the file is read the first time a card gets a
// runner and never again: a card relaunched by hand three hours later is new
// work, and prepending this morning's output to it would be a second bug
// wearing the first one's clothes.
//
// The file is deliberately NOT deleted once claimed. It costs a ring's worth
// of disk, it is rewritten at the next clean stop, and leaving it is the only
// thing that gives a daemon which was killed rather than stopped anything at
// all to come back to.
func (d *Daemon) adoptCarryover(r *runner) {
	if r == nil || !d.sup.claimCarry(r.taskID) {
		return
	}
	c := readCarry(d.carryDir(), r.taskID, api.ScrollbackBytes(d.st))
	if c == nil {
		return
	}
	r.mu.Lock()
	r.carried = c
	r.mu.Unlock()
	log.Printf("[atrium] %s starts with %d bytes of scrollback from before the restart",
		r.taskID, len(c.bytes))
}

// carryFrom is what this runner would have written out right now.
//
// The carried buffer is folded back in when the width still agrees, so
// scrollback survives a SECOND restart rather than only the one it was
// written for. Bounded the same way the ring is: the tail, cut to `max`, and
// then to a line start, because a cut at an arbitrary byte can land inside an
// escape sequence or a rune.
func (r *runner) carryFrom(max int) *carryover {
	if r == nil || r.buf == nil || max <= 0 {
		return nil
	}
	cols := r.buf.CurrentWidth()
	live, _ := r.buf.SnapshotAt(cols)

	r.mu.Lock()
	carried := r.carried
	r.mu.Unlock()

	out := live
	if carried != nil && carried.cols == cols {
		out = make([]byte, 0, len(carried.bytes)+len(carryDivider)+len(live))
		out = append(out, carried.bytes...)
		out = append(out, carryDivider...)
		out = append(out, live...)
	}
	if len(out) > max {
		out = fromLineStart(out[len(out)-max:])
	}
	if len(out) == 0 {
		return nil
	}
	return &carryover{cols: cols, bytes: out}
}

// saveCarryover writes every live runner's scrollback out.
//
// Called from the wind-down, after the runners have been asked to stop, so
// whatever a session said on its way out is included. Bounded by
// `carrySaveBudget` across all of them together, and every failure is logged
// and stepped over: a shutdown must not fail or hang because of this.
func (d *Daemon) saveCarryover(rs []*runner) {
	d.saveCarryoverBy(rs, time.Now().Add(carrySaveBudget))
}

// saveCarryoverBy is saveCarryover with the deadline handed in, so a test can
// name the thing that must not happen rather than time it.
func (d *Daemon) saveCarryoverBy(rs []*runner, deadline time.Time) {
	if len(rs) == 0 {
		return
	}
	dir := d.carryDir()
	max := api.ScrollbackBytes(d.st)
	saved := 0
	for _, r := range rs {
		if time.Now().After(deadline) {
			log.Printf("[atrium] out of time saving scrollback, %d of %d card(s) written",
				saved, len(rs))
			return
		}
		c := r.carryFrom(max)
		if c == nil {
			continue
		}
		if err := writeCarry(dir, r.taskID, c); err != nil {
			log.Printf("[atrium] could not save scrollback for %s: %v", r.taskID, err)
			continue
		}
		saved++
	}
	if saved > 0 {
		log.Printf("[atrium] saved scrollback for %d card(s)", saved)
	}
}

// sweepCarryover deletes files nothing will ever read again.
//
// A card that is gone leaves one behind, and so does a daemon killed mid-write
// and a database that was pointed somewhere else. Runs on the reaper's ticker
// rather than on one of its own, because it is the same question the sweep
// beside it asks and a second timer is a second thing to get wrong at
// shutdown.
//
// A CARD IS ONLY GONE WHEN THE STORE SAYS SO. Any other error, including the
// halt, leaves the file where it is: deleting somebody's scrollback because
// the database could not be read is exactly the wrong way to be wrong.
func (d *Daemon) sweepCarryover() {
	dir := d.carryDir()
	ents, err := os.ReadDir(dir)
	if err != nil {
		// No directory yet is the ordinary case, not a problem.
		return
	}
	cutoff := time.Now().Add(-carryKeep)
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name())
		id, ok := strings.CutSuffix(e.Name(), carrySuffix)
		if !ok || !carryNameOK(id) {
			// Not a name this daemon writes. A `.tmp` from a kill is the one
			// that turns up, and it is only removed once it is old enough that
			// no live write could be holding it.
			if info, err := e.Info(); err == nil && info.ModTime().Before(cutoff) {
				_ = os.Remove(path)
			}
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(path)
			continue
		}
		if _, err := d.st.Get(id); errors.Is(err, sql.ErrNoRows) {
			_ = os.Remove(path)
		}
	}
}
