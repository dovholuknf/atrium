package store

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The ledger as a file anybody can read with the daemon down.
//
// `work-ledger.md` beside the room's database is rewritten after every change
// to the ledger. It is a PROJECTION: nothing ever reads it back, and a write
// that fails is logged by the caller and ignored, the posture of a cold event
// sink. When the daemon died between a change and the rename, `atrium ledger`
// prints the same list from the database itself.

// LedgerFileName is the snapshot's name, beside the database.
const LedgerFileName = "work-ledger.md"

// LedgerFilePath is where a database's snapshot goes.
func LedgerFilePath(dbPath string) string {
	return filepath.Join(filepath.Dir(filepath.FromSlash(dbPath)), LedgerFileName)
}

// WriteLedgerFile renders the ledger and puts it at path, through a temporary
// file renamed over the old one, so a reader never sees half a list.
func (s *Store) WriteLedgerFile(path string) error {
	v, err := s.Ledger()
	if err != nil {
		return err
	}
	return writeFileAtomic(path, []byte(RenderLedger(v)))
}

func writeFileAtomic(path string, body []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".work-ledger-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// RenderLedger is the list as markdown: what is not closed, the crash case
// first, then what was closed in the last seven days.
func RenderLedger(v *LedgerView) string {
	var b strings.Builder
	b.WriteString("# Work ledger\n\n")
	fmt.Fprintf(&b, "Written by atrium at %s. A projection of the room's database, rewritten on every change.\n",
		v.At.Local().Format("2006-01-02 15:04:05"))
	b.WriteString("Nothing reads this file back. `atrium ledger` prints the same list from the database.\n\n")

	groups := []struct {
		state, head string
	}{
		{WorkEnded, "Ended without a report"},
		{WorkReported, "Reported, waiting on a verdict"},
		{WorkReopened, "Reopened"},
		{WorkOpen, "Open"},
	}
	fmt.Fprintf(&b, "## Not closed (%d)\n\n", len(v.Open))
	if len(v.Open) == 0 {
		b.WriteString("Nothing.\n\n")
	}
	for _, g := range groups {
		var in []*WorkItem
		for _, w := range v.Open {
			if w.State == g.state {
				in = append(in, w)
			}
		}
		if len(in) == 0 {
			continue
		}
		fmt.Fprintf(&b, "### %s (%d)\n\n", g.head, len(in))
		for _, w := range in {
			renderItem(&b, w, v.At)
		}
	}
	fmt.Fprintf(&b, "## Closed in the last %d days (%d)\n\n", int(LedgerClosedFor/(24*time.Hour)), len(v.Closed))
	if len(v.Closed) == 0 {
		b.WriteString("Nothing.\n")
	}
	for _, w := range v.Closed {
		renderItem(&b, w, v.At)
	}
	return b.String()
}

func renderItem(b *strings.Builder, w *WorkItem, at time.Time) {
	name := orKeep(w.Handle, w.TaskID)
	fmt.Fprintf(b, "- **%s**", name)
	if w.Title != "" && w.Title != name {
		fmt.Fprintf(b, " (%s)", w.Title)
	}
	fmt.Fprintf(b, ": %s for %s", w.State, since(at, w.StateAt))
	if w.Inferred {
		b.WriteString(", inferred by the backfill")
	}
	b.WriteString("\n")
	fmt.Fprintf(b, "  - launcher %s, arbiter %s, card `%s`\n",
		orKeep(w.LauncherHandle, "unknown"), orKeep(w.ArbiterHandle, "unknown"), w.TaskID)
	if w.Worktree != "" {
		fmt.Fprintf(b, "  - worktree `%s`\n", w.Worktree)
	}
	if line := firstLine(w.Brief); line != "" {
		fmt.Fprintf(b, "  - brief: %s\n", line)
	}
	if r := w.LastReport; r != nil {
		fmt.Fprintf(b, "  - last report: %s %s %s\n", orKeep(r.Status, "done"),
			r.At.Local().Format("2006-01-02 15:04"), reportWords(r))
	} else {
		b.WriteString("  - last report: none\n")
	}
	fmt.Fprintf(b, "  - outputs: %s\n", w.Outputs.Summary())
	b.WriteString("\n")
}

// reportWords is a report's first line quoted, or what atrium knows about it
// when it holds no words, as a backfilled report does not.
func reportWords(r *WorkLogEntry) string {
	if line := firstLine(r.Text); line != "" {
		return fmt.Sprintf("%q", line)
	}
	return "(" + orKeep(r.Note, "no summary") + ")"
}

// since is a coarse age: minutes, hours, or days.
func since(at, t time.Time) string {
	d := at.Sub(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
}

// ReadLedger reads the ledger from a database opened READ ONLY, with no
// migration and no write of any kind, so it is safe against a live room and
// against a room that is down. A database with no ledger yet answers an empty
// list.
func ReadLedger(dbPath string) (*LedgerView, error) {
	if _, err := os.Stat(dbPath); err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(dbPath), RawQuery: "mode=ro"}
	if !strings.HasPrefix(u.Path, "/") {
		// A drive letter, so `file:D:/...` rather than `file://D:/...`.
		u = url.URL{Scheme: "file", Opaque: filepath.ToSlash(dbPath), RawQuery: "mode=ro"}
	}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		return nil, err
	}
	var name string
	err = db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'work_item'`).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return &LedgerView{At: now()}, nil
	}
	if err != nil {
		return nil, err
	}
	return ledgerOn(db, now())
}
