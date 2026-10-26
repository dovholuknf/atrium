//go:build integration

package hubstore

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/base/docshare"
)

func fileIn(name, body string) FileInput {
	return FileInput{Name: name, Data: []byte(body), Origin: "local", By: "operator"}
}

func mustFile(t *testing.T, s *Store, in FileInput) FileRow {
	t.Helper()
	r, err := s.FileAdd(in)
	if err != nil {
		t.Fatalf("add %q: %v", in.Name, err)
	}
	return r
}

// THE MIGRATION CAN RUN TWICE, the rule at the top of schema.go.
func TestFilesMigrationToleratesBeingThere(t *testing.T) {
	s := open(t)
	for i, m := range migrations {
		if m.name != "0017_hub_files" {
			continue
		}
		if i < len(migrations)-19 {
			t.Fatalf("0017_hub_files is at %d of %d, a migration goes at the end", i, len(migrations))
		}
		for _, st := range m.stmts {
			if _, err := s.db.Exec(st); err != nil {
				t.Fatalf("running %q a second time: %v", st, err)
			}
		}
		return
	}
	t.Fatal("no 0017_hub_files migration")
}

func TestFilesAddListOpenDelete(t *testing.T) {
	s := open(t)
	r := mustFile(t, s, fileIn("out.jsonl", "one\ntwo\n"))
	if r.Name != "out.jsonl" || r.Class != "drop" || r.Size != 8 {
		t.Fatalf("row = %+v", r)
	}
	list, err := s.FileList("")
	if err != nil || len(list) != 1 || list[0].Name != "out.jsonl" || list[0].Missing {
		t.Fatalf("list = %+v, %v", list, err)
	}
	f, row, err := s.FileOpen("out.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(f)
	f.Close()
	if string(b) != "one\ntwo\n" || row.Downloads != 1 {
		t.Fatalf("bytes %q, downloads %d", b, row.Downloads)
	}
	list, _ = s.FileList("")
	if list[0].Downloads != 1 || list[0].LastGet == "" {
		t.Fatalf("the download was not counted: %+v", list[0])
	}
	blob := filepath.Join(s.FilesDir(), r.SHA)
	if _, err := os.Stat(blob); err != nil {
		t.Fatalf("no blob: %v", err)
	}
	if err := s.FileDelete("out.jsonl"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(blob); !os.IsNotExist(err) {
		t.Fatalf("the blob is still there: %v", err)
	}
	_, _, err = s.FileOpen("out.jsonl")
	wantKind(t, err, DocNone)
	wantKind(t, s.FileDelete("out.jsonl"), DocNone)
}

func TestFilesNameClashGetsASuffix(t *testing.T) {
	s := open(t)
	a := mustFile(t, s, fileIn("usage.jsonl", "a"))
	b := mustFile(t, s, fileIn("usage.jsonl", "b"))
	c := mustFile(t, s, fileIn("usage.jsonl", "c"))
	if a.Name != "usage.jsonl" || b.Name != "usage-2.jsonl" || c.Name != "usage-3.jsonl" {
		t.Fatalf("names %q %q %q", a.Name, b.Name, c.Name)
	}
	f, _, err := s.FileOpen("usage.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(f)
	f.Close()
	if string(got) != "a" {
		t.Fatalf("the first file was replaced: %q", got)
	}
}

func TestFilesSharedBlobSurvivesOneDelete(t *testing.T) {
	s := open(t)
	a := mustFile(t, s, fileIn("a.txt", "same bytes"))
	mustFile(t, s, fileIn("b.txt", "same bytes"))
	if err := s.FileDelete("a.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.FilesDir(), a.SHA)); err != nil {
		t.Fatalf("the shared blob went with one name: %v", err)
	}
	f, _, err := s.FileOpen("b.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if n, _ := s.FileUsage(); n != int64(len("same bytes")) {
		t.Fatalf("usage counts a blob once, got %d", n)
	}
}

func TestFilesCapsAndRate(t *testing.T) {
	s := open(t)
	clock := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	s.clock = func() time.Time { return clock }

	if _, err := s.SetFileSettings(FileCaps{File: 10, Total: 25}); err != nil {
		t.Fatal(err)
	}
	_, err := s.FileAdd(fileIn("big.bin", "0123456789A"))
	wantKind(t, err, DocTooBig)
	mustFile(t, s, fileIn("a", "0123456789"))
	mustFile(t, s, fileIn("b", "abcdefghij"))
	_, err = s.FileAdd(fileIn("c", "ABCDEFGHIJ"))
	wantKind(t, err, DocFull)
	// Bytes already held cost nothing.
	mustFile(t, s, fileIn("a-again", "0123456789"))

	if _, err := s.SetFileSettings(FileCaps{File: DefaultFileCap, Total: DefaultFileTotalCap}); err != nil {
		t.Fatal(err)
	}
	put := func(card, body string) error {
		_, err := s.FileAdd(FileInput{Name: "p.txt", Data: []byte(body), Origin: "card", By: "r@sg4",
			Card: card, Room: "sg4", Rate: true})
		return err
	}
	for i := 0; i < 30; i++ {
		if err := put("sg4~a", fmt.Sprintf("body %d", i)); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}
	wantKind(t, put("sg4~a", "the 31st"), DocRate)
	if err := put("sg4~b", "other card"); err != nil {
		t.Fatalf("another card is limited too: %v", err)
	}
	mustFile(t, s, fileIn("upload.txt", "board upload"))
	clock = clock.Add(61 * time.Minute)
	if err := put("sg4~a", "later"); err != nil {
		t.Fatalf("the hour passed: %v", err)
	}

	// A bad value reads as the default.
	if err := s.SetHubSetting(SettingFiles, `{"caps":{"file":-5,"total":0,"per_card_hour":-1}}`); err != nil {
		t.Fatal(err)
	}
	set, _ := s.FileSettings()
	if set.Caps != defaultFileSettings().Caps {
		t.Fatalf("a bad setting read as %+v", set.Caps)
	}
	if err := s.SetHubSetting(SettingFiles, `not json`); err != nil {
		t.Fatal(err)
	}
	if set, _ = s.FileSettings(); set.Caps != defaultFileSettings().Caps {
		t.Fatalf("junk read as %+v", set.Caps)
	}
	_, err = s.SetFileSettings(FileCaps{File: -1})
	wantKind(t, err, DocBad)
}

func TestFilesSecretChecks(t *testing.T) {
	s := open(t)
	_, err := s.FileAdd(fileIn(".env", "A=1"))
	if de := wantKind(t, err, DocSecret); de.Rule != docshare.RuleFileName {
		t.Fatalf("rule %q", de.Rule)
	}
	pem := "-----BEGIN RSA PRIVATE KEY-----\n" + string(bytes.Repeat([]byte("MIIEowIBAAKCAQEA"), 20)) +
		"\n-----END RSA PRIVATE KEY-----\n"
	_, err = s.FileAdd(fileIn("notes.txt", pem))
	if de := wantKind(t, err, DocSecret); de.Rule == "" || de.Rule == docshare.RuleFileName {
		t.Fatalf("rule %q", de.Rule)
	}
	_, err = s.FileAdd(fileIn("empty.txt", ""))
	wantKind(t, err, DocBad)
	_, err = s.FileAdd(FileInput{Name: "x", Data: []byte("x"), Origin: "bogus"})
	wantKind(t, err, DocBad)
	_, err = s.FileAdd(fileIn("///", "x"))
	wantKind(t, err, DocBad)
}

func TestFilesExpireAndSweep(t *testing.T) {
	s := open(t)
	clock := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	s.clock = func() time.Time { return clock }

	r := mustFile(t, s, fileIn("old.txt", "old bytes"))
	if err := os.MkdirAll(s.FilesDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(s.FilesDir(), ".tmp-abc")
	fresh := filepath.Join(s.FilesDir(), ".tmp-def")
	orphan := filepath.Join(s.FilesDir(), "deadbeef")
	for _, p := range []string{stale, fresh, orphan} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(stale, clock.Add(-2*time.Hour), clock.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(fresh, clock, clock); err != nil {
		t.Fatal(err)
	}

	clock = clock.Add(8 * 24 * time.Hour)
	_, _, err := s.FileOpen("old.txt")
	wantKind(t, err, DocGone)
	if list, _ := s.FileList(""); len(list) != 0 {
		t.Fatalf("an expired file is listed: %+v", list)
	}
	// The fresh temp file is a day older than when it was made now, so it is stale too. Put it back.
	if err := os.Chtimes(fresh, clock, clock); err != nil {
		t.Fatal(err)
	}
	rows, blobs, err := s.FileSweep()
	if err != nil || rows != 1 || blobs != 3 {
		t.Fatalf("sweep rows=%d blobs=%d err=%v", rows, blobs, err)
	}
	for _, p := range []string{filepath.Join(s.FilesDir(), r.SHA), stale, orphan} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s survived the sweep", p)
		}
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("a fresh temp file was swept: %v", err)
	}
}

func TestFilesMissingBytesAre410(t *testing.T) {
	s := open(t)
	r := mustFile(t, s, fileIn("gone.txt", "bytes"))
	if err := os.Remove(filepath.Join(s.FilesDir(), r.SHA)); err != nil {
		t.Fatal(err)
	}
	_, _, err := s.FileOpen("gone.txt")
	if de := wantKind(t, err, DocGone); de.Msg != "the bytes are missing" {
		t.Fatalf("msg %q", de.Msg)
	}
	if list, _ := s.FileList(""); len(list) != 1 || !list[0].Missing {
		t.Fatalf("list = %+v", list)
	}
}

func TestFilesTTLBounds(t *testing.T) {
	s := open(t)
	clock := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	s.clock = func() time.Time { return clock }

	r := mustFile(t, s, fileIn("a", "a"))
	if want := ts(clock.Add(7 * 24 * time.Hour)); r.Expires != want {
		t.Fatalf("default expiry %s, want %s", r.Expires, want)
	}
	in := fileIn("b", "b")
	in.TTLDays = 30
	if r := mustFile(t, s, in); r.Expires != ts(clock.Add(30*24*time.Hour)) {
		t.Fatalf("30 days gave %s", r.Expires)
	}
	in = fileIn("c", "c")
	in.TTLDays = 31
	_, err := s.FileAdd(in)
	wantKind(t, err, DocBad)
	in.TTLDays = -1
	_, err = s.FileAdd(in)
	wantKind(t, err, DocBad)
}

func TestFilesListByCardAndNoteCut(t *testing.T) {
	s := open(t)
	in := FileInput{Name: "a", Data: []byte("a"), Origin: "card", Card: "sg4~1", Room: "sg4",
		Note: string(bytes.Repeat([]byte("Ã©"), 600))}
	r := mustFile(t, s, in)
	if n := len([]rune(r.Note)); n != fileNoteMax {
		t.Fatalf("note is %d runes", n)
	}
	mustFile(t, s, fileIn("b", "b"))
	if list, _ := s.FileList("sg4~1"); len(list) != 1 || list[0].Name != "a" {
		t.Fatalf("list by card = %+v", list)
	}
	if list, _ := s.FileList(""); len(list) != 2 {
		t.Fatalf("list = %+v", list)
	}
}

func TestFilesCapIsClampedToWhatTheRoomTakes(t *testing.T) {
	s := open(t)
	set, err := s.SetFileSettings(FileCaps{File: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if set.Caps.File != MaxFileCap {
		t.Fatalf("cap = %d", set.Caps.File)
	}
	got, err := s.FileSettings()
	if err != nil || got.Caps.File != MaxFileCap {
		t.Fatalf("stored cap = %d, %v", got.Caps.File, err)
	}
}
