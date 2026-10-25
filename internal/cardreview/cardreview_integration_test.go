//go:build integration

package cardreview

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/base/safepath"
	"github.com/dovholuknf/atrium/internal/store"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

type rig struct {
	svc *Service
	dir string
	ctx context.Context
}

func newRig(t *testing.T) *rig {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "atrium.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	write(t, filepath.Join(dir, "lib", "a.txt"), "one\ntwo\nthree\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "base")
	runGit(t, dir, "checkout", "-b", "feature")
	write(t, filepath.Join(dir, "lib", "a.txt"), "one\ntwo\nthree\nDEBUG print\n")
	write(t, filepath.Join(dir, "lib", "b.txt"), "alpha\nbeta\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "feature")
	return &rig{svc: &Service{St: st}, dir: filepath.ToSlash(dir), ctx: context.Background()}
}

func (r *rig) start(t *testing.T) StartOut {
	t.Helper()
	out, err := r.svc.Start(r.ctx, "c1", r.dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestStartFindsTheMergeBaseAndTheChangedFiles(t *testing.T) {
	r := newRig(t)
	out := r.start(t)
	if len(out.MergeBase) < 7 || len(out.Files) != 2 {
		t.Fatalf("%+v", out)
	}
	if out.Files[0].Path != "lib/a.txt" || out.Files[0].Added != 1 || out.Files[1].Added != 2 {
		t.Fatalf("files: %+v", out.Files)
	}
	if _, err := r.svc.Start(r.ctx, "c1", r.dir, "nope"); err == nil {
		t.Fatal("an unknown base was accepted")
	}
}

func TestFindingMeasuresTheDiffAndTheCountAgainstTheBase(t *testing.T) {
	r := newRig(t)
	r.start(t)
	out, err := r.svc.Finding(r.ctx, "c1", r.dir, FindingIn{
		Cat: "hygiene", Kind: "code", Site: "lib/a.txt:4", Title: "debug print",
		Edits: []Edit{{Path: "lib/a.txt", Old: "DEBUG print\n", New: ""}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Num != 1 || out.Lines != 0 {
		t.Fatalf("after removing the only added line the count against the base is 0: %+v", out)
	}
	f, _ := r.svc.St.FindingGet("c1", 1)
	for _, want := range []string{"--- a/lib/a.txt", "+++ b/lib/a.txt", "-DEBUG print"} {
		if !strings.Contains(f.Diff, want) {
			t.Errorf("diff lacks %q:\n%s", want, f.Diff)
		}
	}
	if f.Cat != "HYGIENE" || f.State != store.FindingWaiting || f.BaseDiff != "" {
		t.Errorf("%+v", f)
	}
	if got := read(t, filepath.Join(r.dir, "lib/a.txt")); !strings.Contains(got, "DEBUG print") {
		t.Errorf("a finding wrote to the card before it was applied: %q", got)
	}

	out, err = r.svc.Finding(r.ctx, "c1", r.dir, FindingIn{
		Cat: "SIMPLIFY", Title: "b", Edits: []Edit{{Path: "lib/b.txt", Old: "beta", New: "gamma"}},
	})
	if err != nil || out.Num != 2 || out.Lines != 2 {
		t.Fatalf("a new file of two lines: %+v %v", out, err)
	}
	f2, _ := r.svc.St.FindingGet("c1", 2)
	if f2.State != store.FindingQueued || !strings.Contains(f2.BaseDiff, "+gamma") || !strings.Contains(f2.BaseDiff, "--- /dev/null") {
		t.Errorf("%+v", f2)
	}
}

func TestFindingRefusesWhatItCannotMeasure(t *testing.T) {
	r := newRig(t)
	if _, err := r.svc.Finding(r.ctx, "c1", r.dir, FindingIn{Cat: "DEAD", Edits: []Edit{{Path: "lib/a.txt", Old: "one", New: "1"}}}); err == nil {
		t.Error("a finding before start was accepted")
	}
	r.start(t)
	cases := map[string]FindingIn{
		"unknown category": {Cat: "VIBES", Edits: []Edit{{Path: "lib/a.txt", Old: "one", New: "1"}}},
		"no edits":         {Cat: "DEAD"},
		"old not found":    {Cat: "DEAD", Edits: []Edit{{Path: "lib/a.txt", Old: "zzz", New: "1"}}},
		"old twice":        {Cat: "DEAD", Edits: []Edit{{Path: "lib/a.txt", Old: "e\n", New: "1"}}},
		"outside the card": {Cat: "DEAD", Edits: []Edit{{Path: "../x.txt", Old: "a", New: "b"}}},
	}
	for name, in := range cases {
		if _, err := r.svc.Finding(r.ctx, "c1", r.dir, in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if fs, _ := r.svc.St.Findings("c1"); len(fs) != 0 {
		t.Errorf("a refused finding was stored: %+v", fs)
	}
}

func TestApplyWritesInsideTheCardAndTakesTheNextFinding(t *testing.T) {
	r := newRig(t)
	r.start(t)
	r.svc.Finding(r.ctx, "c1", r.dir, FindingIn{Cat: "HYGIENE", Edits: []Edit{{Path: "lib/a.txt", Old: "DEBUG print\n", New: ""}}})
	r.svc.Finding(r.ctx, "c1", r.dir, FindingIn{Cat: "DEAD", Edits: []Edit{{Path: "lib/b.txt", Old: "alpha\n", New: ""}}})

	if err := r.svc.Apply("c1", r.dir, 1); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(r.dir, "lib/a.txt")); got != "one\ntwo\nthree\n" {
		t.Errorf("not applied: %q", got)
	}
	f1, _ := r.svc.St.FindingGet("c1", 1)
	f2, _ := r.svc.St.FindingGet("c1", 2)
	if f1.State != store.FindingApplied || f2.State != store.FindingWaiting {
		t.Errorf("states: %s %s", f1.State, f2.State)
	}
	if err := r.svc.Apply("c1", r.dir, 1); err == nil {
		t.Error("applied twice")
	}
}

func TestApplyRefusesAPathOutsideTheCard(t *testing.T) {
	r := newRig(t)
	r.start(t)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	write(t, outside, "keep\n")
	raw := `[{"path":` + quote(filepath.ToSlash(outside)) + `,"old":"keep","new":"gone"},{"path":"../up.txt","old":"a","new":"b"}]`
	for _, edits := range []string{raw, `[{"path":` + quote(filepath.ToSlash(outside)) + `,"old":"keep","new":"gone"}]`} {
		if err := addRaw(r, edits); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []int{1, 2} {
		err := r.svc.Apply("c1", r.dir, n)
		if err == nil || err.Error() != safepath.ErrOutside.Error() {
			t.Errorf("finding %d: %v", n, err)
		}
	}
	if got := read(t, outside); got != "keep\n" {
		t.Errorf("a file outside the card was written: %q", got)
	}
}

func addRaw(r *rig, edits string) error {
	_, err := r.svc.St.FindingAdd(store.Finding{Card: "c1", Cat: "DEAD", Edits: edits, Grp: "single"})
	return err
}

func quote(s string) string { return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"` }

func TestApplyWritesNothingWhenOneEditOfTheFindingFails(t *testing.T) {
	r := newRig(t)
	r.start(t)
	err := addRaw(r, `[{"path":"lib/a.txt","old":"one","new":"1"},{"path":"lib/b.txt","old":"nothere","new":"x"}]`)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.svc.Apply("c1", r.dir, 1); err == nil {
		t.Fatal("applied")
	}
	if got := read(t, filepath.Join(r.dir, "lib/a.txt")); !strings.HasPrefix(got, "one\n") {
		t.Errorf("the first edit was written: %q", got)
	}
}

func TestBatchAppliesAndAVetoPutsItBack(t *testing.T) {
	r := newRig(t)
	r.start(t)
	outs, err := r.svc.Batch(r.ctx, "c1", r.dir, []FindingIn{
		{Cat: "HYGIENE", Title: "debug", Edits: []Edit{{Path: "lib/a.txt", Old: "DEBUG print\n", New: ""}}},
		{Cat: "CHURN", Title: "alpha", Edits: []Edit{{Path: "lib/b.txt", Old: "alpha", New: "ALPHA"}}},
	})
	if err != nil || len(outs) != 2 {
		t.Fatalf("%+v %v", outs, err)
	}
	if got := read(t, filepath.Join(r.dir, "lib/a.txt")); strings.Contains(got, "DEBUG") {
		t.Errorf("a batch item was not applied: %q", got)
	}
	fs, _ := r.svc.St.Findings("c1")
	if fs[0].State != store.FindingApplied || fs[0].Grp != "batch" {
		t.Errorf("%+v", fs[0])
	}
	if err := r.svc.Close("c1", r.dir, 2, store.FindingVetoed, "I like it"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(r.dir, "lib/b.txt")); got != "alpha\nbeta\n" {
		t.Errorf("a veto did not put it back: %q", got)
	}
	if err := r.svc.Close("c1", r.dir, 1, "applied", ""); err == nil {
		t.Error("close took a state it does not own")
	}
}

func TestViewIsTheReviewTheBoardDraws(t *testing.T) {
	r := newRig(t)
	if _, err := r.svc.View("c1"); err != store.ErrNoReview {
		t.Fatalf("a card with no review: %v", err)
	}
	r.start(t)
	v, err := r.svc.View("c1")
	if err != nil || v.Base != "main" || len(v.Files) != 2 || v.Findings == nil || len(v.Findings) != 0 {
		t.Fatalf("%+v %v", v, err)
	}
}
