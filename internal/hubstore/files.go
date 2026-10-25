package hubstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dovholuknf/atrium/internal/base/docshare"
)

// The hub file drop: a place a card leaves a file for a card on another room, or for the board. The design is
// docs/rnd/hub-file-drop.md, the tools are internal/link/files_mcp.go and the board's routes are
// internal/link/files_api.go.
//
// ── what lives where ────────────────────────────────────
//
// ROWS in SQLite (`hub_files`, migration 0017) and BYTES in files named for their SHA-256 under
// `<hub data dir>/files/`, written through a temp file and a rename as documents are. Equal bytes are one blob,
// and a blob goes only when no row names it.
//
// ── one chokepoint ──────────────────────────────────────
//
// FileAdd is the only way a row is made, and it applies every rule: the secret checks, the size, the lifetime, the
// per-card rate and the drop's total. The tools and the board differ only in who they say wrote it.
//
// A DROP FILE IS DISPOSABLE. It expires, the board can delete it, and a restored hub does not have its bytes: the
// ten-minute snapshots cover the rows and not the blobs. Nothing may treat one as kept.

// Default caps and the hub setting they are kept in.
const (
	SettingFiles = "files_settings"

	DefaultFileCap int64 = 32 << 20
	// MaxFileCap is the most the operator can raise the cap to: the room takes a file of 32 MiB and no more, so a larger
	// hub cap would store files that atrium_get cannot fetch.
	MaxFileCap             int64 = 32 << 20
	DefaultFileTotalCap    int64 = 512 << 20
	DefaultFilePerCardHour       = 30
	DefaultFileTTLDays           = 7
	DefaultFileMaxTTLDays        = 30

	fileNoteMax = 500
)

// FileCaps are the limits the operator can change. The JSON names are the API's.
type FileCaps struct {
	File        int64 `json:"file"`
	Total       int64 `json:"total"`
	PerCardHour int   `json:"per_card_hour"`
	TTLDays     int   `json:"ttl_days"`
	MaxTTLDays  int   `json:"max_ttl_days"`
}

// FileSettings is the caps.
type FileSettings struct {
	Caps FileCaps `json:"caps"`
}

func defaultFileSettings() FileSettings {
	return FileSettings{Caps: FileCaps{
		File: DefaultFileCap, Total: DefaultFileTotalCap, PerCardHour: DefaultFilePerCardHour,
		TTLDays: DefaultFileTTLDays, MaxTTLDays: DefaultFileMaxTTLDays}}
}

// FileSettings reads the caps. A value that will not parse, or a cap that is not positive, reads as the default, so
// a bad write cannot turn the drop into one that refuses everything or accepts without limit.
func (s *Store) FileSettings() (FileSettings, error) {
	out := defaultFileSettings()
	v, err := s.HubSetting(SettingFiles)
	if err != nil || strings.TrimSpace(v) == "" {
		return out, err
	}
	var got FileSettings
	if json.Unmarshal([]byte(v), &got) != nil {
		return out, nil
	}
	if got.Caps.File > 0 {
		out.Caps.File = min(got.Caps.File, MaxFileCap)
	}
	if got.Caps.Total > 0 {
		out.Caps.Total = got.Caps.Total
	}
	if got.Caps.PerCardHour > 0 {
		out.Caps.PerCardHour = got.Caps.PerCardHour
	}
	if got.Caps.TTLDays > 0 {
		out.Caps.TTLDays = got.Caps.TTLDays
	}
	if got.Caps.MaxTTLDays > 0 {
		out.Caps.MaxTTLDays = got.Caps.MaxTTLDays
	}
	return out, nil
}

// SetFileSettings applies a patch and answers what is now in force. A zero field is left alone and a negative one is
// refused rather than stored.
func (s *Store) SetFileSettings(p FileCaps) (FileSettings, error) {
	cur, err := s.FileSettings()
	if err != nil {
		return cur, err
	}
	if p.File < 0 || p.Total < 0 || p.PerCardHour < 0 || p.TTLDays < 0 || p.MaxTTLDays < 0 {
		return cur, &DocError{Kind: DocBad, Msg: "a cap has to be more than zero"}
	}
	if p.File > 0 {
		cur.Caps.File = min(p.File, MaxFileCap)
	}
	if p.Total > 0 {
		cur.Caps.Total = p.Total
	}
	if p.PerCardHour > 0 {
		cur.Caps.PerCardHour = p.PerCardHour
	}
	if p.TTLDays > 0 {
		cur.Caps.TTLDays = p.TTLDays
	}
	if p.MaxTTLDays > 0 {
		cur.Caps.MaxTTLDays = p.MaxTTLDays
	}
	if cur.Caps.TTLDays > cur.Caps.MaxTTLDays {
		return cur, &DocError{Kind: DocBad, Msg: "the default lifetime cannot be longer than the longest one"}
	}
	raw, _ := json.Marshal(cur)
	return cur, s.SetHubSetting(SettingFiles, string(raw))
}

// FileRow is one drop file as the API shows it.
type FileRow struct {
	Name      string `json:"name"`
	Class     string `json:"class"`
	Size      int64  `json:"size"`
	SHA       string `json:"sha"`
	Origin    string `json:"origin"`
	By        string `json:"by"`
	Card      string `json:"card"`
	Room      string `json:"room"`
	Note      string `json:"note"`
	At        string `json:"at"`
	Expires   string `json:"expires"`
	Downloads int    `json:"downloads"`
	LastGet   string `json:"last_get"`
	Missing   bool   `json:"missing"`
}

// FileInput is one file to add.
type FileInput struct {
	// Name is the name asked for. The hub picks another when it is taken.
	Name string
	Note string

	// Origin, By, Card and Room are decided by the hub from the request and never read from a form. Origin is
	// local, share or card, and Card is room~id.
	Origin string
	By     string
	Card   string
	Room   string

	Data []byte
	// TTLDays is how long it is kept. Zero is the setting's default.
	TTLDays int
	// Rate counts the write against its card's hourly puts. A put only.
	Rate bool
}

func (s *Store) fileBlob(sha string) string { return filepath.Join(s.filesDir, sha) }

// FilesDir is the folder the drop's bytes are in.
func (s *Store) FilesDir() string { return s.filesDir }

// filesUsageSQL is the bytes held: each blob once.
const filesUsageSQL = `SELECT COALESCE(SUM(sz), 0) FROM (SELECT MAX(size) AS sz FROM hub_files GROUP BY sha)`

// FileAdd makes a row and its blob, applying every rule.
func (s *Store) FileAdd(in FileInput) (FileRow, error) {
	var row FileRow
	set, err := s.FileSettings()
	if err != nil {
		return row, err
	}
	switch in.Origin {
	case "local", "share", "card":
	default:
		return row, &DocError{Kind: DocBad, Msg: "a file needs an origin"}
	}
	if len(in.Data) == 0 {
		return row, &DocError{Kind: DocBad, Msg: "that file is empty"}
	}
	name := cleanName(in.Name)
	if name == "" {
		return row, &DocError{Kind: DocBad, Msg: "a file needs a name"}
	}
	if docshare.SecretFileName(name) {
		return row, &DocError{Kind: DocSecret, Rule: docshare.RuleFileName, Msg: "that file name looks like a secret, so it was " +
			"not stored. the drop is readable by everyone past the board's password"}
	}
	if int64(len(in.Data)) > set.Caps.File {
		return row, &DocError{Kind: DocTooBig, Msg: fmt.Sprintf(
			"that is %d bytes, over the %d byte cap for a drop file", len(in.Data), set.Caps.File)}
	}
	if rule := docshare.SecretContent(in.Data); rule != "" {
		return row, &DocError{Kind: DocSecret, Rule: rule, Msg: "that looks like it holds a secret (" + rule +
			"). the drop is readable by everyone past the board's password, so it was not stored. " +
			"this check is a speed bump and not a guarantee"}
	}
	days := in.TTLDays
	if days == 0 {
		days = set.Caps.TTLDays
	}
	if days < 0 || days > set.Caps.MaxTTLDays {
		return row, &DocError{Kind: DocBad, Msg: fmt.Sprintf("a file is kept for 1 to %d days", set.Caps.MaxTTLDays)}
	}
	note := in.Note
	if utf8.RuneCountInString(note) > fileNoteMax {
		note = string([]rune(note)[:fileNoteMax])
	}
	sha := hexSHA(in.Data)

	// SERIALISED WITH DELETE AND THE SWEEP, for the reason DocAdd gives about a purge: the blob is written inside
	// the transaction and a removal happens after its own commit.
	s.filesMu.Lock()
	defer s.filesMu.Unlock()
	err = s.tx(func(t *sql.Tx) error {
		row = FileRow{}
		if in.Rate && in.Card != "" {
			var recent int
			since := ts(s.now().Add(-time.Hour))
			if err := t.QueryRow(`SELECT COUNT(*) FROM hub_files WHERE card = ? AND at > ?`,
				in.Card, since).Scan(&recent); err != nil {
				return err
			}
			if recent >= set.Caps.PerCardHour {
				return docRefuse(DocRate, fmt.Sprintf(
					"this card has put %d files in the last hour, which is the cap. try again later", recent))
			}
		}
		var used int64
		if err := t.QueryRow(filesUsageSQL).Scan(&used); err != nil {
			return err
		}
		var have int
		if err := t.QueryRow(`SELECT COUNT(*) FROM hub_files WHERE sha = ?`, sha).Scan(&have); err != nil {
			return err
		}
		if have == 0 && used+int64(len(in.Data)) > set.Caps.Total {
			return docRefuse(DocFull, fmt.Sprintf(
				"the file drop is full: %d of %d bytes used. delete files on the Files page to make room",
				used, set.Caps.Total))
		}
		chosen, err := freeFileName(t, name)
		if err != nil {
			return err
		}
		// The blob first inside the transaction, so a failure to write it rolls the row back. One left behind by a
		// later failure is harmless: it is named for its bytes, and the sweep removes it when no row names it.
		if err := writeBlobIn(s.filesDir, sha, in.Data); err != nil {
			return fmt.Errorf("could not store the bytes: %w", err)
		}
		at := s.now()
		row = FileRow{Name: chosen, Class: "drop", Size: int64(len(in.Data)), SHA: sha, Origin: in.Origin,
			By: in.By, Card: in.Card, Room: in.Room, Note: note, At: ts(at),
			Expires: ts(at.Add(time.Duration(days) * 24 * time.Hour))}
		_, err = t.Exec(`INSERT INTO hub_files
			(name, class, size, sha, origin, by, card, room, note, at, expires)
			VALUES (?, 'drop', ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			row.Name, row.Size, row.SHA, row.Origin, row.By, row.Card, row.Room, row.Note, row.At, row.Expires)
		return err
	})
	if err != nil {
		return FileRow{}, err
	}
	return row, nil
}

// freeFileName is name, or stem-2.ext, stem-3.ext and so on, whichever no row has. A file is never replaced.
func freeFileName(t *sql.Tx, name string) (string, error) {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	if stem == "" {
		// A dotfile has no stem, and ".x-2" reads better than "-2.x".
		stem, ext = name, ""
	}
	for i := 1; ; i++ {
		cand := name
		if i > 1 {
			cand = fmt.Sprintf("%s-%d%s", stem, i, ext)
		}
		var n int
		if err := t.QueryRow(`SELECT COUNT(*) FROM hub_files WHERE name = ?`, cand).Scan(&n); err != nil {
			return "", err
		}
		if n == 0 {
			return cand, nil
		}
	}
}

const fileCols = `name, class, size, sha, origin, by, card, room, note, at, expires, downloads, last_get`

func scanFile(sc interface{ Scan(...any) error }) (FileRow, error) {
	var r FileRow
	err := sc.Scan(&r.Name, &r.Class, &r.Size, &r.SHA, &r.Origin, &r.By, &r.Card, &r.Room, &r.Note,
		&r.At, &r.Expires, &r.Downloads, &r.LastGet)
	return r, err
}

// FileList is the files not yet expired, newest first. A non-empty card lists only that card's.
func (s *Store) FileList(card string) ([]FileRow, error) {
	out := []FileRow{}
	err := s.guard(func() error {
		out = out[:0]
		q, args := `SELECT `+fileCols+` FROM hub_files WHERE expires > ?`, []any{ts(s.now())}
		if card != "" {
			q += ` AND card = ?`
			args = append(args, card)
		}
		rows, err := s.db.Query(q+` ORDER BY at DESC, name`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanFile(rows)
			if err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	for i := range out {
		if _, err := os.Stat(s.fileBlob(out[i].SHA)); err != nil {
			out[i].Missing = true
		}
	}
	return out, nil
}

// FileUsage is the bytes the drop holds, each blob once.
func (s *Store) FileUsage() (int64, error) {
	var n int64
	err := s.guard(func() error { return s.db.QueryRow(filesUsageSQL).Scan(&n) })
	return n, err
}

// FileOpen opens the bytes of a file and counts a download. The caller closes the file. 404 for no such name, 410 for
// one past its date or whose bytes are gone.
func (s *Store) FileOpen(name string) (*os.File, FileRow, error) {
	var r FileRow
	err := s.guard(func() error {
		var err error
		r, err = scanFile(s.db.QueryRow(`SELECT `+fileCols+` FROM hub_files WHERE name = ?`, name))
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, FileRow{}, &DocError{Kind: DocNone, Msg: "there is no file called " + name}
	}
	if err != nil {
		return nil, FileRow{}, err
	}
	if r.Expires <= ts(s.now()) {
		return nil, r, &DocError{Kind: DocGone, Msg: "that file expired on " + r.Expires}
	}
	f, err := os.Open(s.fileBlob(r.SHA))
	if err != nil {
		r.Missing = true
		return nil, r, &DocError{Kind: DocGone, Msg: "the bytes are missing"}
	}
	err = s.guard(func() error {
		_, err := s.db.Exec(`UPDATE hub_files SET downloads = downloads + 1, last_get = ? WHERE name = ?`,
			ts(s.now()), name)
		return err
	})
	if err != nil {
		f.Close()
		return nil, r, err
	}
	r.Downloads++
	return f, r, nil
}

// FileDelete removes a row, and its blob when no other row names it. A blob that cannot be removed, which is what a
// reader holding it open does on Windows, is logged and the sweep retries.
func (s *Store) FileDelete(name string) error {
	s.filesMu.Lock()
	defer s.filesMu.Unlock()
	var sha string
	var left int
	err := s.tx(func(t *sql.Tx) error {
		if err := t.QueryRow(`SELECT sha FROM hub_files WHERE name = ?`, name).Scan(&sha); err != nil {
			if err == sql.ErrNoRows {
				return docRefuse(DocNone, "there is no file called "+name)
			}
			return err
		}
		if _, err := t.Exec(`DELETE FROM hub_files WHERE name = ?`, name); err != nil {
			return err
		}
		return t.QueryRow(`SELECT COUNT(*) FROM hub_files WHERE sha = ?`, sha).Scan(&left)
	})
	if err != nil {
		return err
	}
	if left == 0 {
		if err := os.Remove(s.fileBlob(sha)); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("[hub] deleted file %s but could not remove its bytes yet, the sweep will: %v", name, err)
		}
	}
	return nil
}

// FileSweepEvery is how often the sweep runs.
const FileSweepEvery = time.Hour

// fileTmpStale is how old a temp file in the drop's folder has to be before it is taken for a crash's leftover.
const fileTmpStale = time.Hour

// FileSweep deletes rows past their date, then every blob no row names, then a stale temp file. A file past its date
// is already refused by FileOpen, so this is housekeeping and not what enforces the date.
func (s *Store) FileSweep() (rows, blobs int, err error) {
	s.filesMu.Lock()
	defer s.filesMu.Unlock()
	err = s.tx(func(t *sql.Tx) error {
		res, err := t.Exec(`DELETE FROM hub_files WHERE expires <= ?`, ts(s.now()))
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		rows = int(n)
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	held := map[string]bool{}
	err = s.guard(func() error {
		for k := range held {
			delete(held, k)
		}
		r, err := s.db.Query(`SELECT DISTINCT sha FROM hub_files`)
		if err != nil {
			return err
		}
		defer r.Close()
		for r.Next() {
			var sha string
			if err := r.Scan(&sha); err != nil {
				return err
			}
			held[sha] = true
		}
		return r.Err()
	})
	if err != nil {
		return rows, 0, err
	}
	entries, err := os.ReadDir(s.filesDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return rows, 0, nil
		}
		return rows, 0, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".tmp-") {
			if info, err := e.Info(); err == nil && s.now().Sub(info.ModTime()) > fileTmpStale {
				if os.Remove(filepath.Join(s.filesDir, name)) == nil {
					blobs++
				}
			}
			continue
		}
		if held[name] {
			continue
		}
		if err := os.Remove(filepath.Join(s.filesDir, name)); err != nil {
			log.Printf("[hub] file sweep: could not remove %s: %v", name, err)
			continue
		}
		blobs++
	}
	return rows, blobs, nil
}

// FileSweepLoop sweeps the drop on a timer until ctx is done. Started beside VacuumLoop.
func (s *Store) FileSweepLoop(ctx context.Context) {
	t := time.NewTicker(FileSweepEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, _, err := s.FileSweep(); err != nil {
				log.Printf("[hub] file sweep: %v", err)
			}
		}
	}
}
