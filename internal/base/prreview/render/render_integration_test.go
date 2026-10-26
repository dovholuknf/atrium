//go:build integration

package render

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/golden from the current output")

const prURL = "https://github.com/openziti/tlsuv/pull/378"

func load(t *testing.T) []Finding {
	t.Helper()
	raw, err := os.ReadFile("testdata/findings.json")
	if err != nil {
		t.Fatal(err)
	}
	var fs []Finding
	if err := json.Unmarshal(raw, &fs); err != nil {
		t.Fatal(err)
	}
	return fs
}

func input(t *testing.T, fs []Finding) Input {
	t.Helper()
	diff, err := os.ReadFile("testdata/run/pr.diff")
	if err != nil {
		t.Fatal(err)
	}
	return Input{PRURL: prURL, Diff: string(diff), RunDir: "testdata/run", Findings: fs}
}

// clean is the first three fixture findings: they break no rule.
func clean(t *testing.T) Input { return input(t, load(t)[:3]) }

func fileOf(r Result, tail string) File {
	for _, f := range r.Files {
		if strings.Contains(f.Name, "-"+tail) || strings.HasSuffix(f.Name, tail+".txt") {
			return f
		}
	}
	return File{}
}

func rules(cs []Check) map[int]bool {
	m := map[int]bool{}
	for _, c := range cs {
		m[c.Rule] = true
	}
	return m
}

func TestGolden(t *testing.T) {
	in := input(t, load(t))
	in.Resent = true
	res, resend, err := Render(in)
	if err != nil || resend != nil {
		t.Fatalf("err %v, resend %v", err, resend)
	}
	got := map[string]string{"walk.txt": res.Walk}
	for _, f := range res.Files {
		got[f.Name] = f.Text
	}
	if *update {
		_ = os.RemoveAll("testdata/golden")
		_ = os.MkdirAll("testdata/golden", 0o755)
		for n, s := range got {
			if err := os.WriteFile(filepath.Join("testdata/golden", n), []byte(s), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	ents, err := os.ReadDir("testdata/golden")
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != len(got) {
		t.Errorf("golden holds %d files, render made %d", len(ents), len(got))
	}
	for _, e := range ents {
		want, _ := os.ReadFile(filepath.Join("testdata/golden", e.Name()))
		if got[e.Name()] != string(want) {
			t.Errorf("%s differs.\n--- got\n%s\n--- want\n%s", e.Name(), got[e.Name()], want)
		}
	}
}

func TestACleanSetNeedsNoResend(t *testing.T) {
	res, resend, err := Render(clean(t))
	if err != nil || resend != nil || len(res.Files) != 3 {
		t.Fatalf("files %d, resend %v, err %v", len(res.Files), resend, err)
	}
}

func TestWalkOrder(t *testing.T) {
	fs := []Finding{
		{Sev: "nit", Path: "src/http.c", Line: 20, Rank: 1},
		{Sev: "low", Path: "src/http.c", Line: 22, Rank: 2},
		{Sev: "low", Path: "src/http.c", Line: 21, Rank: 1},
		{Sev: "low", Path: "src/tls_engine.c", Line: 415, Rank: 1},
		{Sev: "low", Path: "src/http.c", Line: 20, Rank: 1},
		{Sev: "high", Path: "src/http.c", Line: 20, Rank: 9},
		{Sev: "nit", Path: "src/tls_engine.c", Line: 412, LeftForClint: true},
	}
	in := input(t, fs)
	d := parseDiff(in.Diff)
	var got []string
	sort.SliceStable(fs, func(a, b int) bool { return before(fs[a], fs[b], d) })
	for _, f := range fs {
		got = append(got, f.Sev+" "+f.Path+":"+strconv.Itoa(f.Line))
	}
	want := []string{
		"nit src/tls_engine.c:412", // left for clint comes first, whatever its severity
		"high src/http.c:20",
		"low src/tls_engine.c:415", // rank 1 ties break on the diff's file order, tls_engine.c is first in it
		"low src/http.c:20",
		"low src/http.c:21",
		"low src/http.c:22", // rank 2
		"nit src/http.c:20",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("order\n got %v\nwant %v", got, want)
	}
}

func TestFileNamesCarryTheWalkPosition(t *testing.T) {
	res, _, _ := Render(clean(t))
	var names []string
	for _, f := range res.Files {
		names = append(names, f.Name)
	}
	want := "01-low-http.c-L21.txt 02-med-tls_engine.c-L412.txt 03-med-tls_engine.c-L413.txt"
	if strings.Join(names, " ") != want {
		t.Fatalf("names %v", names)
	}
	if !strings.Contains(res.Walk, "01-low-http.c-L21.txt") || strings.Count(res.Walk, "  open\n") != 3 {
		t.Fatalf("walk.txt:\n%s", res.Walk)
	}
}

// Rule 5.
func TestRule5ALeakAnyReviewerRaisedStaysInTheList(t *testing.T) {
	in := clean(t)
	in.Raw = []Finding{{Path: "src/http.c", Line: 20, Code: "fd = dup(sock);", Leak: "an fd per call", RaisedBy: "go-security-reviewer"}}
	_, resend, err := Render(in)
	if err != nil || len(resend) != 1 || resend[0].Index != -1 || !strings.Contains(resend[0].Prompt, "go-security-reviewer") {
		t.Fatalf("resend %+v, err %v", resend, err)
	}
	in.Resent = true
	_, _, err = Render(in)
	var fm *FailMergeError
	if !errors.As(err, &fm) || fm.Checks[0].Rule != RuleLeak || !strings.Contains(err.Error(), "src/http.c:20") {
		t.Fatalf("err %v", err)
	}
	// Kept: the same leak survives into the final list, even moved to a neighbouring code text.
	in.Findings = append(in.Findings, Finding{Sev: "low", Path: "src/http.c", Line: 20, Leak: "an fd per call", Proven: "no", Rank: 1})
	in.Findings[len(in.Findings)-1].Says = "x"
	if _, resend, err := Render(in); err != nil || resend != nil {
		t.Fatalf("resend %v, err %v", resend, err)
	}
}

// Rules 8 and 35.
func TestRule8TheLineIsOneThePRAdds(t *testing.T) {
	in := clean(t)
	in.Findings[0].Line = 19 // context in the hunk, not added
	cs := Checks(in)
	if !rules(cs)[RuleLine] || cs[0].Finding != "src/tls_engine.c:19" {
		t.Fatalf("checks %v", cs)
	}
	in.Resent = true
	if _, _, err := Render(in); err == nil || !strings.Contains(err.Error(), "src/tls_engine.c:19") {
		t.Fatalf("err %v", err)
	}
	in = clean(t)
	in.Findings[0].Path = "src/elsewhere.c"
	if !rules(Checks(in))[RuleLine] {
		t.Fatal("a file the PR does not touch passed")
	}
}

// Rule 26.
func TestRule26AMedNeedsAllThreeExposureAnswers(t *testing.T) {
	in := clean(t)
	in.Findings[1].Exposure.Likely = " "
	cs := Checks(in)
	if len(cs) != 1 || cs[0].Rule != RuleExposure || !strings.Contains(cs[0].Detail, "likely") {
		t.Fatalf("checks %v", cs)
	}
	in.Resent = true
	if _, _, err := Render(in); err == nil {
		t.Fatal("a MED with no exposure was written")
	}
	in = clean(t)
	in.Findings[2].Exposure = Exposure{} // low: not asked
	if len(Checks(in)) != 0 {
		t.Fatal("a LOW was asked for exposure")
	}
}

// Rule 34.
func TestRule34LLMReviewSaysOnce(t *testing.T) {
	in := clean(t)
	in.Findings[0].Says = "LLM review says a. And LLM review says b."
	if !rules(Checks(in))[RuleLeadIn] {
		t.Fatal("a doubled lead-in passed")
	}
	in.Resent = true
	res, _, err := Render(in)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(fileOf(res, "L413").Text, "LLM review says"); n != 1 {
		t.Fatalf("lead-in appears %d times:\n%s", n, fileOf(res, "L413").Text)
	}
	in = clean(t)
	in.Findings[0].Says = "LLM review says a."
	if rules(Checks(in))[RuleLeadIn] {
		t.Fatal("one lead-in at the start was refused")
	}
}

// Rule 36.
func TestRule36OneFixNotXOrY(t *testing.T) {
	in := clean(t)
	in.Findings[2].Proven = "code"
	in.Findings[2].Fix = "Log the result, or return it through out."
	if !rules(Checks(in))[RuleOneFix] {
		t.Fatal("X, or Y passed")
	}
	in.Resent = true
	res, _, _ := Render(in)
	text := fileOf(res, "L21.").Text
	if strings.Contains(text, "* Suggested fix") || !strings.Contains(text, "more than one change (rule 36)") {
		t.Fatalf("fix kept:\n%s", text)
	}
}

// Rule 40.
func TestRule40APathThatIsNotInSrcIsFlagged(t *testing.T) {
	in := clean(t)
	in.Findings[0].Says = "see `src/gone.c` and `src/http.c` and `engine_test.c` and `sess->closing` and `a.b`"
	cs := Checks(in)
	if len(cs) != 1 || cs[0].Rule != RulePaths || cs[0].Detail != "not in src/: src/gone.c" {
		t.Fatalf("checks %v", cs)
	}
	in.Resent = true
	res, _, _ := Render(in)
	if !strings.Contains(fileOf(res, "L413").Text, "Path not in src/: src/gone.c\n") {
		t.Fatalf("not flagged:\n%s", fileOf(res, "L413").Text)
	}
	in.RunDir = ""
	if len(Checks(in)) != 0 {
		t.Fatal("rule 40 ran with no run folder")
	}
}

// Rule 43.
func TestRule43SuggestedFixOnlyForProvenCode(t *testing.T) {
	res, _, _ := Render(clean(t))
	for _, f := range res.Files {
		proven := strings.Contains(f.Text, "use after free")
		if has := strings.Contains(f.Text, "* Suggested fix: "); has != proven {
			t.Errorf("%s: Suggested fix %v, proven %v", f.Name, has, proven)
		}
	}
	in := clean(t)
	in.Findings[0].Fix = "Check fd."
	if !rules(Checks(in))[RuleProven] {
		t.Fatal("an unproven fix that is not a question passed")
	}
	in.Resent = true
	res, _, _ = Render(in)
	if strings.Contains(fileOf(res, "L413").Text, "* Check fd.") || !strings.Contains(fileOf(res, "L413").Text, "(rule 43)") {
		t.Fatalf("fix written:\n%s", fileOf(res, "L413").Text)
	}
	for _, bad := range []string{"Could we a? Or b?", "Could we a", "Check it?"} {
		if isQuestion(bad) {
			t.Errorf("%q passed as a question", bad)
		}
	}
	for _, ok := range []string{"Could we check fd?", "What if we log it?", "Should we drop it?"} {
		if !isQuestion(ok) {
			t.Errorf("%q refused", ok)
		}
	}
}

func TestResendQuotesTheRule(t *testing.T) {
	in := clean(t)
	in.Findings[1].Exposure = Exposure{}
	_, resend, err := Render(in)
	if err != nil || len(resend) != 1 || resend[0].Finding != "src/tls_engine.c:412" ||
		!strings.Contains(resend[0].Prompt, "rule 26") || !strings.Contains(resend[0].Prompt, "who actually hits it") {
		t.Fatalf("resend %+v err %v", resend, err)
	}
}

// Review f870d872, finding 1: a partial path passes rule 8, and the link must hash the diff's name for the file.
func TestAPartialPathIsRewrittenToTheDiffsName(t *testing.T) {
	in := clean(t)
	in.Findings[1].Path = "tls_engine.c"
	res, resend, err := Render(in)
	if err != nil || resend != nil {
		t.Fatalf("resend %v, err %v", resend, err)
	}
	f := fileOf(res, "L412")
	want := link(prURL, Finding{Path: "src/tls_engine.c", Line: 412})
	if !strings.Contains(f.Text, "\n"+want+"\n") || !strings.Contains(f.Text, "MED src/tls_engine.c line 412:") {
		t.Fatalf("link or label kept the short path:\n%s", f.Text)
	}
}

func TestAPathThatNamesTwoFilesGoesBackToMerge(t *testing.T) {
	in := clean(t)
	in.Diff += "diff --git a/lib/http.c b/lib/http.c\n--- a/lib/http.c\n+++ b/lib/http.c\n@@ -1,1 +1,2 @@\n a\n+b\n"
	in.Findings[2].Path = "http.c"
	cs := Checks(in)
	if len(cs) != 1 || cs[0].Rule != RuleLine || !strings.Contains(cs[0].Detail, "more than one file") {
		t.Fatalf("checks %v", cs)
	}
	in.Findings[2].Path = "src/http.c"
	if len(Checks(in)) != 0 {
		t.Fatal("the full path was refused")
	}
}

func TestAQuotedDiffNameIsRead(t *testing.T) {
	d := parseDiff("diff --git \"a/a b.c\" \"b/a b.c\"\n--- \"a/a b.c\"\n+++ \"b/a b.c\"\n@@ -1,1 +1,2 @@\n x\n+y\n")
	if ok, _ := d.adds("a b.c", 2); !ok {
		t.Fatalf("diff names %v", d.order)
	}
}

// Review f870d872, finding 2: a second render over the same folder.
func TestASecondWriteRemovesStaleFilesAndRefusesAStartedWalk(t *testing.T) {
	dir := t.TempDir()
	first, _, _ := Render(clean(t))
	if err := Write(dir, first); err != nil {
		t.Fatal(err)
	}
	one := clean(t)
	one.Findings = one.Findings[:1]
	second, _, _ := Render(one)
	if err := Write(dir, second); err != nil {
		t.Fatal(err)
	}
	ents, _ := os.ReadDir(filepath.Join(dir, "findings"))
	if len(ents) != 1 || ents[0].Name() != second.Files[0].Name {
		t.Fatalf("findings/ holds %d files after a rewrite to one", len(ents))
	}

	walkTxt := filepath.Join(dir, "walk.txt")
	if err := os.WriteFile(walkTxt, []byte(second.Files[0].Name+"  done  2026-10-01T10:00Z\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Write(dir, first)
	var ws *WalkStartedError
	if !errors.As(err, &ws) {
		t.Fatalf("err %v", err)
	}
	got, _ := os.ReadFile(walkTxt)
	ents, _ = os.ReadDir(filepath.Join(dir, "findings"))
	if !strings.Contains(string(got), "done") || len(ents) != 1 {
		t.Fatalf("a refused write changed the folder: %q, %d files", got, len(ents))
	}
}

// Review f870d872, finding 3.
func TestAnUnsetRankIsRefusedAndSortsLast(t *testing.T) {
	in := clean(t)
	in.Findings[0].Rank = 0
	if cs := Checks(in); len(cs) != 1 || cs[0].Rule != RuleShape || !strings.Contains(cs[0].Detail, "rank") {
		t.Fatalf("checks %v", cs)
	}
	d := parseDiff(in.Diff)
	unset, ranked := Finding{Sev: "med", Path: "src/http.c", Line: 20}, Finding{Sev: "med", Path: "src/http.c", Line: 21, Rank: 5}
	if before(unset, ranked, d) || !before(ranked, unset, d) {
		t.Fatal("an unset rank walked ahead of a ranked finding")
	}
}

// Review f870d872, nit: rule 10 lists BLOCKING as a label of its own.
func TestBlockingKeepsItsOwnLabel(t *testing.T) {
	in := clean(t)
	in.Findings[1].Sev = "blocking"
	res, _, err := Render(in)
	if err != nil {
		t.Fatal(err)
	}
	f := fileOf(res, "L412")
	if f.Name != "02-blocking-tls_engine.c-L412.txt" || !strings.Contains(f.Text, "\nBLOCKING src/tls_engine.c line 412:") {
		t.Fatalf("%s\n%s", f.Name, f.Text)
	}
}

// A file name can hold a space (git quotes one), and the state is not the second field then.
func TestAWalkWithASpacedNameIsReadByItsState(t *testing.T) {
	dir := t.TempDir()
	files := []File{{Name: "01-low-a b.c-L3.txt", Text: "a"}}
	if err := Write(dir, Result{Files: files, Walk: walk(files)}); err != nil {
		t.Fatal(err)
	}
	if err := Write(dir, Result{Files: files, Walk: walk(files)}); err != nil {
		t.Fatalf("an untouched walk with a spaced name was refused: %v", err)
	}
	started := files[0].Name + "  done  2026-10-01T10:00Z  https://github.com/x/pull/1#r2\n"
	if err := os.WriteFile(filepath.Join(dir, "walk.txt"), []byte(started), 0o644); err != nil {
		t.Fatal(err)
	}
	var ws *WalkStartedError
	if err := Write(dir, Result{Files: files, Walk: walk(files)}); !errors.As(err, &ws) || ws.Lines[0] != files[0].Name+" done" {
		t.Fatalf("a done line with a spaced name was not seen: %v", err)
	}
}

func TestWriteMakesTheFolderTheDrawerOpens(t *testing.T) {
	res, _, _ := Render(clean(t))
	dir := t.TempDir()
	if err := Write(dir, res); err != nil {
		t.Fatal(err)
	}
	for _, f := range res.Files {
		if _, err := os.Stat(filepath.Join(dir, "findings", f.Name)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "walk.txt")); err != nil {
		t.Fatal(err)
	}
}
