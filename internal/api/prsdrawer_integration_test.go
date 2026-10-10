//go:build integration

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

const findingText = "https://github.com/openziti/tlsuv/pull/378\n" +
	"MED src/tls.go line 5: committed = true\n" +
	"https://github.com/openziti/tlsuv/pull/378/files#diff-abR5\n\n" +
	"* LLM review says it leaks\n* Suggested fix: close it\n\n" +
	"Evidence\nId: 3a9c01d4e2\nCause: pre-existing\nLeak: fd, one per call\n"

const diffText = "diff --git a/src/tls.go b/src/tls.go\n--- a/src/tls.go\n+++ b/src/tls.go\n" +
	"@@ -1,3 +1,6 @@\n ctx\n+one\n+two\n+three\n+four\n ctx2\n"

// readyPR makes a ready row with two findings, a diff and no walk.txt.
func readyPR(t *testing.T) (*Server, *store.Store, string, string) {
	t.Helper()
	s, st, _ := prServer(t)
	b := prDecode(t, prDo(s, s.postPR, "POST", "/v1/prs", `{"url":"`+prURL+`","head":"ad5ddf4aaaa"}`, ""))
	id, dir := b.PR["id"].(string), filepath.FromSlash(b.PR["run_dir"].(string))
	os.WriteFile(filepath.Join(dir, "findings", "01-med-tls.go-L5.txt"), []byte(findingText), 0o644)
	os.WriteFile(filepath.Join(dir, "findings", "02-nit-x.go-L9.txt"),
		[]byte("https://x/pull/1\nNIT x.go line 9: y := 1\n\n* naming\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "pr.diff"), []byte(diffText), 0o644)
	st.SetPRState(id, store.PRReady, "", "")
	return s, st, id, dir
}

// realSlash is p resolved and in slashes: a temp dir can be a short 8.3 name (C:\Users\RUNNER~1) that the server
// hands back as the long one.
func realSlash(p string) string {
	if r, err := filepath.EvalSymlinks(filepath.FromSlash(p)); err == nil {
		p = r
	}
	return filepath.ToSlash(p)
}

func drawerDo(s *Server, h http.HandlerFunc, method, body string, vals ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/x", strings.NewReader(body))
	r.SetPathValue("id", vals[0])
	if len(vals) > 1 {
		r.SetPathValue("key", vals[1])
	}
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

type findingsBody struct {
	Findings []PRFinding `json:"findings"`
	Code     string      `json:"code"`
}

func TestFindingsAreParsedInWalkOrder(t *testing.T) {
	s, _, id, _ := readyPR(t)
	w := drawerDo(s, s.getPRFindings, "GET", "", id)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var b findingsBody
	json.Unmarshal(w.Body.Bytes(), &b)
	if len(b.Findings) != 2 {
		t.Fatalf("%s", w.Body)
	}
	f := b.Findings[0]
	if f.Key != "f-3a9c01d4e2" || f.Position != 1 || f.Sev != "med" || f.Path != "src/tls.go" || f.Line != 5 ||
		f.Code != "committed = true" || !strings.HasSuffix(f.Link, "diff-abR5") || f.Leak != "fd, one per call" ||
		f.Evidence["Cause"] != "pre-existing" || f.Walk.State != "open" || f.Hash == "" {
		t.Fatalf("%+v", f)
	}
	if !strings.Contains(f.Comment, "Suggested fix") || strings.Contains(f.Comment, "Evidence") {
		t.Fatalf("comment %q", f.Comment)
	}
	if !strings.HasPrefix(f.Hunk, "@@ -1,3 +1,6 @@") || !strings.Contains(f.Hunk, "+four") {
		t.Fatalf("hunk %q", f.Hunk)
	}
	// No Id: the key comes from path and code.
	if k := b.Findings[1].Key; !strings.HasPrefix(k, "f-") || len(k) != 12 {
		t.Fatalf("derived key %q", k)
	}
}

func TestFindingsETagGives304AndWalkChangesIt(t *testing.T) {
	s, _, id, _ := readyPR(t)
	w := drawerDo(s, s.getPRFindings, "GET", "", id)
	etag := w.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no etag")
	}
	r := httptest.NewRequest("GET", "/x", nil)
	r.SetPathValue("id", id)
	r.Header.Set("If-None-Match", etag)
	w2 := httptest.NewRecorder()
	s.getPRFindings(w2, r)
	if w2.Code != 304 || w2.Body.Len() != 0 {
		t.Fatalf("%d", w2.Code)
	}
	if w := drawerDo(s, s.walkPRFinding, "POST", `{"state":"skipped"}`, id, "f-3a9c01d4e2"); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	r = httptest.NewRequest("GET", "/x", nil)
	r.SetPathValue("id", id)
	r.Header.Set("If-None-Match", etag)
	w3 := httptest.NewRecorder()
	s.getPRFindings(w3, r)
	if w3.Code != 200 {
		t.Fatalf("a walk mark did not change the etag: %d", w3.Code)
	}
}

func TestFindingsNeedAReadyRow(t *testing.T) {
	s, st, id, _ := readyPR(t)
	st.SetPRState(id, store.PRRunning, "panel", "")
	w := drawerDo(s, s.getPRFindings, "GET", "", id)
	if w.Code != 409 || prDecode(t, w).Code != "not_ready" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := drawerDo(s, s.getPRFindings, "GET", "", "nope"); w.Code != 404 {
		t.Fatalf("%d", w.Code)
	}
}

func TestARunFolderOutsideTheRootIsOutside(t *testing.T) {
	s, st, _ := prServer(t)
	other := t.TempDir()
	os.MkdirAll(filepath.Join(other, "findings"), 0o755)
	p, _, _ := st.CreatePR(store.NewPR{Org: "o", Repo: "r", Number: 1, Host: "github.com", RunDir: filepath.ToSlash(other)})
	st.SetPRState(p.ID, store.PRReady, "", "")
	w := drawerDo(s, s.getPRFindings, "GET", "", p.ID)
	if w.Code != 403 || prDecode(t, w).Code != "outside" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestEditingAFindingBehindTheHash(t *testing.T) {
	s, _, id, dir := readyPR(t)
	var b findingsBody
	json.Unmarshal(drawerDo(s, s.getPRFindings, "GET", "", id).Body.Bytes(), &b)
	f := b.Findings[0]

	if w := drawerDo(s, s.putPRFinding, "PUT", `{"text":"x"}`, id, f.Key); w.Code != 400 {
		t.Fatalf("no hash: %d", w.Code)
	}
	if w := drawerDo(s, s.putPRFinding, "PUT", `{"text":"x","hash":"abc"}`, id, "f-nothere"); w.Code != 404 {
		t.Fatalf("unknown key: %d", w.Code)
	}
	w := drawerDo(s, s.putPRFinding, "PUT", `{"text":"x","hash":"stale"}`, id, f.Key)
	var conflict struct{ Code, Text, Hash string }
	json.Unmarshal(w.Body.Bytes(), &conflict)
	if w.Code != 409 || conflict.Code != "changed" || conflict.Hash != f.Hash || !strings.Contains(conflict.Text, "Evidence") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}

	edited := strings.Replace(f.Text, "Id: 3a9c01d4e2", "Id: ffffffffff", 1)
	body, _ := json.Marshal(map[string]string{"text": edited, "hash": f.Hash})
	w = drawerDo(s, s.putPRFinding, "PUT", string(body), id, f.Key)
	var ok struct {
		OK        bool
		Hash, Key string
	}
	json.Unmarshal(w.Body.Bytes(), &ok)
	if w.Code != 200 || !ok.OK || ok.Key != "f-ffffffffff" || ok.Hash == f.Hash {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "findings", f.File)); string(got) != edited {
		t.Fatalf("file not written: %q", got)
	}
}

func TestMarkingTheWalkReplacesOnlyThatLine(t *testing.T) {
	s, _, id, dir := readyPR(t)
	walkFile := filepath.Join(dir, "walk.txt")
	os.WriteFile(walkFile, []byte("02-nit-x.go-L9.txt   deferred  2026-10-01T10:00Z\n"), 0o644)

	w := drawerDo(s, s.walkPRFinding, "POST", `{"state":"done","url":"https://github.com/o/r/pull/1#discussion_r1"}`,
		id, "f-3a9c01d4e2")
	var out struct {
		OK     bool
		Walk   PRFindingWalk
		Counts PRWalkCounts
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != 200 || out.Walk.State != "done" || out.Walk.At == "" || out.Counts.Done != 1 || out.Counts.Deferred != 1 ||
		out.Counts.Open != 0 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	raw, _ := os.ReadFile(walkFile)
	if !strings.Contains(string(raw), "02-nit-x.go-L9.txt   deferred  2026-10-01T10:00Z\n") ||
		!strings.Contains(string(raw), "01-med-tls.go-L5.txt  done  ") || !strings.Contains(string(raw), "discussion_r1") {
		t.Fatalf("walk.txt:\n%s", raw)
	}

	// open clears the time and url, and replaces the same line.
	drawerDo(s, s.walkPRFinding, "POST", `{"state":"open","url":"https://x.y/z"}`, id, "f-3a9c01d4e2")
	raw, _ = os.ReadFile(walkFile)
	if strings.Count(string(raw), "01-med-tls.go-L5.txt") != 1 || strings.Contains(string(raw), "discussion_r1") ||
		!strings.Contains(string(raw), "01-med-tls.go-L5.txt  open\n") {
		t.Fatalf("walk.txt:\n%s", raw)
	}
}

func TestAcceptedIsItsOwnWalkState(t *testing.T) {
	s, _, id, dir := readyPR(t)
	w := drawerDo(s, s.walkPRFinding, "POST", `{"state":"accepted","url":"https://x.y/z"}`, id, "f-3a9c01d4e2")
	var out struct {
		Walk   PRFindingWalk
		Counts PRWalkCounts
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != 200 || out.Walk.State != "accepted" || out.Walk.URL != "" || out.Counts.Accepted != 1 ||
		out.Counts.Done != 0 || out.Counts.Open != 1 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "walk.txt"))
	if !strings.Contains(string(raw), "01-med-tls.go-L5.txt  accepted  ") {
		t.Fatalf("walk.txt:\n%s", raw)
	}
	if _, wc := readPRCounts(filepath.ToSlash(dir)); wc.Accepted != 1 || wc.Open != 1 {
		t.Fatalf("counts %+v", wc)
	}
}

func TestWalkMarkRefusals(t *testing.T) {
	s, _, id, _ := readyPR(t)
	cases := []struct {
		body string
		key  string
		code int
		word string
	}{
		{`{"state":"maybe"}`, "f-3a9c01d4e2", 400, "bad_request"},
		{`nope`, "f-3a9c01d4e2", 400, "bad_request"},
		{`{"state":"done","url":"javascript:alert(1)"}`, "f-3a9c01d4e2", 400, "bad_url"},
		{`{"state":"done","url":"https://a b"}`, "f-3a9c01d4e2", 400, "bad_url"},
		{`{"state":"done","url":"https://x.y/` + strings.Repeat("a", 2000) + `"}`, "f-3a9c01d4e2", 400, "bad_url"},
		{`{"state":"done"}`, "f-missing", 404, "not_found"},
	}
	for _, c := range cases {
		w := drawerDo(s, s.walkPRFinding, "POST", c.body, id, c.key)
		if w.Code != c.code || prDecode(t, w).Code != c.word {
			t.Errorf("%s: %d %s", c.body[:min(len(c.body), 40)], w.Code, w.Body)
		}
	}
}

func TestTheWalkerLaunchSetAndClear(t *testing.T) {
	s, st, id, dir := readyPR(t)
	var sent map[string]any
	s.Launch = func(body []byte) (*store.Task, error) {
		json.Unmarshal(body, &sent)
		return cardIn(t, st, dir), nil
	}

	w := drawerDo(s, s.walkerPR, "POST", ``, id)
	b := prDecode(t, w)
	if w.Code != 201 || b.PR["walker_task"] == "" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if cwd, _ := sent["cwd"].(string); realSlash(cwd) != realSlash(dir) || sent["harness"] != "claude" || !strings.Contains(sent["prompt"].(string), "walk.txt") {
		t.Fatalf("launch body %v", sent)
	}
	tags := strings.Join(toStrings(sent["tags"]), ",")
	if tags != "atrium:subagent,dept:review,review,pr,pr:openziti/tlsuv#378" {
		t.Fatalf("tags %s", tags)
	}

	// A live walker is not launched twice.
	sent = nil
	w = drawerDo(s, s.walkerPR, "POST", `{"action":"launch"}`, id)
	if w.Code != 200 || sent != nil {
		t.Fatalf("second launch: %d %s", w.Code, w.Body)
	}

	if w := drawerDo(s, s.walkerPR, "POST", `{"action":"clear"}`, id); w.Code != 200 || prDecode(t, w).PR["walker_task"] != "" {
		t.Fatalf("clear: %d %s", w.Code, w.Body)
	}
	if w := drawerDo(s, s.walkerPR, "POST", `{"action":"set","task":"abc"}`, id); w.Code != 200 || prDecode(t, w).PR["walker_task"] != "abc" {
		t.Fatalf("set: %d %s", w.Code, w.Body)
	}
	for body, code := range map[string]int{`{"action":"set"}`: 400, `{"action":"dance"}`: 400} {
		if w := drawerDo(s, s.walkerPR, "POST", body, id); w.Code != code {
			t.Errorf("%s: %d", body, w.Code)
		}
	}
}

func TestTheWalkerNeedsAReadyRowAndALauncher(t *testing.T) {
	s, st, id, _ := readyPR(t)
	if w := drawerDo(s, s.walkerPR, "POST", ``, id); w.Code != 501 {
		t.Fatalf("no launcher: %d", w.Code)
	}
	s.Launch = func([]byte) (*store.Task, error) { return nil, os.ErrInvalid }
	if w := drawerDo(s, s.walkerPR, "POST", ``, id); w.Code != 500 || prDecode(t, w).Code != "launch_failed" {
		t.Fatalf("failed launch: %d %s", w.Code, w.Body)
	}
	st.SetPRState(id, store.PRRunning, "panel", "")
	if w := drawerDo(s, s.walkerPR, "POST", ``, id); w.Code != 409 || prDecode(t, w).Code != "not_ready" {
		t.Fatalf("not ready: %d %s", w.Code, w.Body)
	}
}

func toStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}
