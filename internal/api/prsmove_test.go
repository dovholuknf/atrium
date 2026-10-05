package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

const prKey = "github.com/openziti/tlsuv/378"

const walkBody = "01-high-leak.txt accepted 2026-10-04T01:00:00Z\n02-nit-style.txt open 2026-10-04T01:00:00Z\n"

// movedRoom is a room with a review of prURL in a finished state, with findings, a walk and a step on disk.
func movedRoom(t *testing.T) (*Server, *store.Store, *store.PRReview) {
	t.Helper()
	s, st, _ := prServer(t)
	s.PRRunner = &fakePRRunner{st: st}
	w := prDo(s, s.postPR, "POST", "/v1/prs", `{"url":"`+prURL+`","why":"security","head":"abcdef0123456789"}`, "")
	if w.Code != http.StatusCreated {
		t.Fatalf("post: %d %s", w.Code, w.Body)
	}
	p, err := st.PRByKey("github.com", "openziti", "tlsuv", 378)
	if err != nil || p == nil {
		t.Fatalf("row: %v %v", p, err)
	}
	dir := filepath.FromSlash(p.RunDir)
	for name, body := range map[string]string{
		"findings/01-high-leak.txt":       "Leak: yes\nthe finding\n",
		"findings/02-nit-style.txt":       "a nit\n",
		"walk.txt":                        walkBody,
		"pr.diff":                         "diff --git a b\n",
		"review.json":                     `{"steps":{}}`,
		"steps/panel/out.json":            `{"findings":[]}`,
		"src/huge/checkout-file-not-sent": "x",
	} {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.SetPRFetched(p.ID, "abcdef0123456789", "the title", "someone", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetPRWalker(p.ID, "old-card"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddPRCost(p.ID, 1.25); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetPRSecond(p.ID, "done", "sum", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetPRState(p.ID, store.PRReady, "", ""); err != nil {
		t.Fatal(err)
	}
	p, _ = st.PRByID(p.ID)
	return s, st, p
}

func exportOf(t *testing.T, s *Server, key string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	s.exportPR(w, httptest.NewRequest("GET", "/v1/prs/export?key="+key, nil))
	return w
}

func importTo(s *Server, body []byte) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.importPR(w, httptest.NewRequest("POST", "/v1/prs/import", bytes.NewReader(body)))
	return w
}

func TestAReviewExportsAndImportsWithItsFilesAndFields(t *testing.T) {
	a, _, was := movedRoom(t)
	w := exportOf(t, a, prKey)
	if w.Code != 200 || w.Header().Get(PRHeaderID) != was.ID {
		t.Fatalf("export: %d %s id %q", w.Code, w.Body.String(), w.Header().Get(PRHeaderID))
	}

	b, bst, _ := prServer(t)
	brun := &fakePRRunner{st: bst}
	b.PRRunner = brun
	got := importTo(b, w.Body.Bytes())
	if got.Code != http.StatusCreated {
		t.Fatalf("import: %d %s", got.Code, got.Body)
	}
	row, err := bst.PRByKey("github.com", "openziti", "tlsuv", 378)
	if err != nil || row == nil {
		t.Fatalf("no row: %v", err)
	}
	if row.ID == was.ID {
		t.Fatalf("the row kept its id")
	}
	if row.State != store.PRReady || row.Why != "security" || row.Head != "abcdef0123456789" || row.Title != "the title" ||
		row.Author != "someone" || row.CostUSD != 1.25 || row.Second.State != "done" || row.CreatedAt != was.CreatedAt ||
		row.ReadyAt != was.ReadyAt || row.StartedAt != was.StartedAt || row.URL != prURL {
		t.Fatalf("fields: %+v want %+v", row, was)
	}
	if row.WalkerTask != "" || row.Claim != "claimed" {
		t.Fatalf("walker %q claim %q", row.WalkerTask, row.Claim)
	}
	if len(brun.started) != 0 {
		t.Fatalf("a ready row was started: %v", brun.started)
	}
	want := filepath.ToSlash(filepath.Join(bst.ReviewsRoot(), "github-openziti-tlsuv", "pr-378-abcdef0"))
	if row.RunDir != want {
		t.Fatalf("run dir %q want %q", row.RunDir, want)
	}
	for name, body := range map[string]string{
		"findings/01-high-leak.txt": "Leak: yes\nthe finding\n",
		"walk.txt":                  walkBody,
		"pr.diff":                   "diff --git a b\n",
		"steps/panel/out.json":      `{"findings":[]}`,
	} {
		b, err := os.ReadFile(filepath.Join(filepath.FromSlash(row.RunDir), filepath.FromSlash(name)))
		if err != nil || string(b) != body {
			t.Fatalf("%s = %q, %v", name, b, err)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.FromSlash(row.RunDir), "src")); err == nil {
		t.Fatalf("the checkout was sent")
	}
}

func TestImportingAKeyAlreadyLiveAnswersThatRowAndWritesNothing(t *testing.T) {
	a, _, _ := movedRoom(t)
	arc := exportOf(t, a, prKey).Body.Bytes()
	b, bst, _ := prServer(t)
	b.PRRunner = &fakePRRunner{st: bst}
	first := importTo(b, arc)
	if first.Code != 201 {
		t.Fatalf("first: %d %s", first.Code, first.Body)
	}
	row, _ := bst.PRByKey("github.com", "openziti", "tlsuv", 378)
	extra := filepath.Join(filepath.FromSlash(row.RunDir), "mine.txt")
	if err := os.WriteFile(extra, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	again := importTo(b, arc)
	if again.Code != 200 || prDecode(t, again).Created {
		t.Fatalf("again: %d %s", again.Code, again.Body)
	}
	rows, _ := bst.PRs(store.PRFilter{Archived: true})
	if len(rows) != 1 {
		t.Fatalf("rows %d", len(rows))
	}
	if _, err := os.Stat(extra); err != nil {
		t.Fatalf("the folder was touched: %v", err)
	}
}

// hand makes an archive by hand, to try the refusals.
func hand(entries ...tar.Header) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	row := `{"version":1,"row":{"url":"` + prURL + `","host":"github.com","org":"openziti","repo":"tlsuv","number":378,` +
		`"reviewed_head":"abcdef0123456789","state":"ready","second_state":"none"}}`
	_ = tw.WriteHeader(&tar.Header{Name: "row.json", Mode: 0o644, Size: int64(len(row)), Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte(row))
	for _, h := range entries {
		h := h
		if h.Typeflag == tar.TypeReg && h.Size == 0 {
			h.Size = 1
		}
		_ = tw.WriteHeader(&h)
		if h.Typeflag == tar.TypeReg {
			_, _ = tw.Write(bytes.Repeat([]byte("x"), int(h.Size)))
		}
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func TestAnImportRefusesWhatLeavesTheFolderALinkOrADuplicate(t *testing.T) {
	reg := func(name string) tar.Header { return tar.Header{Name: name, Mode: 0o644, Typeflag: tar.TypeReg} }
	cases := map[string][]tar.Header{
		"dotdot":    {reg("files/../escape.txt")},
		"deep":      {reg("files/a/../../escape.txt")},
		"absolute":  {reg("files//etc/passwd")},
		"backslash": {reg(`files/..\escape.txt`)},
		"drive":     {reg("files/C:/x.txt")},
		"outside":   {reg("escape.txt")},
		"symlink":   {{Name: "files/link", Typeflag: tar.TypeSymlink, Linkname: "/etc"}},
		"hardlink":  {{Name: "files/link", Typeflag: tar.TypeLink, Linkname: "files/x"}},
		"twice":     {reg("files/a.txt"), reg("files/a.txt")},
	}
	for name, ents := range cases {
		t.Run(name, func(t *testing.T) {
			s, st, root := prServer(t)
			s.PRRunner = &fakePRRunner{st: st}
			w := importTo(s, hand(ents...))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
			if rows, _ := st.PRs(store.PRFilter{Archived: true}); len(rows) != 0 {
				t.Fatalf("a row was made")
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(root), "escape.txt")); err == nil {
				t.Fatalf("escaped")
			}
			var left []string
			_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
				if err == nil && p != root {
					left = append(left, p)
				}
				return nil
			})
			// The top folder of the repo may stay. Nothing under it may.
			for _, p := range left {
				if strings.Contains(filepath.ToSlash(p), "/pr-378-") {
					t.Fatalf("left behind: %v", left)
				}
			}
		})
	}
	t.Run("not gzip", func(t *testing.T) {
		s, _, _ := prServer(t)
		if w := importTo(s, []byte("nope")); w.Code != 400 {
			t.Fatalf("%d", w.Code)
		}
	})
}

func TestAnImportOverTheCapIsRefused(t *testing.T) {
	was := PRArchiveCap
	PRArchiveCap = 1 << 10
	defer func() { PRArchiveCap = was }()
	s, st, _ := prServer(t)
	s.PRRunner = &fakePRRunner{st: st}
	// Zeros compress to nothing, so the entry is over the cap unpacked and under it as sent.
	w := importTo(s, hand(tar.Header{Name: "files/big.bin", Mode: 0o644, Typeflag: tar.TypeReg, Size: 4 << 10}))
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if rows, _ := st.PRs(store.PRFilter{Archived: true}); len(rows) != 0 {
		t.Fatalf("a row was made")
	}
}

func TestAnExportOverTheCapIsRefused(t *testing.T) {
	a, _, _ := movedRoom(t)
	was := PRArchiveCap
	PRArchiveCap = 10
	defer func() { PRArchiveCap = was }()
	if w := exportOf(t, a, prKey); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestAnExportLeavesOutALink(t *testing.T) {
	a, _, p := movedRoom(t)
	dir := filepath.FromSlash(p.RunDir)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "findings", "03-low-link.txt")); err != nil {
		t.Skip("no symlinks here: ", err)
	}
	w := exportOf(t, a, prKey)
	if w.Code != 200 {
		t.Fatalf("%d", w.Code)
	}
	gz, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err != nil {
			break
		}
		if strings.Contains(h.Name, "link") {
			t.Fatalf("the link is in the archive: %s", h.Name)
		}
		if h.Typeflag != tar.TypeReg {
			t.Fatalf("%s is not a file", h.Name)
		}
		b, _ := io.ReadAll(tr)
		if strings.Contains(string(b), "secret") {
			t.Fatalf("the link target was read")
		}
	}
}

func TestAnExportOfNothingIs404(t *testing.T) {
	s, _, _ := prServer(t)
	if w := exportOf(t, s, prKey); w.Code != 404 {
		t.Fatalf("%d", w.Code)
	}
	if w := exportOf(t, s, "not-a-key"); w.Code != 400 {
		t.Fatalf("%d", w.Code)
	}
}

func TestARunningRowArrivesQueuedAndIsStarted(t *testing.T) {
	a, ast, p := movedRoom(t)
	// Put the row back to running, as a run in flight is.
	if _, err := ast.SetPRState(p.ID, store.PRRunning, "panel", ""); err != nil {
		t.Fatal(err)
	}
	arc := exportOf(t, a, prKey).Body.Bytes()
	b, bst, _ := prServer(t)
	// The runner that starts it leaves it running, as the real one does for a while.
	brun := &fakePRRunner{st: bst}
	b.PRRunner = brun
	w := importTo(b, arc)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	row, _ := bst.PRByKey("github.com", "openziti", "tlsuv", 378)
	if len(brun.started) != 1 || brun.started[0] != row.ID {
		t.Fatalf("started %v", brun.started)
	}
	if row.State != store.PRRunning {
		t.Fatalf("state %s", row.State)
	}
	// Its steps are there for the runner to resume from.
	if _, err := os.Stat(filepath.Join(filepath.FromSlash(row.RunDir), "steps", "panel", "out.json")); err != nil {
		t.Fatal(err)
	}
	// And with a runner that does not move it, it is queued as it arrived.
	c, cst, _ := prServer(t)
	c.PRRunner = noStart{}
	importTo(c, arc)
	crow, _ := cst.PRByKey("github.com", "openziti", "tlsuv", 378)
	if crow.State != store.PRQueued || crow.RunState != "" || crow.StartedAt != "" {
		t.Fatalf("arrived %+v", crow)
	}
}

type noStart struct{}

func (noStart) Start(string) {}
func (noStart) Abort(string) {}

func TestArchivingStopsARunAndKeepsTheFolder(t *testing.T) {
	a, ast, p := movedRoom(t)
	run := &fakePRRunner{st: ast}
	a.PRRunner = run
	if _, err := ast.SetPRState(p.ID, store.PRRunning, "panel", ""); err != nil {
		t.Fatal(err)
	}
	w := prDo(a, a.archivePR, "POST", "/v1/prs/"+p.ID+"/archive", "", p.ID)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if len(run.aborted) != 1 {
		t.Fatalf("aborted %v", run.aborted)
	}
	got, _ := ast.PRByID(p.ID)
	if got.Archived == "" || got.State != store.PRAborted {
		t.Fatalf("row %+v", got)
	}
	if _, err := os.Stat(filepath.Join(filepath.FromSlash(p.RunDir), "findings", "01-high-leak.txt")); err != nil {
		t.Fatalf("folder: %v", err)
	}
	if live, _ := ast.PRByKey("github.com", "openziti", "tlsuv", 378); live != nil {
		t.Fatalf("still live")
	}
	// A second archive is the same answer and stops nothing more.
	if w := prDo(a, a.archivePR, "POST", "/x", "", p.ID); w.Code != 200 || len(run.aborted) != 1 {
		t.Fatalf("again: %d %v", w.Code, run.aborted)
	}
	if w := prDo(a, a.archivePR, "POST", "/x", "", "pr_nope"); w.Code != 404 {
		t.Fatalf("%d", w.Code)
	}
}

func TestAReviewComingBackToARoomItLeftGetsAFreshFolder(t *testing.T) {
	a, ast, p := movedRoom(t)
	arc := exportOf(t, a, prKey).Body.Bytes()
	prDo(a, a.archivePR, "POST", "/x", "", p.ID)
	w := importTo(a, arc)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	row, _ := ast.PRByKey("github.com", "openziti", "tlsuv", 378)
	if row.ID == p.ID || row.RunDir == p.RunDir || !strings.Contains(row.RunDir, "-moved-1") {
		t.Fatalf("run dir %q, old %q", row.RunDir, p.RunDir)
	}
}
