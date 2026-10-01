package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

type changesFix struct {
	t    *testing.T
	d    *Daemon
	ts   *httptest.Server
	repo string
	task *store.Task
	tr   string // the transcript
}

var changesBase = time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)

// gitT runs git in dir with an identity and, when date is not zero, that author and committer date.
func gitT(t *testing.T, dir string, date time.Time, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	env := []string{}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(kv), "GIT_") {
			env = append(env, kv)
		}
	}
	env = append(env, "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if !date.IsZero() {
		d := date.Format(time.RFC3339)
		env = append(env, "GIT_AUTHOR_DATE="+d, "GIT_COMMITTER_DATE="+d)
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeF(t *testing.T, path, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newChangesFix is a card on a real repository with one commit (a.txt, b.txt, c.txt), on the branch
// claude/main.
func newChangesFix(t *testing.T) *changesFix {
	t.Helper()
	d := testDaemon(t)
	repo := t.TempDir()
	gitT(t, repo, time.Time{}, "init", "-b", "claude/main")
	for _, n := range []string{"a.txt", "b.txt", "c.txt"} {
		writeF(t, filepath.Join(repo, n), "one\ntwo\n")
	}
	gitT(t, repo, changesBase.Add(-time.Hour), "add", ".")
	gitT(t, repo, changesBase.Add(-time.Hour), "commit", "-m", "first")
	task, _, err := d.st.Register(store.Observed{WireName: "changer", Worktree: filepath.ToSlash(repo), Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetResumeID(task.ID, "s1"); err != nil {
		t.Fatal(err)
	}
	f := &changesFix{t: t, d: d, repo: repo, tr: filepath.Join(t.TempDir(), "s1.jsonl")}
	f.task, _ = d.st.Get(task.ID)
	d.usage.transcript = func(cwd, id string) string {
		if _, err := os.Stat(f.tr); err != nil || id != "s1" {
			return ""
		}
		return f.tr
	}
	f.ts = httptest.NewServer(d.ap.Handler())
	t.Cleanup(f.ts.Close)
	return f
}

func (f *changesFix) get(query string) (int, map[string]any) {
	f.t.Helper()
	res, err := http.Get(f.ts.URL + "/v1/tasks/" + f.task.ID + "/changes" + query)
	if err != nil {
		f.t.Fatal(err)
	}
	defer res.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(res.Body).Decode(&m)
	return res.StatusCode, m
}

func (f *changesFix) ok(query string) *ChangesView {
	f.t.Helper()
	res, err := http.Get(f.ts.URL + "/v1/tasks/" + f.task.ID + "/changes" + query)
	if err != nil {
		f.t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		f.t.Fatalf("changes%s answered %d", query, res.StatusCode)
	}
	var v ChangesView
	if err := json.NewDecoder(res.Body).Decode(&v); err != nil {
		f.t.Fatal(err)
	}
	return &v
}

func fileOf(v *ChangesView, path string) *ChangeFile {
	for i := range v.Files {
		if v.Files[i].Path == path {
			return &v.Files[i]
		}
	}
	return nil
}

func (f *changesFix) prompt(at time.Time, text string) {
	f.t.Helper()
	b, _ := json.Marshal(map[string]any{"type": "user", "timestamp": at.Format(time.RFC3339Nano),
		"message": map[string]any{"content": text}})
	fh, err := os.OpenFile(f.tr, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		f.t.Fatal(err)
	}
	defer fh.Close()
	fmt.Fprintln(fh, string(b))
}

func toolUse(name, path string) map[string]any {
	return map[string]any{"type": "tool_use", "name": name, "input": map[string]any{"file_path": path}}
}

// turn writes one turn: a prompt, an assistant message calling the given edits, and a text reply.
// It returns the reply's time.
func (f *changesFix) turn(start time.Time, id string, edits ...map[string]any) time.Time {
	f.prompt(start, "do "+id)
	if len(edits) > 0 {
		transcriptReply(f.t, f.tr, id+"-tools", start.Add(time.Second), false, edits...)
	}
	at := start.Add(2 * time.Second)
	transcriptReply(f.t, f.tr, id+"-text", at, false, textBlock("done "+id))
	return at
}

func TestChangesHeadShowsUncommittedAndUntracked(t *testing.T) {
	f := newChangesFix(t)
	// A GIT_DIR in the room's own environment must not change what is diffed.
	other := t.TempDir()
	gitT(t, other, time.Time{}, "init")
	t.Setenv("GIT_DIR", filepath.Join(other, ".git"))

	writeF(t, filepath.Join(f.repo, "a.txt"), "one\nTWO\nthree\n")
	writeF(t, filepath.Join(f.repo, "new.txt"), "x\ny\n")
	writeF(t, filepath.Join(f.repo, "dir", "deep.txt"), "z\n")
	writeF(t, filepath.Join(f.repo, ".gitignore"), "ignored.txt\n")
	writeF(t, filepath.Join(f.repo, "ignored.txt"), "no\n")
	if err := os.WriteFile(filepath.Join(f.repo, "bin.dat"), []byte{1, 2, 0, 3}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(f.repo, "c.txt")); err != nil {
		t.Fatal(err)
	}
	v := f.ok("")
	if v.Against != "head" || !v.Dirty || v.Head == "" || v.Total != len(v.Files) {
		t.Fatalf("got %+v", v)
	}
	if a := fileOf(v, "a.txt"); a == nil || a.Status != "modified" || a.Added != 2 || a.Removed != 1 || !strings.Contains(a.Hunks, "+TWO") {
		t.Fatalf("a.txt: %+v", a)
	}
	if n := fileOf(v, "new.txt"); n == nil || n.Status != "added" || n.Added != 2 || !strings.Contains(n.Hunks, "+x\n+y\n") {
		t.Fatalf("untracked file is not a whole-file addition: %+v", n)
	}
	if fileOf(v, "dir/deep.txt") == nil {
		t.Fatalf("a file in an untracked directory is missing: %+v", v.Files)
	}
	if fileOf(v, "ignored.txt") != nil {
		t.Fatal("an ignored file is listed")
	}
	if b := fileOf(v, "bin.dat"); b == nil || b.Status != "binary" || b.Hunks != "" {
		t.Fatalf("a binary file is listed with content: %+v", b)
	}
	if c := fileOf(v, "c.txt"); c == nil || c.Status != "deleted" || c.Removed != 2 {
		t.Fatalf("c.txt: %+v", c)
	}
	// head is the default, so asking for it is the same answer.
	if w := f.ok("?against=head"); w.Total != v.Total {
		t.Fatalf("against=head differs from the default: %d and %d", w.Total, v.Total)
	}
}

func TestChangesBaseIsTheBranchSinceTheMergeBase(t *testing.T) {
	f := newChangesFix(t)
	gitT(t, f.repo, time.Time{}, "switch", "-c", "feature")
	writeF(t, filepath.Join(f.repo, "b.txt"), "one\ntwo\nbranch\n")
	gitT(t, f.repo, changesBase, "commit", "-am", "on the branch")
	writeF(t, filepath.Join(f.repo, "a.txt"), "uncommitted\n")

	head := f.ok("")
	if fileOf(head, "b.txt") != nil || fileOf(head, "a.txt") == nil {
		t.Fatalf("head should show only the uncommitted file: %+v", head.Files)
	}
	base := f.ok("?against=base")
	if base.Against != "base" || fileOf(base, "b.txt") == nil || fileOf(base, "a.txt") == nil {
		t.Fatalf("base should show the branch and the uncommitted file: %+v", base)
	}
	if base.Base == "" || base.Base == base.Head {
		t.Fatalf("base %q head %q", base.Base, base.Head)
	}

	// No claude/main or main: base falls back to head and says so.
	gitT(t, f.repo, time.Time{}, "branch", "-m", "claude/main", "trunk")
	fb := f.ok("?against=base")
	if fb.Against != "head" || fb.Note == "" || fileOf(fb, "b.txt") != nil {
		t.Fatalf("no integration branch: %+v", fb)
	}
}

func TestChangesRefusals(t *testing.T) {
	f := newChangesFix(t)
	if code, m := f.get("?path=a.txt"); code != 400 {
		t.Fatalf("a path parameter answered %d %v", code, m)
	}
	for _, q := range []string{"?file=a", "?dir=/", "?paths=a", "?cwd=/"} {
		if code, _ := f.get(q); code != 400 {
			t.Fatalf("%s answered %d", q, code)
		}
	}
	at := url.QueryEscape(changesBase.Format(time.RFC3339Nano))
	if code, _ := f.get("?against=head&turn=" + at); code != 400 {
		t.Fatalf("against and turn together answered %d", code)
	}
	if code, _ := f.get("?turn=yesterday"); code != 400 {
		t.Fatalf("a turn that is not RFC3339 answered %d", code)
	}
	if code, _ := f.get("?against=sideways"); code != 400 {
		t.Fatalf("an unknown against answered %d", code)
	}
	f.turn(changesBase, "t1")
	code, m := f.get("?turn=" + url.QueryEscape(changesBase.Add(42*time.Minute).Format(time.RFC3339Nano)))
	if code != 404 || !strings.Contains(fmt.Sprint(m["error"]), "no reply") {
		t.Fatalf("an unknown turn answered %d %v", code, m)
	}

	// A directory that is not a worktree.
	plain := t.TempDir()
	task, _, err := f.d.st.Register(store.Observed{WireName: "plain", Worktree: filepath.ToSlash(plain), Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.Get(f.ts.URL + "/v1/tasks/" + task.ID + "/changes")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body map[string]string
	_ = json.NewDecoder(res.Body).Decode(&body)
	if res.StatusCode != 404 || body["error"] != "this card's directory is not a git worktree" {
		t.Fatalf("not a worktree answered %d %v", res.StatusCode, body)
	}
}

func TestChangesBoundsCutTheListAndSayWhat(t *testing.T) {
	f := newChangesFix(t)
	for i := 0; i < changesFilesMax+5; i++ {
		writeF(t, filepath.Join(f.repo, "many", fmt.Sprintf("f%04d.txt", i)), "x\n")
	}
	v := f.ok("")
	if len(v.Files) != changesFilesMax || v.Total != changesFilesMax+5 || v.Cut == nil || v.Cut.Files != 5 || v.Cut.Why == "" {
		t.Fatalf("files: len %d total %d cut %+v", len(v.Files), v.Total, v.Cut)
	}

	f2 := newChangesFix(t)
	big := strings.Repeat("0123456789abcdef\n", (changesFileMax/17)+100)
	writeF(t, filepath.Join(f2.repo, "big.txt"), big)
	writeF(t, filepath.Join(f2.repo, "small.txt"), "s\n")
	w := f2.ok("")
	b := fileOf(w, "big.txt")
	if b == nil || !b.HunksCut || b.Hunks != "" || b.Added == 0 || w.Cut == nil || w.Cut.Hunks != 1 {
		t.Fatalf("a file over the bound keeps its counts only: %+v cut %+v", b, w.Cut)
	}
	if s := fileOf(w, "small.txt"); s == nil || s.Hunks == "" {
		t.Fatalf("a small file lost its hunks: %+v", s)
	}

	// Past 2 MB in all, later files show counts only.
	f3 := newChangesFix(t)
	chunk := strings.Repeat("0123456789abcdef\n", (changesFileMax/17)-100)
	for i := 0; i < 12; i++ {
		writeF(t, filepath.Join(f3.repo, fmt.Sprintf("m%02d.txt", i)), chunk)
	}
	x := f3.ok("")
	total, cutCount := 0, 0
	for _, fl := range x.Files {
		total += len(fl.Hunks)
		if fl.HunksCut {
			cutCount++
		}
	}
	if total > changesHunksMax || cutCount == 0 || x.Cut == nil {
		t.Fatalf("hunks %d bytes, %d cut, cut %+v", total, cutCount, x.Cut)
	}
}

func TestChangesTurnListsOnlyWhatTheTurnsEditsNamed(t *testing.T) {
	f := newChangesFix(t)
	outside := filepath.Join(t.TempDir(), "secret-name.txt")
	writeF(t, outside, "x\n")
	a, n := filepath.Join(f.repo, "a.txt"), filepath.Join(f.repo, "fresh.txt")
	writeF(t, a, "one\nchanged\n")
	writeF(t, n, "brand\nnew\n")
	writeF(t, filepath.Join(f.repo, "b.txt"), "touched by a shell\n")
	at := f.turn(changesBase, "t1", toolUse("Edit", a), toolUse("Write", n), toolUse("Write", outside),
		toolUse("Edit", filepath.Join(f.repo, "..", "sneaky.txt")), toolUse("Edit", "relative.txt"),
		map[string]any{"type": "tool_use", "name": "Bash", "input": map[string]any{"command": "echo hi"}})

	v := f.ok("?turn=" + url.QueryEscape(at.Format(time.RFC3339Nano)))
	if v.Against != "turn" || v.Partial == nil || !*v.Partial || !strings.Contains(v.Why, "shell command") {
		t.Fatalf("a turn answer is partial and says why: %+v", v)
	}
	if v.Head == "" || v.Base == "" || !v.Dirty {
		t.Fatalf("base, head and dirty are the card-level ones: %+v", v)
	}
	if len(v.Files) != 2 || fileOf(v, "a.txt") == nil || fileOf(v, "fresh.txt") == nil || fileOf(v, "b.txt") != nil {
		t.Fatalf("the turn lists the paths its edits named and no others: %+v", v.Files)
	}
	if v.Outside != 3 {
		t.Fatalf("outside = %d, want 3 (an absolute outside path, a .. path, a relative one)", v.Outside)
	}
	raw, _ := json.Marshal(v)
	if strings.Contains(string(raw), "secret-name") || strings.Contains(string(raw), "sneaky") {
		t.Fatalf("an outside path is named in the answer: %s", raw)
	}
	for _, fl := range v.Files {
		if fl.Via != "edits" || fl.Cumulative {
			t.Fatalf("%+v", fl)
		}
	}
	if fresh := fileOf(v, "fresh.txt"); fresh.Added != 2 || fresh.Status != "added" {
		t.Fatalf("fresh.txt: %+v", fresh)
	}

	// /replies carries the cheap count and no git runs for it.
	d := f.d
	rv, err := d.repliesFor(f.task.ID, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(rv.Replies) != 1 || rv.Replies[0].Edited != 2 {
		t.Fatalf("edited: %+v", rv.Replies)
	}
}

func TestChangesTurnWithNoEditCallsIsEmptyAndPartial(t *testing.T) {
	f := newChangesFix(t)
	writeF(t, filepath.Join(f.repo, "a.txt"), "dirty by a shell\n")
	at := f.turn(changesBase, "t1")
	v := f.ok("?turn=" + url.QueryEscape(at.Format(time.RFC3339Nano)))
	if len(v.Files) != 0 || v.Partial == nil || !*v.Partial {
		t.Fatalf("%+v", v)
	}
	if rv, _ := f.d.repliesFor(f.task.ID, 5); rv.Replies[0].Edited != 0 {
		t.Fatalf("edited %d", rv.Replies[0].Edited)
	}
}

func TestChangesTurnFindsWhatTheTurnCommitted(t *testing.T) {
	f := newChangesFix(t)
	c := filepath.Join(f.repo, "c.txt")
	writeF(t, c, "one\ntwo\nthree\nfour\n")
	at := f.turn(changesBase, "t1", toolUse("Edit", c))
	gitT(t, f.repo, changesBase.Add(10*time.Second), "commit", "-am", "the turn commits")
	// Another turn, later, with its own commit that does not touch c.txt.
	writeF(t, filepath.Join(f.repo, "b.txt"), "later\n")
	f.turn(changesBase.Add(time.Hour), "t2", toolUse("Edit", filepath.Join(f.repo, "b.txt")))
	gitT(t, f.repo, changesBase.Add(time.Hour+10*time.Second), "commit", "-am", "later")

	v := f.ok("?turn=" + url.QueryEscape(at.Format(time.RFC3339Nano)))
	got := fileOf(v, "c.txt")
	if len(v.Files) != 1 || got == nil || got.Via != "commits" || got.Added != 2 || got.Removed != 0 || !strings.Contains(got.Hunks, "+four") {
		t.Fatalf("via commits: %+v", v.Files)
	}
	if v.Dirty || strings.Contains(v.Why, "rewritten") {
		t.Fatalf("%+v", v)
	}
}

func TestChangesTurnSaysWhenCommitDatesWereRewritten(t *testing.T) {
	f := newChangesFix(t)
	c := filepath.Join(f.repo, "c.txt")
	writeF(t, c, "one\ntwo\nthree\n")
	at := f.turn(changesBase, "t1", toolUse("Edit", c))
	// Authored a day before, committed inside the window: what a rebase leaves.
	cmd := exec.Command("git", "commit", "-am", "rebased")
	cmd.Dir = f.repo
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_AUTHOR_DATE="+changesBase.Add(-24*time.Hour).Format(time.RFC3339),
		"GIT_COMMITTER_DATE="+changesBase.Add(10*time.Second).Format(time.RFC3339))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	v := f.ok("?turn=" + url.QueryEscape(at.Format(time.RFC3339Nano)))
	if v.Partial == nil || !*v.Partial || !strings.Contains(v.Why, "dates were rewritten") {
		t.Fatalf("%+v", v)
	}
}

func TestChangesTurnMarksAFileEditedInAnotherTurnCumulative(t *testing.T) {
	f := newChangesFix(t)
	a, b := filepath.Join(f.repo, "a.txt"), filepath.Join(f.repo, "b.txt")
	writeF(t, a, "one\nfirst\n")
	first := f.turn(changesBase, "t1", toolUse("Edit", a), toolUse("Edit", b))
	writeF(t, a, "one\nfirst\nsecond\n")
	writeF(t, b, "one\nb\n")
	second := f.turn(changesBase.Add(time.Hour), "t2", toolUse("Edit", a))
	// The last commit is before both turns, so a.txt in turn 1 carries turn 2's lines too.
	v := f.ok("?turn=" + url.QueryEscape(first.Format(time.RFC3339Nano)))
	if x := fileOf(v, "a.txt"); x == nil || !x.Cumulative {
		t.Fatalf("a.txt was edited in another turn since the last commit: %+v", v.Files)
	}
	if y := fileOf(v, "b.txt"); y == nil || y.Cumulative {
		t.Fatalf("b.txt was edited in one turn only: %+v", v.Files)
	}
	w := f.ok("?turn=" + url.QueryEscape(second.Format(time.RFC3339Nano)))
	if x := fileOf(w, "a.txt"); x == nil || !x.Cumulative {
		t.Fatalf("turn 2's a.txt also carries turn 1's lines: %+v", w.Files)
	}
}

func TestChangesATranscriptPathIsALiteralPathspec(t *testing.T) {
	f := newChangesFix(t)
	// A file literally named with a glob and a magic prefix must not match anything else.
	writeF(t, filepath.Join(f.repo, "a.txt"), "changed\n")
	at := f.turn(changesBase, "t1", toolUse("Edit", filepath.Join(f.repo, "*.txt")))
	v := f.ok("?turn=" + url.QueryEscape(at.Format(time.RFC3339Nano)))
	if len(v.Files) != 0 {
		t.Fatalf("a glob in a transcript path matched files: %+v", v.Files)
	}
}

func TestTurnsOfReadsOnlyWhatTheTranscriptGained(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	edit := func(id string, at time.Time, p string) {
		transcriptReply(t, path, id, at, false, toolUse("Edit", p))
	}
	for i := 0; i < 200; i++ {
		edit(fmt.Sprintf("m%d", i), changesBase.Add(time.Duration(i)*time.Second), "/work/old.txt")
	}
	x, err := turnsOf(path)
	if err != nil || len(x.edits) != 200 {
		t.Fatalf("first read: %v %d", err, len(x.edits))
	}
	info, _ := os.Stat(path)
	before := turnScanned.Load()
	edit("new", changesBase.Add(time.Hour), "/work/new.txt")
	x, err = turnsOf(path)
	if err != nil || len(x.edits) != 201 || x.edits[200].path != "/work/new.txt" {
		t.Fatalf("after growth: %v %d", err, len(x.edits))
	}
	if read := turnScanned.Load() - before; read <= 0 || read > info.Size()/10 {
		t.Fatalf("a grown file was read for %d bytes of %d: it should start from where it stopped", read, info.Size())
	}

	// A file that got shorter is read again from the start.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	edit("only", changesBase, "/work/only.txt")
	if x, err = turnsOf(path); err != nil || len(x.edits) != 1 || x.edits[0].path != "/work/only.txt" {
		t.Fatalf("after truncation: %v %+v", err, x.edits)
	}

	// A replacement that is longer than what was read is told by its bytes.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		edit(fmt.Sprintf("r%d", i), changesBase.Add(time.Duration(i)*time.Second), fmt.Sprintf("/work/replaced%d.txt", i))
	}
	if x, err = turnsOf(path); err != nil || len(x.edits) != 50 || x.edits[0].path != "/work/replaced0.txt" {
		t.Fatalf("after replacement: %v %d", err, len(x.edits))
	}

	// A line still being written is read once it has its newline.
	fh, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	b, _ := json.Marshal(map[string]any{"type": "assistant", "timestamp": changesBase.Add(time.Hour).Format(time.RFC3339Nano),
		"message": map[string]any{"id": "half", "content": []map[string]any{toolUse("Edit", "/work/half.txt")}}})
	fh.Write(b[:len(b)/2])
	if x, _ = turnsOf(path); len(x.edits) != 50 {
		t.Fatalf("a half line was read: %d", len(x.edits))
	}
	fh.Write(b[len(b)/2:])
	fh.WriteString("\n")
	fh.Close()
	if x, _ = turnsOf(path); len(x.edits) != 51 {
		t.Fatalf("the finished line was not read: %d", len(x.edits))
	}
}

func TestChangesTurnWithNoPromptBeforeItFindsNoCommits(t *testing.T) {
	f := newChangesFix(t)
	c := filepath.Join(f.repo, "c.txt")
	writeF(t, c, "one\ntwo\nthree\n")
	gitT(t, f.repo, changesBase.Add(-30*time.Minute), "commit", "-am", "long ago, nothing to do with the turn")
	// A reply with no prompt recorded before it: the transcript starts mid-conversation.
	transcriptReply(t, f.tr, "tools", changesBase.Add(time.Second), false, toolUse("Edit", c))
	at := changesBase.Add(2 * time.Second)
	transcriptReply(t, f.tr, "text", at, false, textBlock("done"))
	v := f.ok("?turn=" + url.QueryEscape(at.Format(time.RFC3339Nano)))
	if len(v.Files) != 0 || v.Partial == nil || !*v.Partial {
		t.Fatalf("a turn with no start counted history as its own: %+v", v.Files)
	}
}

func TestChangesTurnWithHundredsOfPathsFitsTheCommandLine(t *testing.T) {
	f := newChangesFix(t)
	dir := "a/rather/deeply/nested/directory/structure/for/the/command/line/overflow/test"
	const n = 600
	var edits []map[string]any
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("%s/file-with-a-long-name-%04d.txt", dir, i)
		writeF(t, filepath.Join(f.repo, filepath.FromSlash(name)), "one\n")
		edits = append(edits, toolUse("Edit", filepath.Join(f.repo, filepath.FromSlash(name))))
	}
	gitT(t, f.repo, changesBase.Add(-time.Minute), "add", ".")
	gitT(t, f.repo, changesBase.Add(-time.Minute), "commit", "-m", "many")
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("%s/file-with-a-long-name-%04d.txt", dir, i)
		writeF(t, filepath.Join(f.repo, filepath.FromSlash(name)), "one\ntwo\n")
	}
	if total := n * (len(dir) + 36); total < 32767 {
		t.Fatalf("the paths total %d characters and would not overflow Windows", total)
	}
	at := f.turn(changesBase, "t1", edits...)
	v := f.ok("?turn=" + url.QueryEscape(at.Format(time.RFC3339Nano)))
	if v.Total != n || len(v.Files) != changesFilesMax || v.Cut == nil || v.Cut.Files != n-changesFilesMax {
		t.Fatalf("total %d listed %d cut %+v", v.Total, len(v.Files), v.Cut)
	}
}

func TestChangesAHugeTrackedDiffIsStoppedWhileRead(t *testing.T) {
	f := newChangesFix(t)
	line := strings.Repeat("0123456789abcdef", 64) + "\n"
	big := strings.Repeat(line, (patchMax*2)/len(line))
	writeF(t, filepath.Join(f.repo, "big.txt"), "x\n")
	gitT(t, f.repo, changesBase.Add(-time.Minute), "add", ".")
	gitT(t, f.repo, changesBase.Add(-time.Minute), "commit", "-m", "big")
	writeF(t, filepath.Join(f.repo, "big.txt"), big)
	writeF(t, filepath.Join(f.repo, "z-after.txt"), "z\n")
	v := f.ok("")
	b := fileOf(v, "big.txt")
	if b == nil || b.Hunks != "" || !b.HunksCut || b.Added < 1000 || v.Cut == nil {
		t.Fatalf("big.txt: %+v cut %+v", b, v.Cut)
	}
	if z := fileOf(v, "z-after.txt"); z == nil || z.Hunks == "" {
		t.Fatalf("a small file after it lost its hunks: %+v", z)
	}
}
