//go:build integration

package api

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func TestABlockingFindingIsCountedListedAndMarkable(t *testing.T) {
	s, st, _ := prServer(t)
	b := prDecode(t, prDo(s, s.postPR, "POST", "/v1/prs", `{"url":"`+prURL+`","head":"ad5ddf4aaaa"}`, ""))
	id, dir := b.PR["id"].(string), filepath.FromSlash(b.PR["run_dir"].(string))
	os.WriteFile(filepath.Join(dir, "findings", "01-blocking-a.go-L1.txt"),
		[]byte("https://x/pull/1\nBLOCKING a.go line 1: boom()\nhttps://x/l\n\n* bad\n\nEvidence\nId: aaaa111111\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "findings", "02-high-b.go-L2.txt"), []byte("HIGH b.go line 2: x\n"), 0o644)
	st.SetPRState(id, store.PRReady, "", "")

	got := prDecode(t, prDo(s, s.getPR, "GET", "/x", "", id))
	if f := got.PR["findings"].(map[string]any); f["high"] != float64(2) {
		t.Fatalf("a blocking finding is uncounted: %v", f)
	}
	var fb findingsBody
	json.Unmarshal(drawerDo(s, s.getPRFindings, "GET", "", id).Body.Bytes(), &fb)
	if len(fb.Findings) != 2 {
		t.Fatalf("blocking missing from the list: %+v", fb)
	}
	f := fb.Findings[0]
	if f.Sev != "high" || !f.Blocking || f.Path != "a.go" || f.Line != 1 || f.Code != "boom()" || f.Key != "f-aaaa111111" {
		t.Fatalf("%+v", f)
	}
	if fb.Findings[1].Blocking {
		t.Fatal("a plain high finding is not blocking")
	}
	if w := drawerDo(s, s.walkPRFinding, "POST", `{"state":"done"}`, id, f.Key); w.Code != 200 {
		t.Fatalf("a blocking finding cannot be marked: %d %s", w.Code, w.Body)
	}
}

func TestASpacedFileNameKeepsOneWalkLineAndReadsBack(t *testing.T) {
	s, st, _ := prServer(t)
	b := prDecode(t, prDo(s, s.postPR, "POST", "/v1/prs", `{"url":"`+prURL+`","head":"ad5ddf4aaaa"}`, ""))
	id, dir := b.PR["id"].(string), filepath.FromSlash(b.PR["run_dir"].(string))
	os.WriteFile(filepath.Join(dir, "findings", "01-med-my file.go-L1.txt"),
		[]byte("MED my file.go line 1: x\n\n* y\n\nEvidence\nId: bbbb222222\n"), 0o644)
	st.SetPRState(id, store.PRReady, "", "")
	walkFile := filepath.Join(dir, "walk.txt")
	os.WriteFile(walkFile, []byte("01-med-my file.go-L1.txt  open\n"), 0o644)

	for i := 0; i < 2; i++ {
		if w := drawerDo(s, s.walkPRFinding, "POST", `{"state":"done","url":"https://x.y/z"}`, id, "f-bbbb222222"); w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
	}
	raw, _ := os.ReadFile(walkFile)
	if strings.Count(strings.TrimSpace(string(raw)), "\n") != 0 || !strings.HasPrefix(string(raw), "01-med-my file.go-L1.txt  done  ") {
		t.Fatalf("walk.txt grew a second line:\n%s", raw)
	}
	var fb findingsBody
	json.Unmarshal(drawerDo(s, s.getPRFindings, "GET", "", id).Body.Bytes(), &fb)
	if len(fb.Findings) != 1 || fb.Findings[0].Walk.State != "done" || fb.Findings[0].Walk.URL != "https://x.y/z" {
		t.Fatalf("does not read back: %+v", fb.Findings)
	}
	if _, wc := readPRCounts(filepath.ToSlash(dir)); wc.Done != 1 || wc.Open != 0 {
		t.Fatalf("%+v", wc)
	}
}

func TestParseWalkLineSharesTheRenderersRule(t *testing.T) {
	l, ok := parseWalkLine("01-med-a b.go-L1.txt  deferred  2026-10-01T10:00:00Z  https://x.y/z")
	if !ok || l.File != "01-med-a b.go-L1.txt" || l.State != "deferred" || l.At != "2026-10-01T10:00:00Z" || l.URL != "https://x.y/z" {
		t.Fatalf("%+v %v", l, ok)
	}
	if _, ok := parseWalkLine("01-med-a.txt  maybe"); ok {
		t.Fatal("an unknown state parsed")
	}
	if _, ok := parseWalkLine("garbage"); ok {
		t.Fatal("a line with no name parsed")
	}
}

func TestOnlyOneOfManyRacingRetriesWins(t *testing.T) {
	s, st, _ := prServer(t)
	b := prDecode(t, prDo(s, s.postPR, "POST", "/v1/prs", `{"url":"`+prURL+`"}`, ""))
	id := b.PR["id"].(string) // failed by the stub
	run := &fakePRRunner{st: st}
	s.PRRunner = run

	var wg sync.WaitGroup
	var mu sync.Mutex
	codes := map[int]int{}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := prDo(s, s.retryPR, "POST", "/x", "", id)
			mu.Lock()
			codes[w.Code]++
			mu.Unlock()
		}()
	}
	wg.Wait()
	if codes[202] != 1 || codes[409] != 7 || len(run.started) != 1 {
		t.Fatalf("codes %v started %v", codes, run.started)
	}
}

func TestOnlyOneOfManyRacingStartsAndAbortsWins(t *testing.T) {
	s, st, _ := prServer(t)
	s.PRRunner = &fakePRRunner{st: st}
	run := s.PRRunner.(*fakePRRunner)
	p, _, _ := st.CreatePR(store.NewPR{Org: "o", Repo: "r", Number: 1, Host: "github.com", RunDir: filepath.ToSlash(t.TempDir())})

	var wg sync.WaitGroup
	var mu sync.Mutex
	codes := map[int]int{}
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := prDo(s, s.startPR, "POST", "/x", "", p.ID)
			mu.Lock()
			codes[w.Code]++
			mu.Unlock()
		}()
	}
	wg.Wait()
	if codes[202] != 1 || codes[409] != 5 || len(run.started) != 1 {
		t.Fatalf("start: codes %v started %v", codes, run.started)
	}

	codes = map[int]int{}
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := prDo(s, s.abortPR, "POST", "/x", "", p.ID)
			mu.Lock()
			codes[w.Code]++
			mu.Unlock()
		}()
	}
	wg.Wait()
	if codes[200] != 1 || codes[409] != 5 || len(run.aborted) != 1 {
		t.Fatalf("abort: codes %v aborted %v", codes, run.aborted)
	}
}

func TestARefusedStateChangeIsNotAHalt(t *testing.T) {
	s, _, _ := prServer(t)
	b := prDecode(t, prDo(s, s.postPR, "POST", "/v1/prs", `{"url":"`+prURL+`"}`, ""))
	id := b.PR["id"].(string)
	// failed rows cannot be aborted
	if w := prDo(s, s.abortPR, "POST", "/x", "", id); w.Code != 409 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if halted, _ := s.st.Halted(); halted {
		t.Fatal("a 409 halted the store")
	}
}

func TestAPasteWithNoHeadWhileOneIsLiveFindsTheSameReview(t *testing.T) {
	s, st, _ := prServer(t)
	run := &fakePRRunner{st: st}
	s.PRRunner = run
	first := prDecode(t, prDo(s, s.postPR, "POST", "/v1/prs", `{"url":"`+prURL+`"}`, ""))
	id := first.PR["id"].(string)

	// The fetch step moved the run off pending.
	dir, _ := st.RunFolder("openziti", "tlsuv", 378, "ad5ddf4")
	st.SetPRFetched(id, "ad5ddf4aaaa", "t", "a", dir)

	w := prDo(s, s.postPR, "POST", "/v1/prs", `{"url":"`+prURL+`"}`, "")
	second := prDecode(t, w)
	if w.Code != 200 || second.Created || second.PR["id"] != id || len(run.started) != 1 {
		t.Fatalf("a second review started: %d %s", w.Code, w.Body)
	}
	if rows, _ := st.PRs(store.PRFilter{}); len(rows) != 1 {
		t.Fatalf("%d rows", len(rows))
	}
	// A paste that names a head is the folder's identity, and a different head is a new review.
	w = prDo(s, s.postPR, "POST", "/v1/prs", `{"url":"`+prURL+`","head":"bbbbbbb1111"}`, "")
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestAHaltedStoreInsideRecogniseIs503(t *testing.T) {
	path := filepath.Join(t.TempDir(), "atrium.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s := New(st)
	s.Recognise = func(string) (*store.Resolved, error) {
		// Break the table under the store, then read through it so it halts.
		raw, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		defer raw.Close()
		if _, err := raw.Exec(`DROP TABLE recogniser`); err != nil {
			t.Fatal(err)
		}
		_, err = st.Recognisers()
		return nil, err
	}
	w := prDo(s, s.postPR, "POST", "/v1/prs", `{"url":"`+prURL+`"}`, "")
	if halted, _ := st.Halted(); !halted {
		t.Skip("the store did not halt on a dropped table")
	}
	if w.Code != 503 || !strings.Contains(w.Body.String(), `"halted":true`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestARefusedPostLeavesNoFolder(t *testing.T) {
	s, _, root := prServer(t)
	for _, body := range []string{
		`{"url":"https://example.invalid/x"}`,
		`{"url":"https://github.com/openziti/tlsuv"}`,
		`{"url":"` + prURL + `","head":"xyz"}`,
		`{"url":"` + prURL + `","why":"` + strings.Repeat("a", store.MaxPRWhy+1) + `"}`,
	} {
		if w := prDo(s, s.postPR, "POST", "/v1/prs", body, ""); w.Code < 400 {
			t.Fatalf("%s accepted", body)
		}
	}
	if ents, _ := os.ReadDir(filepath.FromSlash(root)); len(ents) != 0 {
		t.Fatalf("a refused post left %d entries", len(ents))
	}
}

func TestTheWalkerBriefNamesThePullRequestNumber(t *testing.T) {
	s, st, id, dir := readyPR(t)
	var sent map[string]any
	s.Launch = func(body []byte) (*store.Task, error) {
		json.Unmarshal(body, &sent)
		return cardIn(t, st, dir), nil
	}
	if w := drawerDo(s, s.walkerPR, "POST", ``, id); w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	prompt := sent["prompt"].(string)
	if strings.Contains(prompt, "<n>") || !strings.Contains(prompt, "gh pr view 378 --json") {
		t.Fatalf("%s", prompt)
	}
}
