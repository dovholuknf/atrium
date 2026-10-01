package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

type fakePRRunner struct {
	started, aborted []string
	st               *store.Store
}

func (f *fakePRRunner) Start(id string) {
	f.started = append(f.started, id)
	f.st.SetPRState(id, store.PRRunning, "panel", "")
}
func (f *fakePRRunner) Abort(id string) { f.aborted = append(f.aborted, id) }

func prServer(t *testing.T) (*Server, *store.Store, string) {
	t.Helper()
	s, st := recogniserServer(t)
	root := filepath.ToSlash(t.TempDir())
	st.DefaultReviewsRoot = root
	s.Recognise = func(url string) (*store.Resolved, error) {
		if !strings.Contains(url, "github.com") {
			return nil, store.ErrNoRecogniser
		}
		parts := strings.Split(strings.TrimPrefix(url, "https://github.com/"), "/")
		if len(parts) < 4 || parts[2] != "pull" {
			return &store.Resolved{Vars: map[string]string{"host": "github.com", "org": parts[0]}}, nil
		}
		return &store.Resolved{Vars: map[string]string{"host": "github.com", "org": parts[0], "repo": parts[1],
			"num": parts[3]}}, nil
	}
	return s, st, root
}

func prDo(s *Server, h http.HandlerFunc, method, target, body, id string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	if id != "" {
		r.SetPathValue("id", id)
	}
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

type prAnswerBody struct {
	PR      map[string]any `json:"pr"`
	Created bool           `json:"created"`
	Error   string         `json:"error"`
	Code    string         `json:"code"`
	State   string         `json:"state"`
	RunLog  string         `json:"run_log"`
}

func prDecode(t *testing.T, w *httptest.ResponseRecorder) prAnswerBody {
	t.Helper()
	var b prAnswerBody
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatalf("body %q: %v", w.Body.String(), err)
	}
	return b
}

const prURL = "https://github.com/openziti/tlsuv/pull/378"

func TestPostingAPRMakesARowAndAFolderAndStartsIt(t *testing.T) {
	s, st, _ := prServer(t)
	run := &fakePRRunner{st: st}
	s.PRRunner = run

	w := prDo(s, s.postPR, "POST", "/v1/prs", `{"url":"`+prURL+`","why":"security"}`, "")
	if w.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	b := prDecode(t, w)
	if !b.Created || b.PR["state"] != "running" || b.PR["org_repo"] != "openziti/tlsuv" || b.PR["number"] != float64(378) {
		t.Fatalf("answer: %s", w.Body)
	}
	if len(run.started) != 1 {
		t.Fatalf("started %v", run.started)
	}
	dir, _ := b.PR["run_dir"].(string)
	if !strings.HasSuffix(dir, "/github-openziti-tlsuv/pr-378-pending") {
		t.Fatalf("run_dir %q", dir)
	}
	for _, sub := range []string{"steps", "findings"} {
		if _, err := os.Stat(filepath.Join(filepath.FromSlash(dir), sub)); err != nil {
			t.Fatal(err)
		}
	}
	// counts objects are always present
	for _, k := range []string{"findings", "walk"} {
		if _, ok := b.PR[k].(map[string]any); !ok {
			t.Fatalf("%s missing: %s", k, w.Body)
		}
	}

	// The same PR again finds the same row and starts nothing.
	w = prDo(s, s.postPR, "POST", "/v1/prs", `{"url":"`+prURL+`"}`, "")
	b2 := prDecode(t, w)
	if w.Code != http.StatusOK || b2.Created || b2.PR["id"] != b.PR["id"] || len(run.started) != 1 {
		t.Fatalf("second post: %d %s started=%v", w.Code, w.Body, run.started)
	}
}

func TestPostingAPRRefusals(t *testing.T) {
	s, _, _ := prServer(t)
	cases := []struct {
		name, body string
		status     int
		code       string
	}{
		{"not json", `nope`, 400, "bad_request"},
		{"no url", `{"url":" "}`, 400, "bad_request"},
		{"bad head", `{"url":"` + prURL + `","head":"xyz"}`, 400, "bad_request"},
		{"long why", `{"url":"` + prURL + `","why":"` + strings.Repeat("a", store.MaxPRWhy+1) + `"}`, 400, "bad_request"},
		{"no recogniser", `{"url":"https://example.invalid/x"}`, 422, "no_recogniser"},
		{"not a pr", `{"url":"https://github.com/openziti/tlsuv"}`, 422, "not_a_pr"},
	}
	for _, c := range cases {
		w := prDo(s, s.postPR, "POST", "/v1/prs", c.body, "")
		b := prDecode(t, w)
		if w.Code != c.status || b.Code != c.code || b.Error == "" {
			t.Errorf("%s: %d %s", c.name, w.Code, w.Body)
		}
	}
	s.Recognise = nil
	if w := prDo(s, s.postPR, "POST", "/v1/prs", `{"url":"`+prURL+`"}`, ""); w.Code != 501 {
		t.Errorf("unwired: %d", w.Code)
	}
}

// With no runner wired the stub fails the run at once and the row says so.
func TestTheStubRunnerFailsTheRowAndARepostRetriesIt(t *testing.T) {
	s, _, _ := prServer(t)
	w := prDo(s, s.postPR, "POST", "/v1/prs", `{"url":"`+prURL+`","head":"ad5ddf4aaaa"}`, "")
	b := prDecode(t, w)
	if w.Code != 201 || b.PR["state"] != "failed" || b.PR["run_error"] != "runner: runner not built" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if !strings.HasSuffix(b.PR["run_dir"].(string), "pr-378-ad5ddf4") {
		t.Fatalf("run_dir %v", b.PR["run_dir"])
	}
	run := &fakePRRunner{st: s.st}
	s.PRRunner = run
	w = prDo(s, s.postPR, "POST", "/v1/prs", `{"url":"`+prURL+`","head":"ad5ddf4aaaa"}`, "")
	b = prDecode(t, w)
	if w.Code != 200 || b.Created || b.PR["state"] != "running" || len(run.started) != 1 {
		t.Fatalf("repost of a failed row: %d %s", w.Code, w.Body)
	}
}

func TestListingAndTheNavCount(t *testing.T) {
	s, st, _ := prServer(t)
	for _, n := range []string{"1", "2", "3"} {
		w := prDo(s, s.postPR, "POST", "/v1/prs", `{"url":"https://github.com/o/r/pull/`+n+`"}`, "")
		id := prDecode(t, w).PR["id"].(string)
		if n == "1" {
			st.SetPRState(id, store.PRReady, "", "")
		}
	}
	// 1 is ready, 2 and 3 are failed by the stub
	w := prDo(s, s.listPRs, "GET", "/v1/prs", "", "")
	var out struct {
		PRs      []map[string]any `json:"prs"`
		Counts   map[string]int   `json:"counts"`
		NavCount int              `json:"nav_count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.PRs) != 3 || out.NavCount != 3 || len(out.Counts) != 6 || out.Counts["ready"] != 1 || out.Counts["failed"] != 2 {
		t.Fatalf("%s", w.Body)
	}
	w = prDo(s, s.listPRs, "GET", "/v1/prs?state=ready", "", "")
	json.Unmarshal(w.Body.Bytes(), &out)
	if len(out.PRs) != 1 || out.NavCount != 3 {
		t.Fatalf("filtered: %s", w.Body)
	}
	if w = prDo(s, s.listPRs, "GET", "/v1/prs?state=bogus", "", ""); w.Code != 400 {
		t.Fatalf("bogus state: %d", w.Code)
	}
}

func TestAnEmptyIndexIsNotNull(t *testing.T) {
	s, _, _ := prServer(t)
	w := prDo(s, s.listPRs, "GET", "/v1/prs", "", "")
	if !strings.Contains(w.Body.String(), `"prs":[]`) || !strings.Contains(w.Body.String(), `"nav_count":0`) {
		t.Fatalf("%s", w.Body)
	}
}

func TestARowReadsItsFolderForCountsAndLog(t *testing.T) {
	s, st, _ := prServer(t)
	w := prDo(s, s.postPR, "POST", "/v1/prs", `{"url":"`+prURL+`"}`, "")
	b := prDecode(t, w)
	id, dir := b.PR["id"].(string), filepath.FromSlash(b.PR["run_dir"].(string))
	write := func(name, text string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("findings/01-med-a.go-L1.txt", "x\nLeak: token in log\n")
	write("findings/02-medium-b.go-L2.txt", "x\n")
	write("findings/03-nit-c.go-L3.txt", "x\n")
	write("findings/notes.txt", "not a finding\n")
	write("walk.txt", "01-med-a.go-L1.txt done 2026-10-01T14:20Z https://x\n03-nit-c.go-L3.txt deferred 2026-10-01T14:21Z\n")
	write("run.log", "14:02:11 fetch start\n")

	// Not ready: all zeros, however many files there are.
	got := prDecode(t, prDo(s, s.getPR, "GET", "/v1/prs/"+id, "", id))
	if got.PR["findings"].(map[string]any)["med"] != float64(0) || got.RunLog != "14:02:11 fetch start\n" {
		t.Fatalf("running row: %+v", got)
	}
	st.SetPRState(id, store.PRReady, "", "")
	got = prDecode(t, prDo(s, s.getPR, "GET", "/v1/prs/"+id, "", id))
	f, wk := got.PR["findings"].(map[string]any), got.PR["walk"].(map[string]any)
	if f["med"] != float64(2) || f["nit"] != float64(1) || f["leak"] != float64(1) || f["high"] != float64(0) {
		t.Fatalf("findings %v", f)
	}
	if wk["done"] != float64(1) || wk["deferred"] != float64(1) || wk["open"] != float64(1) || wk["skipped"] != float64(0) {
		t.Fatalf("walk %v", wk)
	}
}

func TestTheRunLogTailStartsAtALine(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "run")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "run.log"), []byte("aaaa\nbbbb\ncccc\n"), 0o644)
	if got := tailLog(filepath.ToSlash(root), filepath.ToSlash(dir), 8); got != "cccc\n" {
		t.Fatalf("%q", got)
	}
	if got := tailLog(filepath.ToSlash(root), filepath.ToSlash(dir), 1000); got != "aaaa\nbbbb\ncccc\n" {
		t.Fatalf("%q", got)
	}
	// A folder outside the root has no log to read.
	if tailLog(filepath.ToSlash(t.TempDir()), filepath.ToSlash(dir), 1000) != "" {
		t.Fatal("read a log outside the root")
	}
	// A run.log that links out of the folder is not followed.
	secret := filepath.Join(t.TempDir(), "secret.txt")
	os.WriteFile(secret, []byte("secret\n"), 0o644)
	linked := filepath.Join(root, "linked")
	os.MkdirAll(linked, 0o755)
	if err := os.Symlink(secret, filepath.Join(linked, "run.log")); err == nil {
		if got := tailLog(filepath.ToSlash(root), filepath.ToSlash(linked), 1000); got != "" {
			t.Fatalf("followed a link out: %q", got)
		}
	}
	if tailLog(filepath.ToSlash(root), filepath.ToSlash(t.TempDir()), 10) != "" {
		t.Fatal("no log")
	}
}

func TestAMissingRowIsThePullsNotFound(t *testing.T) {
	s, _, _ := prServer(t)
	for name, h := range map[string]http.HandlerFunc{"get": s.getPR, "retry": s.retryPR, "abort": s.abortPR,
		"start": s.startPR} {
		w := prDo(s, h, "POST", "/x", "", "nope")
		if b := prDecode(t, w); w.Code != 404 || b.Code != "not_found" || b.Error != "no such pr" {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}
}

func TestAbortRetryAndStartGuards(t *testing.T) {
	s, st, root := prServer(t)
	run := &fakePRRunner{st: st}
	s.PRRunner = run
	w := prDo(s, s.postPR, "POST", "/v1/prs", `{"url":"`+prURL+`"}`, "")
	b := prDecode(t, w)
	id, dir := b.PR["id"].(string), b.PR["run_dir"].(string)

	if w := prDo(s, s.retryPR, "POST", "/x", "", id); w.Code != 409 || prDecode(t, w).Code != "not_retryable" {
		t.Fatalf("retry of a running row: %d %s", w.Code, w.Body)
	}
	if w := prDo(s, s.startPR, "POST", "/x", "", id); w.Code != 409 || prDecode(t, w).Code != "not_startable" {
		t.Fatalf("start of a running row: %d %s", w.Code, w.Body)
	}

	w = prDo(s, s.abortPR, "POST", "/x", "", id)
	b = prDecode(t, w)
	if w.Code != 200 || b.PR["state"] != "aborted" || len(run.aborted) != 1 {
		t.Fatalf("abort: %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.FromSlash(dir)); !os.IsNotExist(err) {
		t.Fatalf("the run folder is still there: %v", err)
	}
	if _, err := os.Stat(filepath.FromSlash(root)); err != nil {
		t.Fatal("the reviews root went with it")
	}
	if w := prDo(s, s.abortPR, "POST", "/x", "", id); w.Code != 409 || prDecode(t, w).Code != "not_abortable" {
		t.Fatalf("second abort: %d %s", w.Code, w.Body)
	}

	w = prDo(s, s.retryPR, "POST", "/x", "", id)
	b = prDecode(t, w)
	if w.Code != 202 || b.PR["state"] != "running" || len(run.started) != 2 {
		t.Fatalf("retry of an aborted row: %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(filepath.FromSlash(b.PR["run_dir"].(string)), "findings")); err != nil {
		t.Fatalf("retry made no new folder: %v", err)
	}
}

func TestAbortLeavesAFolderOutsideTheRootAlone(t *testing.T) {
	s, st, _ := prServer(t)
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "keep.txt"), []byte("x"), 0o644)
	p, _, err := st.CreatePR(store.NewPR{Org: "o", Repo: "r", Number: 1, Host: "github.com",
		RunDir: filepath.ToSlash(outside)})
	if err != nil {
		t.Fatal(err)
	}
	w := prDo(s, s.abortPR, "POST", "/x", "", p.ID)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(outside, "keep.txt")); err != nil {
		t.Fatal("a folder outside the reviews root was deleted")
	}
}

func TestRetryOfAFailedRowAndStartOfAQueuedOne(t *testing.T) {
	s, st, _ := prServer(t)
	// the stub fails it
	b := prDecode(t, prDo(s, s.postPR, "POST", "/v1/prs", `{"url":"`+prURL+`"}`, ""))
	id := b.PR["id"].(string)
	run := &fakePRRunner{st: st}
	s.PRRunner = run
	w := prDo(s, s.retryPR, "POST", "/x", "", id)
	if b = prDecode(t, w); w.Code != 202 || b.PR["state"] != "running" || b.PR["run_error"] != "" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	st.ResetPR(id, "")
	if w := prDo(s, s.startPR, "POST", "/x", "", id); w.Code != 202 || len(run.started) != 2 {
		t.Fatalf("start: %d %s", w.Code, w.Body)
	}
}
