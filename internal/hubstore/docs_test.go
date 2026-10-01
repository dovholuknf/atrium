package hubstore

import (
	"bytes"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func docIn(title, name, body string) DocInput {
	return DocInput{Title: title, Name: name, Data: []byte(body), Origin: "local", By: "operator"}
}

func mustAdd(t *testing.T, s *Store, in DocInput) DocResult {
	t.Helper()
	r, err := s.DocAdd(in)
	if err != nil {
		t.Fatalf("add %q: %v", in.Title, err)
	}
	return r
}

func wantKind(t *testing.T, err error, kind DocErrKind) *DocError {
	t.Helper()
	de, ok := AsDocError(err)
	if !ok || de.Kind != kind {
		t.Fatalf("wanted a doc error of kind %d, got %v", kind, err)
	}
	return de
}

// THE MIGRATION CAN RUN TWICE. Every statement tolerates already being there, the rule at the top of schema.go.
func TestDocsMigrationToleratesBeingThere(t *testing.T) {
	s := open(t)
	for _, m := range migrations {
		if m.name != "0007_docs" {
			continue
		}
		for _, st := range m.stmts {
			if _, err := s.db.Exec(st); err != nil {
				t.Fatalf("running %q a second time: %v", st[:40], err)
			}
		}
		return
	}
	t.Fatal("no 0007_docs migration")
}

// THE DOCS MIGRATION IS LAST, so an existing database applies it.
func TestDocsMigrationIsAtTheEnd(t *testing.T) {
	if got := migrations[len(migrations)-1].name; got != "0007_docs" {
		t.Fatalf("the last migration is %q", got)
	}
}

// `Ab Ab` TWICE GIVES `ab-ab` THEN `ab-ab-2`.
func TestSlugCollisionGetsASuffix(t *testing.T) {
	s := open(t)
	a := mustAdd(t, s, docIn("Ab Ab", "a.md", "one"))
	b := mustAdd(t, s, docIn("Ab Ab", "b.md", "two"))
	c := mustAdd(t, s, docIn("Ab Ab", "c.md", "three"))
	if a.Slug != "ab-ab" || b.Slug != "ab-ab-2" || c.Slug != "ab-ab-3" {
		t.Fatalf("slugs %q %q %q", a.Slug, b.Slug, c.Slug)
	}
}

// A TOMBSTONED DOCUMENT KEEPS ITS SLUG, because its URL still answers.
func TestADeletedSlugIsNotReused(t *testing.T) {
	s := open(t)
	a := mustAdd(t, s, docIn("Report", "r.md", "one"))
	if err := s.DocDelete(a.Slug, "operator"); err != nil {
		t.Fatal(err)
	}
	if b := mustAdd(t, s, docIn("Report", "r.md", "two")); b.Slug != "report-2" {
		t.Fatalf("reused or odd slug %q", b.Slug)
	}
}

func TestSlugShapes(t *testing.T) {
	for title, want := range map[string]string{
		"Usage 2026-09-29":      "usage-2026-09-29",
		"  --Hello,  World!! ":  "hello-world",
		"Ünï":                   "n",
		"!!!":                   "",
		strings.Repeat("a", 90): strings.Repeat("a", 60),
	} {
		if got := DocSlugFor(title); got != want {
			t.Errorf("DocSlugFor(%q) = %q, want %q", title, got, want)
		}
	}
	// A title that leaves nothing gets doc-<sha8>.
	s := open(t)
	r := mustAdd(t, s, DocInput{Title: "", Name: "", Data: []byte("# hi"), Origin: "card", By: "r-1@sg4", Card: "sg4~1"})
	if !strings.HasPrefix(r.Slug, "doc-") || len(r.Slug) != 12 {
		t.Fatalf("fallback slug %q", r.Slug)
	}
	// A collision at the length limit still fits.
	long := strings.Repeat("a", 60)
	mustAdd(t, s, docIn(long, "x.md", "1"))
	r2 := mustAdd(t, s, docIn(long, "y.md", "2"))
	if len(r2.Slug) > 60 || !strings.HasSuffix(r2.Slug, "-2") {
		t.Fatalf("collision slug %q", r2.Slug)
	}
	// A document cannot take a name a route answers.
	if r := mustAdd(t, s, docIn("Settings", "s.md", "3")); r.Slug == "settings" {
		t.Fatal("a document took the settings route's name")
	}
}

// OLD VERSIONS KEEP THEIR BYTES AFTER A NEW ONE.
func TestOldVersionsKeepTheirBytes(t *testing.T) {
	s := open(t)
	r := mustAdd(t, s, docIn("Notes", "notes.md", "first draft"))
	in := docIn("", "notes.md", "second draft")
	in.Slug = r.Slug
	r2 := mustAdd(t, s, in)
	if r2.Version != 2 || r2.Slug != r.Slug {
		t.Fatalf("second write %+v", r2)
	}
	for n, want := range map[int]string{1: "first draft", 2: "second draft", 0: "second draft"} {
		f, v, err := s.DocOpen(r.Slug, n)
		if err != nil {
			t.Fatalf("v%d: %v", n, err)
		}
		var b bytes.Buffer
		b.ReadFrom(f)
		f.Close()
		if b.String() != want {
			t.Errorf("v%d holds %q, want %q (%+v)", n, b.String(), want, v)
		}
	}
	d, _ := s.DocGet(r.Slug)
	if len(d.Versions) != 2 || d.Versions[0].N != 1 || d.Versions[1].N != 2 {
		t.Fatalf("versions %+v", d.Versions)
	}
	if d.Title != "Notes" {
		t.Fatalf("a new version changed the title to %q", d.Title)
	}
}

// BYTES ARE FILES NAMED FOR THEIR HASH, never rows, and two versions of the same bytes are one file.
func TestBytesAreContentAddressedFiles(t *testing.T) {
	s := open(t)
	r := mustAdd(t, s, docIn("A", "a.md", "same bytes"))
	mustAdd(t, s, docIn("B", "b.md", "same bytes"))
	d, _ := s.DocGet(r.Slug)
	sha := d.Versions[0].SHA
	if len(sha) != 64 {
		t.Fatalf("sha %q", sha)
	}
	ents, _ := os.ReadDir(s.docsDir)
	if len(ents) != 1 || ents[0].Name() != sha {
		t.Fatalf("blob folder holds %v", ents)
	}
	if u, _, _ := s.DocUsage(); u.Bytes != int64(len("same bytes")) {
		t.Fatalf("usage counted the blob twice: %d", u.Bytes)
	}
}

// A VERSION WHOSE FILE IS MISSING SAYS SO AND DOES NOT FAIL THE PAGE.
func TestAMissingBlobIsSaidNotFatal(t *testing.T) {
	s := open(t)
	r := mustAdd(t, s, docIn("Gone", "g.md", "bytes"))
	d, _ := s.DocGet(r.Slug)
	if err := os.Remove(s.blobPath(d.Versions[0].SHA)); err != nil {
		t.Fatal(err)
	}
	d, err := s.DocGet(r.Slug)
	if err != nil || !d.Versions[0].Missing {
		t.Fatalf("detail %+v err %v", d, err)
	}
	if l, err := s.DocList("", false, ""); err != nil || len(l) != 1 || !l[0].Latest.Missing {
		t.Fatalf("list %+v err %v", l, err)
	}
	_, _, err = s.DocOpen(r.Slug, 1)
	if de := wantKind(t, err, DocGone); de.Msg != "the bytes are missing" {
		t.Fatalf("message %q", de.Msg)
	}
	if halted, _ := s.Halted(); halted {
		t.Fatal("the store halted over a missing blob")
	}
}

// A SIXTH MIB OF TEXT IS REFUSED. Exactly five is fine.
func TestTextOverTheCapIsRefused(t *testing.T) {
	s := open(t)
	five := strings.Repeat("a", 5<<20)
	mustAdd(t, s, docIn("Five", "five.txt", five))
	_, err := s.DocAdd(docIn("Six", "six.txt", five+"a"))
	wantKind(t, err, DocTooBig)
	// A file that is not text has the larger cap.
	img := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{1}, 6<<20)...)
	mustAdd(t, s, docIn("Pic", "p.png", string(img)))
	big := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{2}, 20<<20)...)
	_, err = s.DocAdd(docIn("Big", "big.png", string(big)))
	wantKind(t, err, DocTooBig)
}

// PER-CARD, 30 PUBLISHES AN HOUR. A board upload is not counted, and another card has its own.
func TestACardIsLimitedToThirtyAnHour(t *testing.T) {
	s := open(t)
	clock := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	old := now
	now = func() time.Time { return clock }
	defer func() { now = old }()

	pub := func(card, body string) error {
		_, err := s.DocAdd(DocInput{Title: "t " + body, Name: "t.md", Data: []byte(body), Origin: "card",
			By: "r@sg4", Card: card, Rate: true})
		return err
	}
	for i := 0; i < 30; i++ {
		if err := pub("sg4~a", string(rune('A'+i))+"x"); err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
	}
	wantKind(t, pub("sg4~a", "one too many"), DocRate)
	if err := pub("sg4~b", "other card"); err != nil {
		t.Fatalf("another card is limited too: %v", err)
	}
	mustAdd(t, s, docIn("Upload", "u.md", "board upload"))
	clock = clock.Add(61 * time.Minute)
	if err := pub("sg4~a", "next hour"); err != nil {
		t.Fatalf("an hour later: %v", err)
	}
}

// THE TOTAL CAP REFUSES WITH THE REASON, and nothing is evicted.
func TestTheStoreTotalRefuses(t *testing.T) {
	s := open(t)
	total := int64(100)
	if _, err := s.SetDocSettings(DocSettingsPatch{Caps: &struct {
		Text        *int64 `json:"text"`
		Other       *int64 `json:"other"`
		Total       *int64 `json:"total"`
		PerCardHour *int   `json:"per_card_hour"`
	}{Total: &total}}); err != nil {
		t.Fatal(err)
	}
	mustAdd(t, s, docIn("A", "a.txt", strings.Repeat("a", 60)))
	_, err := s.DocAdd(docIn("B", "b.txt", strings.Repeat("b", 60)))
	de := wantKind(t, err, DocFull)
	if !strings.Contains(de.Msg, "full") {
		t.Fatalf("no reason in %q", de.Msg)
	}
	if l, _ := s.DocList("", false, ""); len(l) != 1 {
		t.Fatalf("something was evicted or added: %d", len(l))
	}
	// The same bytes again cost nothing.
	mustAdd(t, s, docIn("A again", "a2.txt", strings.Repeat("a", 60)))
}

// PUBLISHING TURNED OFF IS A 503 WITH A SENTENCE.
func TestPublishingCanBeTurnedOff(t *testing.T) {
	s := open(t)
	off := false
	if _, err := s.SetDocSettings(DocSettingsPatch{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	_, err := s.DocAdd(docIn("A", "a.md", "x"))
	if de := wantKind(t, err, DocOff); !strings.Contains(de.Msg, "turned off") {
		t.Fatalf("reason %q", de.Msg)
	}
	set, _ := s.DocSettings()
	if set.Enabled || set.Caps.Text != DefaultDocTextCap {
		t.Fatalf("settings %+v", set)
	}
}

func TestCapsMustBePositive(t *testing.T) {
	s := open(t)
	zero := int64(0)
	_, err := s.SetDocSettings(DocSettingsPatch{Caps: &struct {
		Text        *int64 `json:"text"`
		Other       *int64 `json:"other"`
		Total       *int64 `json:"total"`
		PerCardHour *int   `json:"per_card_hour"`
	}{Text: &zero}})
	wantKind(t, err, DocBad)
}

// EVERY SECRET RULE, ON CONTENT, through the one door DocAdd is.
func TestSecretContentIsRefusedWithTheRuleNamed(t *testing.T) {
	cases := map[string]string{
		RulePEM:    "notes\n-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----\n",
		RuleGitHub: "token ghp_" + strings.Repeat("a1", 20),
		RuleAWS:    "key AKIAIOSFODNN7EXAMPLE here",
		RuleSlack:  "slack xoxb-123456789012-abcdefghij",
		RuleJWT:    "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U",
		RuleZrok:   "run: zrok enable ZcyXhMTX6HQe0",
	}
	cases[RuleGitHub+"-pat"] = "github_pat_" + strings.Repeat("A", 30)
	s := open(t)
	for name, body := range cases {
		want := strings.TrimSuffix(name, "-pat")
		_, err := s.DocAdd(docIn("S", "s.md", body))
		if de := wantKind(t, err, DocSecret); de.Rule != want {
			t.Errorf("%s: rule %q", name, de.Rule)
		}
	}
	// Plain prose, and a JWT-ish word with two segments, are not refused.
	for _, ok := range []string{"AKIA is a prefix", "eyJhbGciOi.only.two", "the zrok token is on the card", "ghp_short"} {
		if _, err := s.DocAdd(docIn("Fine "+ok, "f.md", ok)); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	// An override lets one through and records it.
	in := docIn("Overridden", "o.md", cases[RuleAWS])
	in.Override = true
	r := mustAdd(t, s, in)
	if d, _ := s.DocGet(r.Slug); !d.Versions[0].Override {
		t.Fatal("the override was not recorded")
	}
}

// A SECRET FILE NAME IS REFUSED, case-insensitively, on the path it was resolved to.
func TestSecretFileNames(t *testing.T) {
	for _, n := range []string{".env", ".ENV", ".env.local", "deploy/.Env.prod", "prod.env", "server.PEM", "a/b/tls.key",
		"id_rsa", "id_rsa.pub", "x.p12", "x.pfx", ".npmrc", ".NETRC", "credentials", "Credentials.json",
		".git/config", "sub/.git/hooks/x", ".ssh/known_hosts", "a/.SSH/id", "zrok-share", "room.json", "ca.key"} {
		if !SecretFileName(n) {
			t.Errorf("%q was allowed", n)
		}
	}
	for _, n := range []string{"notes.md", "environment.md", "docs/key-ideas.md", "keyboard.txt", "report.md", ".gitignore", "env.md", "git/readme.md"} {
		if SecretFileName(n) {
			t.Errorf("%q was refused", n)
		}
	}
}

func TestEmptyAndTitleRules(t *testing.T) {
	s := open(t)
	_, err := s.DocAdd(docIn("t", "t.md", ""))
	wantKind(t, err, DocBad)
	// A title of only control characters, on a file with no name either.
	_, err = s.DocAdd(DocInput{Title: "\r\n\t", Data: []byte("x"), Origin: "local", By: "operator", Name: "\x01"})
	wantKind(t, err, DocBad)
	r := mustAdd(t, s, docIn("A\r\nB\r\n", "t.md", "x"))
	if d, _ := s.DocGet(r.Slug); d.Title != "A B" {
		t.Fatalf("title %q", d.Title)
	}
	if err := s.DocRetitle(r.Slug, " \r\n "); err == nil {
		t.Fatal("an empty rename was accepted")
	}
	// The default title is the file name without its extension.
	if r := mustAdd(t, s, docIn("", "usage.v2.md", "y")); r.Slug != "usage-v2" {
		t.Fatalf("slug %q", r.Slug)
	}
	// A rename never changes the slug.
	s.DocRetitle("usage-v2", "Something Else")
	if d, _ := s.DocGet("usage-v2"); d.Title != "Something Else" {
		t.Fatalf("title %q", d.Title)
	}
}

func TestKinds(t *testing.T) {
	png := "\x89PNG\r\n\x1a\n" + "xxxx"
	for _, c := range []struct{ name, body, kind, mime string }{
		{"a.md", "# hi", "markdown", "text/markdown"},
		{"a.txt", "hi", "text", "text/plain"},
		{"a.diff", "+a\n-b\n", "diff", "text/x-diff"},
		{"a.patch", "+a\n", "diff", "text/x-diff"},
		{"a.png", png, "image", "image/png"},
		{"liar.md", png, "image", "image/png"},
		{"a.html", "<script>alert(1)</script>", "text", "text/plain"},
		{"a.svg", "<svg onload=alert(1)/>", "text", "text/plain"},
		{"a.bin", "\x00\x01\x02", "other", "application/octet-stream"},
		{"", "plain words", "markdown", "text/markdown"},
	} {
		k, m := DocKind(c.name, []byte(c.body))
		if k != c.kind || m != c.mime {
			t.Errorf("%q: %s %s, want %s %s", c.name, k, m, c.kind, c.mime)
		}
	}
}

// DELETE IS A TOMBSTONE, restore undoes it, and a purged document is restored by the operator only.
func TestTombstoneRestoreAndPurge(t *testing.T) {
	s := open(t)
	r := mustAdd(t, s, docIn("Doc", "d.md", "bytes one"))
	in := docIn("", "d.md", "bytes two")
	in.Slug = r.Slug
	mustAdd(t, s, in)

	if err := s.DocDelete(r.Slug, "operator"); err != nil {
		t.Fatal(err)
	}
	if l, _ := s.DocList("", false, ""); len(l) != 0 {
		t.Fatal("a tombstone is in the list")
	}
	gone, _ := s.DocList("", true, "")
	if len(gone) != 1 || gone[0].Deleted == nil || gone[0].Deleted.By != "operator" {
		t.Fatalf("restore list %+v", gone)
	}
	d, err := s.DocGet(r.Slug)
	if err != nil || d.Deleted == nil {
		t.Fatalf("a tombstone must answer: %+v %v", d, err)
	}
	_, _, err = s.DocOpen(r.Slug, 0)
	wantKind(t, err, DocGone)
	if err := s.DocRestore(r.Slug, false); err != nil {
		t.Fatal(err)
	}
	if l, _ := s.DocList("", false, ""); len(l) != 1 {
		t.Fatal("not restored")
	}

	// Purge version 1: its bytes go, the rows stay, version 2 is untouched.
	if _, err := s.DocPurge(r.Slug, 1); err != nil {
		t.Fatal(err)
	}
	d, _ = s.DocGet(r.Slug)
	if !d.Versions[0].Purged || d.Versions[1].Purged {
		t.Fatalf("purge flags %+v", d.Versions)
	}
	if _, err := os.Stat(s.blobPath(d.Versions[0].SHA)); err == nil {
		t.Fatal("the purged blob is still on disk")
	}
	_, _, err = s.DocOpen(r.Slug, 1)
	wantKind(t, err, DocGone)
	f, _, err := s.DocOpen(r.Slug, 2)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	_, _, err = s.DocOpen(r.Slug, 9)
	wantKind(t, err, DocNone)
	_, err = s.DocPurge(r.Slug, 9)
	wantKind(t, err, DocNone)

	s.DocDelete(r.Slug, "operator")
	wantKind(t, s.DocRestore(r.Slug, false), DocForbidden)
	if err := s.DocRestore(r.Slug, true); err != nil {
		t.Fatalf("the operator could not restore: %v", err)
	}
	// Purging everything.
	if _, err := s.DocPurge(r.Slug, 0); err != nil {
		t.Fatal(err)
	}
	if u, _, _ := s.DocUsage(); u.Bytes != 0 || u.Versions != 2 {
		t.Fatalf("usage after purge %+v", u)
	}
}

func TestUnknownDocumentsAreRefusedNotFatal(t *testing.T) {
	s := open(t)
	_, err := s.DocGet("nope")
	wantKind(t, err, DocNone)
	wantKind(t, s.DocDelete("nope", "x"), DocNone)
	wantKind(t, s.DocRetitle("nope", "T"), DocNone)
	in := docIn("", "a.md", "x")
	in.Slug = "nope"
	_, err = s.DocAdd(in)
	wantKind(t, err, DocNone)
	if halted, _ := s.Halted(); halted {
		t.Fatal("a refusal halted the store")
	}
}

func TestListFiltersAndOrder(t *testing.T) {
	s := open(t)
	clock := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	old := now
	now = func() time.Time { clock = clock.Add(time.Second); return clock }
	defer func() { now = old }()

	mustAdd(t, s, docIn("Usage report", "u.md", "1"))
	mustAdd(t, s, DocInput{Title: "Eval", Name: "e.md", Data: []byte("2"), Origin: "card", By: "r-9@sg4", Card: "sg4~9"})
	mustAdd(t, s, docIn("usage again", "u2.md", "3"))
	l, _ := s.DocList("", false, "")
	if len(l) != 3 || l[0].Title != "usage again" {
		t.Fatalf("order %+v", l)
	}
	if l, _ := s.DocList("USAGE", false, ""); len(l) != 2 {
		t.Fatalf("filter gave %d", len(l))
	}
	l, _ = s.DocList("", false, "sg4~9")
	if len(l) != 1 || l[0].Slug != "eval" || l[0].Latest.Origin != "card" || l[0].Latest.Card != "sg4~9" {
		t.Fatalf("card filter %+v", l)
	}
	// A new version moves a document to the top and counts.
	in := docIn("", "u.md", "4")
	in.Slug = "usage-report"
	mustAdd(t, s, in)
	l, _ = s.DocList("", false, "")
	if l[0].Slug != "usage-report" || l[0].Versions != 2 || l[0].Updated != l[0].Latest.At {
		t.Fatalf("after a new version %+v", l[0])
	}
}

func TestLargestAndUsage(t *testing.T) {
	s := open(t)
	mustAdd(t, s, docIn("Small", "s.md", "ab"))
	mustAdd(t, s, docIn("Large", "l.md", strings.Repeat("x", 500)))
	u, large, err := s.DocUsage()
	if err != nil || u.Docs != 2 || u.Versions != 2 || u.Bytes != 502 {
		t.Fatalf("usage %+v %v", u, err)
	}
	if len(large) != 2 || large[0].Slug != "large" || large[0].Bytes != 500 {
		t.Fatalf("largest %+v", large)
	}
}

// THE DAILY COPY mirrors the blobs, once a day, and a purge reaches the copy.
func TestTheDailyCopyOfTheBlobs(t *testing.T) {
	s := open(t)
	r := mustAdd(t, s, docIn("Keep", "k.md", "keep me"))
	r2 := mustAdd(t, s, docIn("Drop", "d.md", "drop me"))
	d, _ := s.DocGet(r.Slug)
	dir := filepath.Join(t.TempDir(), "backups")

	n, err := s.CopyDocs(dir)
	if err != nil || n != 2 {
		t.Fatalf("copied %d: %v", n, err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "docs", d.Versions[0].SHA)); err != nil || string(b) != "keep me" {
		t.Fatalf("copy %q %v", b, err)
	}
	// Called again at once: a day has not passed.
	if n, _ := s.CopyDocs(dir); n != 0 {
		t.Fatalf("copied again after a minute: %d", n)
	}
	// The snapshots still list, with a folder and a stamp beside them.
	if _, err := Backups(dir); err != nil {
		t.Fatal(err)
	}
	// A day later: a purged blob leaves the copy, and a blob that merely vanished from the live
	// folder does NOT, which is what the copy is for.
	dd, _ := s.DocGet(r2.Slug)
	s.DocPurge(r2.Slug, 0)
	os.Remove(s.blobPath(d.Versions[0].SHA))
	old := time.Now().Add(-25 * time.Hour)
	os.Chtimes(filepath.Join(dir, docsStamp), old, old)
	if _, err := s.CopyDocs(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "docs", dd.Versions[0].SHA)); err == nil {
		t.Fatal("a purged blob is still in the backup")
	}
	if _, err := os.Stat(filepath.Join(dir, "docs", d.Versions[0].SHA)); err != nil {
		t.Fatal("a vanished live blob was removed from the backup")
	}
}

// A WRITE THAT FAILS LEAVES NO ROW.
func TestARefusedWriteLeavesNothing(t *testing.T) {
	s := open(t)
	_, err := s.DocAdd(docIn("Secret", "s.md", "AKIAIOSFODNN7EXAMPLE"))
	wantKind(t, err, DocSecret)
	var n int
	if err := s.guard(func() error { return s.db.QueryRow(`SELECT COUNT(*) FROM doc`).Scan(&n) }); err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d rows", n)
	}
	if _, err := os.Stat(s.docsDir); err == nil {
		ents, _ := os.ReadDir(s.docsDir)
		if len(ents) != 0 {
			t.Fatalf("blobs %v", ents)
		}
	}
}
