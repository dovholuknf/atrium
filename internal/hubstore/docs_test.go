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
	type c struct{ rule, body string }
	cases := []c{
		{RulePEM, "notes\n-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----\n"},
		{RuleGitHub, "token ghp_" + strings.Repeat("a1", 20)},
		{RuleGitHub, "github_pat_" + strings.Repeat("A", 30)},
		{RuleAWS, "key AKIAIOSFODNN7EXAMPLE here"},
		{RuleSlack, "slack xoxb-123456789012-abcdefghij"},
		{RuleJWT, "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"},
		{RuleZrok, "run: zrok enable ZcyXhMTX6HQe0"},
	}
	// Every spelling of the zrok hint, since the pattern is case-insensitive.
	for _, b := range []string{"ZROK ENABLE abcDEF123xyz", "Zrok Enable abcDEF123xyz", "ZROK_TOKEN=abcDEF123xyz",
		"zRoK_account_token: abcDEF123xyz"} {
		cases = append(cases, c{RuleZrok, b})
	}
	// Every Slack prefix the tokens come in.
	for _, k := range "abeoprs" {
		cases = append(cases, c{RuleSlack, "xox" + string(k) + "-123456789012-abcdefghij"})
	}
	s := open(t)
	for i, cs := range cases {
		_, err := s.DocAdd(docIn("S"+string(rune('a'+i)), "s.md", cs.body))
		if de := wantKind(t, err, DocSecret); de.Rule != cs.rule {
			t.Errorf("%q: rule %q, want %s", cs.body, de.Rule, cs.rule)
		}
	}
	// Plain prose, and a JWT-ish word with two segments, are not refused.
	for _, ok := range []string{"AKIA is a prefix", "eyJhbGciOi.only.two", "the zrok token is on the card", "ghp_short"} {
		if _, err := s.DocAdd(docIn("Fine "+ok, "f.md", ok)); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	// An override lets one through and records it.
	in := docIn("Overridden", "o.md", "key AKIAIOSFODNN7EXAMPLE here")
	in.Override = true
	r := mustAdd(t, s, in)
	if d, _ := s.DocGet(r.Slug); !d.Versions[0].Override {
		t.Fatal("the override was not recorded")
	}
}

// A SECRET FILE NAME IS REFUSED, case-insensitively, on the path it was resolved to.
func TestSecretFileNames(t *testing.T) {
	for _, n := range []string{".env", ".ENV", ".env.local", "deploy/.Env.prod", "prod.env", "server.PEM", "a/b/tls.key",
		"id_rsa", "id_rsa.pub", "id_dsa", "ID_DSA.pub", ".htpasswd", ".HTPASSWD", "vault.kdbx", "Pass.KDBX",
		".zrok/environment.json", "home/.zrok2/x", ".aws/credentials", ".AWS/config", ".kube/config", "a/.gnupg/pubring.kbx",
		".docker/config.json", "x.p12", "x.pfx", ".npmrc", ".NETRC", "credentials", "Credentials.json",
		".git/config", "sub/.git/hooks/x", ".ssh/known_hosts", "a/.SSH/id", "zrok-share", "room.json", "ca.key"} {
		if !SecretFileName(n) {
			t.Errorf("%q was allowed", n)
		}
	}
	for _, n := range []string{"notes.md", "environment.md", "docs/key-ideas.md", "keyboard.txt", "awsome.md", "zrok-notes.md", "docker.md", "report.md", ".gitignore", "env.md", "git/readme.md"} {
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

// A MULTIBYTE CHARACTER ACROSS BYTE 8192 DOES NOT MAKE MARKDOWN "other". The check reads the first
// 8 KiB, and a cut through a character used to be judged invalid because the back-up loop compared
// against the whole document. Both probes from the review: 8191 ASCII plus é, and 8190.
func TestTextWithAMultibyteCharacterAtTheBoundaryIsStillText(t *testing.T) {
	tail := strings.Repeat("z", 3000)
	for _, c := range []struct {
		name, ch string
		pad      int
	}{
		{"2-byte at 8191", "é", 8191},
		{"2-byte at 8190", "é", 8190},
		{"2-byte at 8192", "é", 8192},
		{"3-byte at 8190", "€", 8190},
		{"3-byte at 8191", "€", 8191},
		{"4-byte at 8189", "😀", 8189},
		{"4-byte at 8190", "😀", 8190},
		{"4-byte at 8191", "😀", 8191},
	} {
		body := strings.Repeat("a", c.pad) + c.ch + tail
		if k, m := DocKind("report.md", []byte(body)); k != "markdown" || m != "text/markdown" {
			t.Errorf("%s: %s %s, want markdown", c.name, k, m)
		}
		if k, _ := DocKind("report.txt", []byte(body)); k != "text" {
			t.Errorf("%s as .txt: %s", c.name, k)
		}
	}
	// A document with a NUL in the part read is still other, and so is one with invalid bytes there.
	if k, _ := DocKind("x.md", []byte("ab\x00cd")); k != "other" {
		t.Errorf("NUL: %s", k)
	}
	if k, _ := DocKind("x.md", append([]byte("ok "), 0xff, 0xfe, 'z')); k != "other" {
		t.Errorf("invalid UTF-8: %s", k)
	}
}

// A NEW VERSION OF A TOMBSTONED DOCUMENT IS REFUSED, 410, and nothing is written.
func TestAVersionOfATombstonedDocumentIsGone(t *testing.T) {
	s := open(t)
	r := mustAdd(t, s, docIn("Doc", "d.md", "one"))
	if err := s.DocDelete(r.Slug, "operator"); err != nil {
		t.Fatal(err)
	}
	in := docIn("", "d.md", "two")
	in.Slug = r.Slug
	_, err := s.DocAdd(in)
	wantKind(t, err, DocGone)
	if d, _ := s.DocGet(r.Slug); len(d.Versions) != 1 {
		t.Fatalf("a version was added to a tombstone: %+v", d.Versions)
	}
	s.DocRestore(r.Slug, true)
	mustAdd(t, s, in)
}

// A PURGE RACING A WRITE OF THE SAME BYTES CANNOT LEAVE THE NEW VERSION MISSING. DocAdd and DocPurge
// share one lock, so the file is never removed under a version that just referenced it. The hook
// holds the purge in the gap after its commit, which is where a write of the same bytes used to land:
// it saw the file, skipped writing it, and the purge then removed it.
func TestPurgeRacingAnAddOfTheSameBytesKeepsTheNewVersion(t *testing.T) {
	s := open(t)
	first := mustAdd(t, s, docIn("Racer", "r.md", "the shared bytes"))
	var added DocResult
	var addErr error
	addDone := make(chan struct{})
	afterPurgeCommit = func() {
		go func() {
			defer close(addDone)
			added, addErr = s.DocAdd(docIn("Racer copy", "r.md", "the shared bytes"))
		}()
		// Long enough for an unserialised write to finish inside the gap.
		time.Sleep(300 * time.Millisecond)
	}
	defer func() { afterPurgeCommit = nil }()
	if _, err := s.DocPurge(first.Slug, 0); err != nil {
		t.Fatal(err)
	}
	<-addDone
	if addErr != nil {
		t.Fatal(addErr)
	}
	d, _ := s.DocGet(added.Slug)
	v := d.Versions[0]
	if v.Purged || v.Missing {
		t.Fatalf("the new version lost its bytes: %+v", v)
	}
	f, _, err := s.DocOpen(added.Slug, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
}

// A PURGE THAT COULD NOT REMOVE ITS FILE IS RETRIED BY THE NEXT COPY PASS, which is the Windows case of
// a reader holding the blob open. No sweeper beyond that.
func TestAStuckPurgedFileIsRemovedOnTheNextCopyPass(t *testing.T) {
	s := open(t)
	r := mustAdd(t, s, docIn("Stuck", "s.md", "held open"))
	d, _ := s.DocGet(r.Slug)
	sha := d.Versions[0].SHA
	res, err := s.DocPurge(r.Slug, 0)
	if err != nil || res.Blobs != 1 {
		t.Fatalf("purge %+v %v", res, err)
	}
	// The file the purge could not remove.
	if err := os.WriteFile(s.blobPath(sha), []byte("held open"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "backups")
	// A stamp from a minute ago: the daily copy is not due, the retry still runs.
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, docsStamp), []byte("x"), 0o600)
	if _, err := s.CopyDocs(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.blobPath(sha)); err == nil {
		t.Fatal("the purged file is still in the live folder")
	}
	// A file that an unpurged version still uses is never swept.
	live := mustAdd(t, s, docIn("Live", "l.md", "still wanted"))
	ld, _ := s.DocGet(live.Slug)
	s.CopyDocs(dir)
	if _, err := os.Stat(s.blobPath(ld.Versions[0].SHA)); err != nil {
		t.Fatal("a live blob was swept")
	}
}

// A PURGE NAMES THE OTHER DOCUMENTS A SHARED SHA REACHED, for the audit line, and not the ones it was
// pointed at.
func TestAPurgeNamesWhatTheSharedBytesReached(t *testing.T) {
	s := open(t)
	a := mustAdd(t, s, docIn("Alpha", "a.md", "identical bytes"))
	b := mustAdd(t, s, docIn("Bravo", "b.md", "identical bytes"))
	in := docIn("", "a.md", "different bytes")
	in.Slug = a.Slug
	mustAdd(t, s, in)
	res, err := s.DocPurge(a.Slug, 1)
	if err != nil {
		t.Fatal(err)
	}
	if res.Blobs != 1 || len(res.Also) != 1 || res.Also[0] != b.Slug+"@1" {
		t.Fatalf("%+v", res)
	}
	// Bravo is purged with it, and Alpha's version 2 is not.
	if d, _ := s.DocGet(b.Slug); !d.Versions[0].Purged {
		t.Fatal("bravo kept bytes that were deleted")
	}
	if d, _ := s.DocGet(a.Slug); d.Versions[1].Purged {
		t.Fatal("version 2 was purged")
	}
	// Purging the whole of a document names only the others.
	c := mustAdd(t, s, docIn("Charlie", "c.md", "third"))
	d2 := mustAdd(t, s, docIn("Delta", "d.md", "third"))
	res, _ = s.DocPurge(c.Slug, 0)
	if len(res.Also) != 1 || res.Also[0] != d2.Slug+"@1" {
		t.Fatalf("%+v", res)
	}
	if res, _ := s.DocPurge(c.Slug, 0); len(res.Also) != 0 {
		t.Fatalf("purging again reached %v", res.Also)
	}
}

// purged AND override ARE 0 OR 1, BY CHECK.
func TestTheFlagsAreZeroOrOne(t *testing.T) {
	s := open(t)
	r := mustAdd(t, s, docIn("Flags", "f.md", "x"))
	for _, col := range []string{"purged", "override"} {
		_, err := s.db.Exec(`UPDATE doc_version SET `+col+` = 2 WHERE doc = ?`, r.Slug)
		if err == nil {
			t.Errorf("%s = 2 was accepted", col)
		}
	}
}
