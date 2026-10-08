package forge

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeGH answers the gh commands of the CI reads, no network. It records each command.
type fakeGH struct {
	mu   sync.Mutex
	got  []Cmd
	logs string
	err  error
	// dl is written under --dir when `run download` runs.
	dl map[string]string
	// zipBytes, when set, is the artifact as the forge serves it.
	zipBytes []byte
	zips     int
}

// zipOf is a zip of the named files.
func zipOf(files map[string]string) []byte {
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for n, body := range files {
		f, _ := w.Create(n)
		_, _ = f.Write([]byte(body))
	}
	_ = w.Close()
	return b.Bytes()
}

func (f *fakeGH) run(_ context.Context, c Cmd) ([]byte, error) {
	f.mu.Lock()
	f.got = append(f.got, c)
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	a := strings.Join(c.Args, " ")
	switch {
	case strings.HasPrefix(a, "run list"):
		return []byte(`[{"databaseId":37771754840,"workflowName":"ci","status":"completed","conclusion":"failure",` +
			`"headSha":"8918f846","headBranch":"claude/main","createdAt":"2026-10-08T10:00:00Z","url":"https://github.com/o/r/actions/runs/37771754840"}]`), nil
	case strings.HasPrefix(a, "run view") && strings.Contains(a, "--json"):
		return []byte(`{"databaseId":9,"workflowName":"ci","status":"completed","conclusion":"failure","headSha":"abc",` +
			`"headBranch":"b","createdAt":"t","url":"u","jobs":[{"databaseId":55,"name":"windows","status":"completed",` +
			`"conclusion":"failure","url":"ju","steps":[{"number":1,"name":"checkout","status":"completed","conclusion":"success"},` +
			`{"number":2,"name":"test","status":"completed","conclusion":"failure"}]}]}`), nil
	case strings.HasPrefix(a, "run view"):
		if c.Tail > 0 {
			w := &TailWriter{Lines: c.Tail, Bytes: c.Limit}
			_, _ = w.Write([]byte(f.logs))
			t, _, _ := w.Result()
			return []byte(t), nil
		}
		return []byte(f.logs), nil
	case strings.HasPrefix(a, "api") && strings.HasSuffix(a, "/zip"):
		f.mu.Lock()
		f.zips++
		f.mu.Unlock()
		if f.zipBytes != nil {
			_, err := c.Sink.Write(f.zipBytes)
			return nil, err
		}
		_, err := c.Sink.Write(zipOf(f.dl))
		return nil, err
	case strings.HasPrefix(a, "api"):
		return []byte(`{"artifacts":[{"id":1,"name":"ci","size_in_bytes":20,"expired":false,"created_at":"t"},` +
			`{"id":2,"name":"big","size_in_bytes":999999999999,"expired":false},{"id":3,"name":"old","size_in_bytes":1,"expired":true}]}`), nil
	}
	return nil, fmt.Errorf("unexpected gh %s", a)
}

var ciRef = Ref{Org: "o", Repo: "r"}

func TestCIRunsAreFilteredAndShaped(t *testing.T) {
	f := &fakeGH{}
	g := newGitHub("", f.run)
	runs, err := g.Runs(context.Background(), ciRef, RunQuery{Branch: "claude/main", SHA: "8918f846", Limit: 500})
	if err != nil || len(runs) != 1 {
		t.Fatalf("%+v %v", runs, err)
	}
	r := runs[0]
	if r.ID != 37771754840 || r.Workflow != "ci" || r.Conclusion != "failure" || r.Head != "8918f846" || r.Created == "" || r.URL == "" {
		t.Errorf("%+v", r)
	}
	a := strings.Join(f.got[0].Args, " ")
	for _, want := range []string{"run list", "--repo o/r", "--limit 50", "--branch claude/main", "--commit 8918f846"} {
		if !strings.Contains(a, want) {
			t.Errorf("%q lacks %q", a, want)
		}
	}
}

func TestCIRunHasJobsAndSteps(t *testing.T) {
	g := newGitHub("", (&fakeGH{}).run)
	d, err := g.RunDetail(context.Background(), ciRef, 9)
	if err != nil || len(d.Jobs) != 1 {
		t.Fatalf("%+v %v", d, err)
	}
	j := d.Jobs[0]
	if j.ID != 55 || j.Conclusion != "failure" || len(j.Steps) != 2 || j.Steps[1].Name != "test" || j.Steps[1].Conclusion != "failure" {
		t.Errorf("%+v", j)
	}
}

func TestCILogIsReadBoundedNotCutAfterwards(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 100000; i++ {
		fmt.Fprintf(&b, "windows\ttest\tline %d\n", i)
	}
	f := &fakeGH{logs: b.String()}
	g := newGitHub("", f.run)
	l, err := g.Log(context.Background(), ciRef, LogQuery{RunID: 9, FailedOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	// The runner is asked to bound the read: a tail and a byte cap, so nothing reads the whole log.
	if c := f.got[0]; c.Tail != DefaultTail || c.Limit != LogBytes {
		t.Errorf("cmd = %+v", c)
	}
	if !strings.Contains(strings.Join(f.got[0].Args, " "), "run view 9 --repo o/r --log-failed") {
		t.Errorf("args = %v", f.got[0].Args)
	}
	if !l.Truncated || len(l.Text) > LogBytes+100 || !strings.HasSuffix(l.Text, "line 100000\n") || !strings.HasPrefix(l.Text, TailMarker) {
		t.Errorf("truncated=%v len=%d head=%.60q", l.Truncated, len(l.Text), l.Text)
	}
	if l.Lines > DefaultTail+1 {
		t.Errorf("lines = %d", l.Lines)
	}
}

func TestCILogOfAJobAndOfARunnerThatDoesNotBound(t *testing.T) {
	// A runner that ignores Tail is bounded again by the caller.
	run := func(_ context.Context, c Cmd) ([]byte, error) {
		return []byte(strings.Repeat("x\n", 1000)), nil
	}
	g := newGitHub("", run)
	l, err := g.Log(context.Background(), ciRef, LogQuery{JobID: 55, Tail: 10})
	if err != nil || l.Lines != 10 || !l.Truncated {
		t.Fatalf("%+v %v", l, err)
	}
	if _, err := g.Log(context.Background(), ciRef, LogQuery{}); err == nil {
		t.Error("a log of nothing")
	}
	f := &fakeGH{logs: "a\nb\n"}
	g = newGitHub("", f.run)
	l, _ = g.Log(context.Background(), ciRef, LogQuery{JobID: 55})
	a := strings.Join(f.got[0].Args, " ")
	if !strings.Contains(a, "--job 55") || !strings.Contains(a, "--log") || strings.Contains(a, "--log-failed") || l.Truncated || l.Text != "a\nb\n" {
		t.Errorf("%s %+v", a, l)
	}
}

func TestTailWriterHoldsToItsBoundsWhileWritten(t *testing.T) {
	w := &TailWriter{Lines: 3, Bytes: 40}
	for i := 0; i < 100000; i++ {
		fmt.Fprintf(w, "line %d\n", i)
		if len(w.lines) > 5 || w.size > 60 {
			t.Fatalf("held %d lines, %d bytes", len(w.lines), w.size)
		}
	}
	text, kept, dropped := w.Result()
	if kept != 3 || dropped != 99997 || !strings.HasSuffix(text, "line 99999\n") {
		t.Errorf("%d %d %q", kept, dropped, text)
	}
	// One endless line is bounded too.
	w = &TailWriter{Lines: 3, Bytes: 10}
	for i := 0; i < 1000; i++ {
		_, _ = w.Write([]byte("0123456789"))
	}
	if len(w.partial) > 10 {
		t.Errorf("partial = %d", len(w.partial))
	}
}

func TestExecBoundsALogItReads(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go to print a big output with")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	_ = os.WriteFile(src, []byte(`package main
import ("fmt";"os")
func main(){ w:=os.Stdout; for i:=1;i<=200000;i++{ fmt.Fprintf(w,"row %d\n",i) } }`), 0o644)
	bin := filepath.Join(dir, "printer")
	if out, err := exec.Command("go", "build", "-o", bin, src).CombinedOutput(); err != nil {
		t.Skipf("cannot build the printer: %v %s", err, out)
	}
	out, err := Exec(nil)(context.Background(), Cmd{Name: bin, Tail: 5, Limit: 1 << 10})
	if err != nil {
		t.Fatalf("a long log is not an error: %v", err)
	}
	if !strings.HasSuffix(string(out), "row 200000\n") || strings.Count(string(out), "\n") != 6 || !strings.HasPrefix(string(out), TailMarker) {
		t.Errorf("%q", out)
	}
	// Without Tail the same output is refused, as before.
	if _, err := Exec(nil)(context.Background(), Cmd{Name: bin, Limit: 1 << 10}); err == nil {
		t.Error("want the old refusal")
	}
}

func TestCIArtifactsListedAndDownloadedWithinTheCap(t *testing.T) {
	f := &fakeGH{dl: map[string]string{"summary.txt": "ok\nfine\n"}}
	g := newGitHub("", f.run)
	list, err := g.Artifacts(context.Background(), ciRef, 9)
	if err != nil || len(list) != 3 || list[0].Name != "ci" || list[0].Size != 20 || !list[2].Expired {
		t.Fatalf("%+v %v", list, err)
	}
	dest := t.TempDir()
	d, err := g.Download(context.Background(), ciRef, 9, "ci", dest, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(d.Dir, dest) || len(d.Files) != 1 || d.Files[0].Path != "summary.txt" || d.Bytes != 8 {
		t.Errorf("%+v", d)
	}
	if err := ReadFile(d, "summary.txt", 1); err != nil || d.Text != "fine\n" && !strings.Contains(d.Text, "fine") {
		t.Errorf("%q %v", d.Text, err)
	}
	if err := ReadFile(d, "../../x", 1); err == nil {
		t.Error("a path out of the artifact")
	}
	// Downloaded once: a second ask reads the folder.
	n := len(f.got)
	if _, err := g.Download(context.Background(), ciRef, 9, "ci", dest, 0); err != nil || len(f.got) != n+1 {
		t.Errorf("second ask ran %d more commands: %v", len(f.got)-n, err)
	}
	for name, want := range map[string]string{"big": "over the", "old": "expired", "nope": "has no artifact named"} {
		if _, err := g.Download(context.Background(), ciRef, 9, name, dest, 0); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Over the cap once unpacked: removed.
	f2 := &fakeGH{dl: map[string]string{"a": strings.Repeat("x", 100)}}
	if _, err := newGitHub("", f2.run).Download(context.Background(), ciRef, 9, "ci", t.TempDir(), 50); err == nil {
		t.Error("want the unpacked size refused")
	}
}

func TestCIMissingLoginIsAnAccessError(t *testing.T) {
	for _, e := range []error{errors.New("gh run list: To get started with GitHub CLI, please run:  gh auth login"),
		&exec.Error{Name: "gh", Err: exec.ErrNotFound}} {
		_, err := newGitHub("", (&fakeGH{err: e}).run).Runs(context.Background(), ciRef, RunQuery{})
		var ae *AccessError
		if !errors.As(err, &ae) || !strings.Contains(err.Error(), "gh auth login --hostname github.com") {
			t.Errorf("%v", err)
		}
	}
}

func TestCIOnBitbucketIsNotSupported(t *testing.T) {
	b, _ := New(Bitbucket, "", nil)
	_, err := CIOf(b)
	var ns *NotSupportedError
	if !errors.As(err, &ns) || ns.Code() != "not_supported" || !strings.Contains(err.Error(), "not supported on bitbucket") {
		t.Fatalf("%v", err)
	}
	g, _ := New(GitHub, "", nil)
	if _, err := CIOf(g); err != nil {
		t.Errorf("%v", err)
	}
}

// THE CAP HOLDS WHILE THE ZIP IS UNPACKED: a zip bomb of small headers and large content stops at the cap, and leaves
// nothing behind.
func TestCIDownloadStopsAZipBombWhileWriting(t *testing.T) {
	f := &fakeGH{zipBytes: zipOf(map[string]string{"a.bin": strings.Repeat("\x00", 5<<20), "b.bin": strings.Repeat("\x00", 5<<20)})}
	dest := t.TempDir()
	_, err := newGitHub("", f.run).Download(context.Background(), ciRef, 9, "ci", dest, 1<<20)
	if err == nil || !strings.Contains(err.Error(), "byte cap once unpacked") {
		t.Fatalf("%v", err)
	}
	var left []string
	_ = filepath.WalkDir(dest, func(p string, e os.DirEntry, _ error) error {
		if !e.IsDir() {
			left = append(left, p)
		}
		return nil
	})
	if len(left) != 0 {
		t.Errorf("left behind: %v", left)
	}
	// The zip itself is bounded as it is written: gh printing more than it listed is stopped.
	f = &fakeGH{zipBytes: bytes.Repeat([]byte("z"), 3<<20)}
	if _, err := newGitHub("", f.run).Download(context.Background(), ciRef, 9, "ci", t.TempDir(), 1<<20); err == nil {
		t.Error("want the oversize zip refused")
	}
}

func TestCIDownloadRefusesEntriesOutsideItsFolder(t *testing.T) {
	for _, name := range []string{"../evil.txt", "/etc/evil.txt", "a/../../evil.txt", "..\\evil.txt"} {
		f := &fakeGH{dl: map[string]string{name: "x"}}
		dest := t.TempDir()
		_, err := newGitHub("", f.run).Download(context.Background(), ciRef, 9, "ci", dest, 0)
		if err == nil || !strings.Contains(err.Error(), "outside its own folder") {
			t.Errorf("%q: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(dest), "evil.txt")); err == nil {
			t.Errorf("%q escaped", name)
		}
	}
}

func TestPruneDownloadsByAgeAndTotalSize(t *testing.T) {
	root := t.TempDir()
	mk := func(run string, size int, age time.Duration) string {
		d := filepath.Join(root, "github.com", "o", "r", run, "ci")
		_ = os.MkdirAll(d, 0o755)
		_ = os.WriteFile(filepath.Join(d, "f"), make([]byte, size), 0o644)
		at := time.Now().Add(-age)
		_ = os.Chtimes(d, at, at)
		return d
	}
	old := mk("1", 10, 48*time.Hour)
	a := mk("2", 100, 3*time.Hour)
	b := mk("3", 100, 2*time.Hour)
	c := mk("4", 100, time.Hour)
	writing := filepath.Join(root, "github.com", "o", "r", "5", "ci"+tmpMark+"x")
	_ = os.MkdirAll(writing, 0o755)
	_ = os.WriteFile(filepath.Join(writing, "f"), make([]byte, 500), 0o644)
	PruneDownloads(root, 24*time.Hour, 250)
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	if exists(old) || exists(a) || !exists(b) || !exists(c) || !exists(writing) {
		t.Errorf("old=%v a=%v b=%v c=%v writing=%v", exists(old), exists(a), exists(b), exists(c), exists(writing))
	}
}

func TestCIConcurrentDownloadsOfOneArtifactDoNotRace(t *testing.T) {
	f := &fakeGH{dl: map[string]string{"summary.txt": "ok\n"}}
	g := newGitHub("", f.run)
	dest := t.TempDir()
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := g.Download(context.Background(), ciRef, 9, "ci", dest, 0)
			if err == nil && (len(d.Files) != 1 || d.Files[0].Path != "summary.txt") {
				err = fmt.Errorf("files = %+v", d.Files)
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
}

// A DOWNLOAD IN USE IS NEVER PRUNED: another download's prune, with a cap that would take it, leaves it alone, and takes
// it once it is released.
func TestPruneNeverTakesADownloadInUse(t *testing.T) {
	old := pruneBytes
	pruneBytes = 1
	t.Cleanup(func() { pruneBytes = old })
	f := &fakeGH{dl: map[string]string{"summary.txt": "some bytes\n"}}
	g := newGitHub("", f.run)
	dest := t.TempDir()
	a, err := g.Download(context.Background(), ciRef, 1, "ci", dest, 0)
	if err != nil {
		t.Fatal(err)
	}
	// B's prune would hit A: the cap is one byte and A is the older.
	b, err := g.Download(context.Background(), ciRef, 2, "ci", dest, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(a.Dir, "summary.txt")); err != nil {
		t.Fatalf("a download in use was pruned: %v", err)
	}
	if err := ReadFile(a, "summary.txt", 5); err != nil || !strings.Contains(a.Text, "some bytes") {
		t.Errorf("%v %q", err, a.Text)
	}
	a.Release()
	b.Release()
	if _, err := g.Download(context.Background(), ciRef, 3, "ci", dest, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(a.Dir); err == nil {
		t.Error("a released download was kept past the cap")
	}
}

func TestACacheHitKeepsTheFolderFromAgingOut(t *testing.T) {
	f := &fakeGH{dl: map[string]string{"summary.txt": "x\n"}}
	g := newGitHub("", f.run)
	dest := t.TempDir()
	d, err := g.Download(context.Background(), ciRef, 1, "ci", dest, 0)
	if err != nil {
		t.Fatal(err)
	}
	d.Release()
	old := time.Now().Add(-23 * time.Hour)
	_ = os.Chtimes(d.Dir, old, old)
	d, err = g.Download(context.Background(), ciRef, 1, "ci", dest, 0)
	if err != nil {
		t.Fatal(err)
	}
	d.Release()
	if st, _ := os.Stat(d.Dir); time.Since(st.ModTime()) > time.Minute || f.zips != 1 {
		t.Errorf("mtime age %v, downloads %d", time.Since(st.ModTime()), f.zips)
	}
}

// Entry names are judged the same on every OS, so this holds on macOS for the hub that runs on Windows.
func TestEntryNamesAreJudgedLikeWindowsOnEveryOS(t *testing.T) {
	for _, bad := range []string{"", "/a", "../a", "a/../b", "a//b", "./a", "C:/x", "C:x", "a:b", "a\\b", "..\\a", "CON", "con.txt",
		"a/NUL", "a/nul.tar.gz", "COM1", "lpt9.log", "AUX.", "a.", "a /b", "b ", "x/y.", "a\x00b", "a\x1fb"} {
		if safeEntryName(bad) {
			t.Errorf("%q was accepted", bad)
		}
	}
	for _, good := range []string{"a", "a/b.txt", "dir/", "build.claude/ci/summary.json", "console.log", "com10", "lpt", ".hidden", "a b/c"} {
		if !safeEntryName(good) {
			t.Errorf("%q was refused", good)
		}
	}
	f := &fakeGH{dl: map[string]string{"C:evil.txt": "x"}}
	if _, err := newGitHub("", f.run).Download(context.Background(), ciRef, 9, "ci", t.TempDir(), 0); err == nil {
		t.Error("a drive name unpacked")
	}
}
