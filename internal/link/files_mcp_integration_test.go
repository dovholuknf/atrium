//go:build integration

package link

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dovholuknf/atrium/internal/docshare"
	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/safepath"
)

// dropSeen is one PUT the fake room took.
type dropSeen struct {
	room, card, name, dest, replace string
	body                            string
}

// filesHarness is the four drop tools over a fake room. The room serves the card list, `GET .../files` as
// internal/api does, `PUT .../files/drop`, `GET /v1/usage/export` and `GET /v1/tasks`, and answers for any room name.
type filesHarness struct {
	*publishHarness
	mu        sync.Mutex
	drops     []dropSeen
	olderDrop bool
	older     map[string]bool
	audits    []string
}

func newFilesHarness(t *testing.T) *filesHarness {
	t.Helper()
	work := t.TempDir()
	st, err := hubstore.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ph := &publishHarness{t: t, st: st, work: work}
	h := &filesHarness{publishHarness: ph, older: map[string]bool{}}
	board := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		room := r.Header.Get(RoomHeader)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/tasks":
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []map[string]any{
				{"id": "card1", "wire_name": "r-031", "status": "working", "worktree": work},
				{"id": "card2", "wire_name": "r-032", "status": "working", "worktree": work},
			}})
		case r.URL.Path == "/v1/usage/export":
			if h.older[room] {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/x-ndjson")
			_, _ = io.WriteString(w, `{"room":"`+room+`","since":"`+r.URL.Query().Get("since")+`"}`+"\n")
		case strings.HasPrefix(r.URL.Path, "/v1/tasks/card") && strings.HasSuffix(r.URL.Path, "/files/drop"):
			if h.olderDrop {
				http.NotFound(w, r)
				return
			}
			b, _ := io.ReadAll(r.Body)
			q := r.URL.Query()
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/tasks/"), "/files/drop")
			h.mu.Lock()
			h.drops = append(h.drops, dropSeen{room: room, card: id, name: q.Get("name"), dest: q.Get("dest"),
				replace: q.Get("replace"), body: string(b)})
			h.mu.Unlock()
			if strings.Contains(q.Get("dest"), "taken") && q.Get("replace") != "1" {
				w.WriteHeader(409)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "a file is already there, pass replace"})
				return
			}
			rel := ".atrium/incoming/20261012-120000-" + q.Get("name")
			if q.Get("dest") != "" {
				rel = q.Get("dest")
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"path": filepath.ToSlash(work) + "/" + rel, "rel": rel})
		case strings.HasPrefix(r.URL.Path, "/v1/tasks/card") && strings.HasSuffix(r.URL.Path, "/files"):
			want := r.URL.Query().Get("path")
			h.mu.Lock()
			ph.asked = append(ph.asked, want)
			h.mu.Unlock()
			real, err := safepath.Contained(work, want)
			if err != nil {
				w.WriteHeader(403)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": safepath.ErrOutside.Error()})
				return
			}
			b, err := os.ReadFile(real)
			if err != nil {
				w.WriteHeader(404)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "no such file"})
				return
			}
			root, _ := filepath.EvalSymlinks(work)
			rel, _ := filepath.Rel(root, real)
			w.Header().Set(realPathHeader, url.PathEscape(filepath.ToSlash(rel)))
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(b)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(board.Close)
	h.c = &controlMCP{board: board.URL, client: board.Client(), docs: func() *hubstore.Store { return st },
		rooms: func() []resourceRoom {
			return []resourceRoom{{Name: "sg4", Online: true}, {Name: "m1mini", Online: true},
				{Name: "sg3", Online: false}, {Name: "old", Online: true}}
		},
		audit: func(room, kind, detail string) {
			h.mu.Lock()
			h.audits = append(h.audits, kind+" "+detail)
			h.mu.Unlock()
		}}
	return h
}

func (h *filesHarness) put(agent string, in putInput) (putOutput, error) {
	_, out, err := h.c.putHandler(context.Background(), ctlReq(agent, "sg4"), in)
	return out, err
}

func (h *filesHarness) get(agent string, in getInput) (getOutput, error) {
	_, out, err := h.c.getHandler(context.Background(), ctlReq(agent, "sg4"), in)
	return out, err
}

func (h *filesHarness) export(in exportInput) (exportOutput, error) {
	_, out, err := h.c.exportHandler(context.Background(), ctlReq("r-031", "sg4"), in)
	return out, err
}

func (h *filesHarness) list(agent string, mine bool) filesOutput {
	h.t.Helper()
	_, out, err := h.c.filesHandler(context.Background(), ctlReq(agent, "sg4"), filesInput{Mine: mine})
	if err != nil {
		h.t.Fatal(err)
	}
	return out
}

func TestFilesToolPutByContentAndList(t *testing.T) {
	h := newFilesHarness(t)
	out, err := h.put("r-031", putInput{Content: "a,b\n1,2\n", Name: "table.csv", Note: "for sg4"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Name != "table.csv" || out.Size != 8 || out.Expires == "" {
		t.Fatalf("answer %+v", out)
	}
	all := h.list("r-032", false)
	if len(all.Files) != 1 {
		t.Fatalf("list %+v", all)
	}
	f := all.Files[0]
	if f.Origin != "card" || f.Card != "sg4~card1" || f.By != "r-031@sg4" || f.Room != "sg4" || f.Note != "for sg4" {
		t.Fatalf("row %+v", f)
	}
	if all.Usage.Bytes != 8 || all.Usage.Cap != hubstore.DefaultFileTotalCap {
		t.Fatalf("usage %+v", all.Usage)
	}
	if mine := h.list("r-031", true); len(mine.Files) != 1 {
		t.Fatalf("mine for the putter %+v", mine)
	}
	if mine := h.list("r-032", true); len(mine.Files) != 0 {
		t.Fatalf("mine for another card %+v", mine)
	}
	// Content needs a name, and so does giving neither or both.
	for _, in := range []putInput{{Content: "x"}, {}, {Content: "x", Path: "y", Name: "n"}} {
		if _, err := h.put("r-031", in); refusalOf(t, err).Status != 400 {
			t.Fatalf("%+v: %v", in, err)
		}
	}
	if _, err := h.put("", putInput{Content: "x", Name: "n"}); err == nil {
		t.Fatal("a put with no agent was accepted")
	}
}

func TestFilesToolPutByPathReadsThroughTheRoom(t *testing.T) {
	h := newFilesHarness(t)
	h.write("out/usage.jsonl", "{}\n")
	out, err := h.put("r-031", putInput{Path: "out/usage.jsonl", TTLDays: 3})
	if err != nil {
		t.Fatal(err)
	}
	if out.Name != "usage.jsonl" || out.Size != 3 {
		t.Fatalf("answer %+v", out)
	}
	if len(h.asked) != 1 || h.asked[0] != "out/usage.jsonl" {
		t.Fatalf("the room was asked %v", h.asked)
	}
	// A second put of the same name gets a suffix, and says so.
	again, err := h.put("r-031", putInput{Path: "out/usage.jsonl"})
	if err != nil || again.Name != "usage-2.jsonl" {
		t.Fatalf("second put %+v, %v", again, err)
	}
	named, err := h.put("r-031", putInput{Path: "out/usage.jsonl", Name: "mine.jsonl"})
	if err != nil || named.Name != "mine.jsonl" {
		t.Fatalf("named put %+v, %v", named, err)
	}
}

func TestFilesToolPutRefusesASecretNameOnTheResolvedTarget(t *testing.T) {
	h := newFilesHarness(t)
	h.write(".env", "just a harmless line")
	h.write("fine.txt", "fine")
	if err := os.Symlink(".env", filepath.Join(h.work, "notes.md")); err != nil {
		t.Skip("no symlinks here:", err)
	}
	for _, p := range []string{".env", ".ENV", "notes.md"} {
		_, err := h.put("r-031", putInput{Path: p})
		if r := refusalOf(t, err); r.Status != 422 || r.Rule != docshare.RuleFileName {
			t.Errorf("%s: %+v", p, r)
		}
	}
	// And by name with content, and a secret in the content.
	_, err := h.put("r-031", putInput{Content: "x", Name: "server.pem"})
	if r := refusalOf(t, err); r.Status != 422 {
		t.Errorf("content under a secret name: %+v", r)
	}
	_, err = h.put("r-031", putInput{Content: "ghp_" + strings.Repeat("a", 36), Name: "ok.txt"})
	if r := refusalOf(t, err); r.Status != 422 {
		t.Errorf("secret content: %+v", r)
	}
	if got := h.list("r-031", false); len(got.Files) != 0 {
		t.Fatalf("a refused put stored %+v", got.Files)
	}
	if _, err := h.put("r-031", putInput{Path: "fine.txt"}); err != nil {
		t.Fatalf("an ordinary file was refused: %v", err)
	}
}

func TestFilesToolPutOutsideTheCardIs403(t *testing.T) {
	h := newFilesHarness(t)
	outside := t.TempDir()
	secret := filepath.Join(outside, "data.txt")
	if err := os.WriteFile(secret, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{secret, "../" + filepath.Base(outside) + "/data.txt", "../../etc/passwd"} {
		_, err := h.put("r-031", putInput{Path: p})
		if r := refusalOf(t, err); r.Status != 403 {
			t.Errorf("%s: status %d (%v)", p, r.Status, err)
		}
	}
	if got := h.list("r-031", false); len(got.Files) != 0 {
		t.Fatalf("a refused put stored %+v", got.Files)
	}
}

// A GET IS SENT TO THE CALLER'S OWN CARD. Nothing in the input names a card, so one card cannot write into another.
func TestFilesToolGetSendsToTheCallersOwnCard(t *testing.T) {
	h := newFilesHarness(t)
	if _, err := h.put("r-031", putInput{Content: "payload", Name: "p.txt"}); err != nil {
		t.Fatal(err)
	}
	out, err := h.get("r-032", getInput{Name: "p.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(h.drops) != 1 {
		t.Fatalf("the room saw %d drops", len(h.drops))
	}
	d := h.drops[0]
	if d.card != "card2" || d.name != "p.txt" || d.body != "payload" || d.room != "sg4" {
		t.Fatalf("the room saw %+v", d)
	}
	if out.Size != 7 || !strings.HasSuffix(out.Rel, "-p.txt") || out.Path == "" {
		t.Fatalf("answer %+v", out)
	}
	if _, ok := any(getInput{}).(interface{ Card() string }); ok {
		t.Fatal("the input can name a card")
	}
	if f := h.list("r-031", false).Files[0]; f.Downloads != 1 {
		t.Fatalf("downloads %d", f.Downloads)
	}
}

func TestFilesToolGetPassesDestAndReplace(t *testing.T) {
	h := newFilesHarness(t)
	if _, err := h.put("r-031", putInput{Content: "payload", Name: "p.txt"}); err != nil {
		t.Fatal(err)
	}
	out, err := h.get("r-032", getInput{Name: "p.txt", Dest: "out/taken.txt"})
	if r := refusalOf(t, err); r.Status != 409 || !strings.Contains(r.Msg, "replace") {
		t.Fatalf("a taken dest: %+v %v", r, out)
	}
	out, err = h.get("r-032", getInput{Name: "p.txt", Dest: "out/taken.txt", Replace: true})
	if err != nil || out.Rel != "out/taken.txt" {
		t.Fatalf("replace: %+v, %v", out, err)
	}
	last := h.drops[len(h.drops)-1]
	if last.dest != "out/taken.txt" || last.replace != "1" {
		t.Fatalf("the room saw %+v", last)
	}
	// A secret-looking dest is refused here, before the room is asked.
	n := len(h.drops)
	_, err = h.get("r-032", getInput{Name: "p.txt", Dest: "conf/.env"})
	if r := refusalOf(t, err); r.Status != 422 {
		t.Fatalf("%+v", r)
	}
	if len(h.drops) != n {
		t.Fatal("the room was asked about a secret dest")
	}
}

func TestFilesToolGetFromAnOlderRoomSaysSo(t *testing.T) {
	h := newFilesHarness(t)
	if _, err := h.put("r-031", putInput{Content: "payload", Name: "p.txt"}); err != nil {
		t.Fatal(err)
	}
	h.olderDrop = true
	_, err := h.get("r-032", getInput{Name: "p.txt"})
	if r := refusalOf(t, err); !strings.Contains(r.Msg, "older than the file drop") {
		t.Fatalf("%+v", r)
	}
}

// Expiry itself is hubstore's, in TestFilesExpireAndSweep. Here: a name that is not there is 404, and bytes that are
// gone are 410, the words documents use.
func TestFilesToolGetOfAMissingOrGoneFileIs404Or410(t *testing.T) {
	h := newFilesHarness(t)
	_, err := h.get("r-032", getInput{Name: "nothing.txt"})
	if r := refusalOf(t, err); r.Status != 404 {
		t.Fatalf("%+v", r)
	}
	out, err := h.put("r-031", putInput{Content: "payload", Name: "p.txt"})
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := h.st.FileList("")
	if err := os.Remove(filepath.Join(h.st.FilesDir(), rows[0].SHA)); err != nil {
		t.Fatal(err)
	}
	_, err = h.get("r-032", getInput{Name: out.Name})
	if r := refusalOf(t, err); r.Status != 410 || r.Msg != "the bytes are missing" {
		t.Fatalf("%+v", r)
	}
	if len(h.drops) != 0 {
		t.Fatal("the room was asked for a file with no bytes")
	}
}

func TestFilesToolExportStoresOneFilePerRoom(t *testing.T) {
	h := newFilesHarness(t)
	h.older["old"] = true
	out, err := h.export(exportInput{What: "usage", Since: "2026-10-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Files) != 2 {
		t.Fatalf("files %+v", out.Files)
	}
	got := map[string]string{}
	for _, f := range out.Files {
		got[f.Room] = f.Name
		if !strings.HasPrefix(f.Name, "usage-"+f.Room+"-") || !strings.HasSuffix(f.Name, ".jsonl") {
			t.Fatalf("name %q", f.Name)
		}
	}
	if got["sg4"] == "" || got["m1mini"] == "" {
		t.Fatalf("files %+v", out.Files)
	}
	skipped := map[string]string{}
	for _, s := range out.Skipped {
		skipped[s.Room] = s.Why
	}
	if len(skipped) != 1 || !strings.Contains(skipped["old"], "older than the file drop") {
		t.Fatalf("skipped %+v", out.Skipped)
	}
	f, row, err := h.st.FileOpen(got["sg4"])
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(f)
	f.Close()
	if !strings.Contains(string(b), `"room":"sg4"`) || !strings.Contains(string(b), "2026-10-01T00:00:00Z") {
		t.Fatalf("bytes %q", b)
	}
	if row.Origin != "card" || row.Card != "sg4~card1" || row.Room != "sg4" || !strings.HasPrefix(row.Note, "export of usage from sg4 since") {
		t.Fatalf("row %+v", row)
	}

	// A room that is named and offline is skipped, and the others still land.
	out, err = h.export(exportInput{What: "usage", Rooms: []string{"sg3", "sg4"}})
	if err != nil || len(out.Files) != 1 || len(out.Skipped) != 1 || !strings.Contains(out.Skipped[0].Why, "offline") {
		t.Fatalf("%+v, %v", out, err)
	}

	// Named rooms only, and an unknown one is skipped. Tasks are stored as they came.
	out, err = h.export(exportInput{What: "tasks", Rooms: []string{"m1mini", "nowhere"}})
	if err != nil || len(out.Files) != 1 || out.Files[0].Room != "m1mini" ||
		!strings.HasSuffix(out.Files[0].Name, ".json") || len(out.Skipped) != 1 {
		t.Fatalf("%+v, %v", out, err)
	}
	// Exports are not counted against a card's hourly puts.
	for i := 0; i < 3; i++ {
		if _, err := h.export(exportInput{What: "usage", Rooms: []string{"sg4"}}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFilesToolExportBadWhatIs400(t *testing.T) {
	h := newFilesHarness(t)
	for _, in := range []exportInput{{What: "sqlite"}, {What: ""}, {What: "usage", Since: "yesterday"}} {
		_, err := h.export(in)
		if r := refusalOf(t, err); r.Status != 400 {
			t.Fatalf("%+v: %+v", in, r)
		}
	}
}

func TestFilesToolsAreAudited(t *testing.T) {
	h := newFilesHarness(t)
	ctx := context.Background()
	run := func() {
		// Through the same wrapper the server registers, which is what writes the line.
		_, _, _ = audited(h.c, "ctl-put", describePut, h.c.putHandler)(ctx, ctlReq("r-031", "sg4"),
			putInput{Content: "x", Name: "a.txt"})
		_, _, _ = audited(h.c, "ctl-put", describePut, h.c.putHandler)(ctx, ctlReq("r-031", "sg4"),
			putInput{Content: "x", Name: ".env"})
		_, _, _ = audited(h.c, "ctl-get", describeGet, h.c.getHandler)(ctx, ctlReq("r-032", "sg4"), getInput{Name: "a.txt"})
		_, _, _ = audited(h.c, "ctl-export", describeExport, h.c.exportHandler)(ctx, ctlReq("r-031", "sg4"),
			exportInput{What: "tasks", Rooms: []string{"sg4"}})
	}
	run()
	joined := strings.Join(h.audits, "\n")
	for _, want := range []string{"ctl-put by r-031@sg4 (claimed): put a.txt, ok", "ctl-get by r-032@sg4 (claimed): get a.txt",
		"ctl-export by r-031@sg4 (claimed): export tasks, 1 stored, 0 skipped"} {
		if !strings.Contains(joined, want) {
			t.Errorf("no audit line %q in\n%s", want, joined)
		}
	}
	// The refused put wrote a line too, and says it was refused.
	refused := 0
	for _, a := range h.audits {
		if strings.HasPrefix(a, "ctl-put") && strings.Contains(a, ".env") {
			refused++
		}
	}
	if refused != 1 {
		t.Errorf("the refused put wrote %d lines\n%s", refused, joined)
	}
}

func TestFilesToolPutContentOverTheCapIs413(t *testing.T) {
	h := newFilesHarness(t)
	if _, err := h.st.SetFileSettings(hubstore.FileCaps{File: 10}); err != nil {
		t.Fatal(err)
	}
	_, err := h.put("r-031", putInput{Content: "01234567890", Name: "big.txt"})
	if refusalOf(t, err).Status != 413 {
		t.Fatalf("err %v", err)
	}
}
