//go:build integration

package link

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/safepath"
)

// publishHarness is atrium_publish over a fake room. The room serves the card list and
// `GET /v1/tasks/{id}/files` the way internal/api does: safepath.Contained, 403 outside, and the
// resolved path in X-Atrium-Real-Path.
type publishHarness struct {
	t        *testing.T
	st       *hubstore.Store
	c        *controlMCP
	work     string
	noHeader bool
	asked    []string
}

func newPublishHarness(t *testing.T) *publishHarness {
	t.Helper()
	work := t.TempDir()
	st, err := hubstore.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	h := &publishHarness{t: t, st: st, work: work}
	board := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/tasks":
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []map[string]any{
				{"id": "card1", "wire_name": "r-031", "status": "working", "worktree": work,
					"tags": []string{"atrium:subagent"}},
				{"id": "card2", "wire_name": "r-032", "status": "working", "worktree": work},
			}})
		case strings.HasPrefix(r.URL.Path, "/v1/tasks/card") && strings.HasSuffix(r.URL.Path, "/files"):
			want := r.URL.Query().Get("path")
			h.asked = append(h.asked, want)
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
			if !h.noHeader {
				root, _ := filepath.EvalSymlinks(work)
				rel, _ := filepath.Rel(root, real)
				w.Header().Set(realPathHeader, url.PathEscape(filepath.ToSlash(rel)))
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(b)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(board.Close)
	h.c = &controlMCP{board: board.URL, client: board.Client(), docs: func() *hubstore.Store { return st }}
	return h
}

func (h *publishHarness) write(name, body string) {
	h.t.Helper()
	p := filepath.Join(h.work, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		h.t.Fatal(err)
	}
}

func (h *publishHarness) publish(in publishInput) (publishOutput, error) {
	_, out, err := h.c.publishHandler(context.Background(), ctlReq("r-031", "sg4"), in)
	return out, err
}

func refusalOf(t *testing.T, err error) *docsRefusal {
	t.Helper()
	var r *docsRefusal
	if !errors.As(err, &r) {
		t.Fatalf("wanted a docsRefusal, got %v", err)
	}
	return r
}

func (h *publishHarness) none() {
	h.t.Helper()
	if l, _ := h.st.DocList("", false, ""); len(l) != 0 {
		h.t.Fatalf("a refused publish stored %d documents", len(l))
	}
}

// A PUBLISH RECORDS origin card, THE CARD IN room~id FORM, AND THE CARD'S HANDLE.
func TestPublishRecordsTheCardAsOrigin(t *testing.T) {
	h := newPublishHarness(t)
	out, err := h.publish(publishInput{Title: "Usage 2026-09-29", Content: "# usage\n"})
	if err != nil {
		t.Fatal(err)
	}
	if out.URL != "/d/usage-2026-09-29" || out.Slug != "usage-2026-09-29" || out.Version != 1 {
		t.Fatalf("answer %+v", out)
	}
	d, _ := h.st.DocGet(out.Slug)
	v := d.Versions[0]
	if v.Origin != "card" || v.Card != "sg4~card1" || v.By != "r-031@sg4" || v.Kind != "markdown" {
		t.Fatalf("version %+v", v)
	}
	// And it is in that card's own list, which is what @ui's "published N documents" reads.
	if l, _ := h.st.DocList("", false, "sg4~card1"); len(l) != 1 {
		t.Fatalf("card list %+v", l)
	}
	if l, _ := h.st.DocList("", false, "sg4~card2"); len(l) != 0 {
		t.Fatalf("another card's list %+v", l)
	}
}

// A PATH IS READ ON THE CARD'S ROOM, and the hub reads no disk itself.
func TestPublishReadsAPathThroughTheRoom(t *testing.T) {
	h := newPublishHarness(t)
	h.write("notes/report.md", "# the report\n")
	out, err := h.publish(publishInput{Title: "Report", Path: "notes/report.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(h.asked) != 1 || h.asked[0] != "notes/report.md" {
		t.Fatalf("the room was asked %v", h.asked)
	}
	d, _ := h.st.DocGet(out.Slug)
	if v := d.Versions[0]; v.Name != "report.md" || v.Origin != "card" || v.Size != int64(len("# the report\n")) {
		t.Fatalf("version %+v", v)
	}
}

// A PUBLISH FROM A PATH OUTSIDE THE CARD IS 403, the same answer as the download endpoint.
func TestPublishOutsideTheCardIs403(t *testing.T) {
	h := newPublishHarness(t)
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(h.work, "innocent.md")); err != nil {
		t.Skip("no symlinks here:", err)
	}
	for _, p := range []string{secret, "../" + filepath.Base(outside) + "/secret.txt", "../../etc/passwd", "innocent.md"} {
		_, err := h.publish(publishInput{Title: "x", Path: p})
		if r := refusalOf(t, err); r.Status != 403 {
			t.Errorf("%s: status %d (%v)", p, r.Status, err)
		}
	}
	h.none()
}

// A SECRET FILE NAME IS REFUSED ON THE RESOLVED TARGET, case-insensitively, INCLUDING A LINK
// `notes.md -> .env` AND `.ENV`.
func TestPublishRefusesASecretFileNameOnTheResolvedTarget(t *testing.T) {
	h := newPublishHarness(t)
	h.write(".env", "just a harmless line")
	h.write(".ENV", "another harmless line")
	h.write("deploy/server.PEM", "harmless")
	h.write("id_rsa", "harmless")
	h.write(".git/config", "harmless")
	h.write(".ssh/known_hosts", "harmless")
	h.write("sub/Credentials.json", "harmless")
	h.write(".kube/config", "harmless")
	h.write(".zrok/environment.json", "harmless")
	h.write("keys/vault.KDBX", "harmless")
	h.write(".htpasswd", "harmless")
	h.write("fine.md", "fine")
	for link, target := range map[string]string{
		"notes.md":        ".env",
		"upper.md":        ".ENV",
		"report.md":       "deploy/server.PEM",
		"viagitdir.md":    ".git/config",
		"nested/link.txt": "../.ssh/known_hosts",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(h.work, link)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(h.work, link)); err != nil {
			t.Skip("no symlinks here:", err)
		}
	}
	// A link in a directory that is itself a link, so the name asked for has no secret in it at all.
	if err := os.Symlink(".ssh", filepath.Join(h.work, "pub")); err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{".kube/config", ".zrok/environment.json", "keys/vault.KDBX", ".htpasswd",
		".env", ".ENV", "./.Env", "deploy/server.PEM", "id_rsa", ".git/config",
		".ssh/known_hosts", "sub/Credentials.json",
		"notes.md", "upper.md", "report.md", "viagitdir.md", "nested/link.txt", "pub/known_hosts"} {
		_, err := h.publish(publishInput{Title: "t " + p, Path: p})
		r := refusalOf(t, err)
		if r.Status != 422 || r.Rule != "secret-file-name" {
			t.Errorf("%s: %+v", p, r)
		}
	}
	h.none()
	if _, err := h.publish(publishInput{Title: "fine", Path: "fine.md"}); err != nil {
		t.Fatalf("an ordinary file was refused: %v", err)
	}
}

// A ROOM THAT CANNOT SAY WHAT A PATH RESOLVED TO IS REFUSED, not guessed at. content still works.
func TestPublishRefusesAPathFromARoomThatCannotSayWhatItResolvedTo(t *testing.T) {
	h := newPublishHarness(t)
	h.noHeader = true
	h.write("fine.md", "fine")
	_, err := h.publish(publishInput{Title: "t", Path: "fine.md"})
	if r := refusalOf(t, err); r.Status != 502 || !strings.Contains(r.Msg, "content") {
		t.Fatalf("%+v", r)
	}
	h.none()
	if _, err := h.publish(publishInput{Title: "t", Content: "fine"}); err != nil {
		t.Fatalf("content from the same room: %v", err)
	}
}

// A SECRET CONTENT RULE APPLIES TO path, TO content, AND (see docs_api_test) TO A BOARD UPLOAD.
func TestPublishRefusesSecretContentOnPathAndOnContent(t *testing.T) {
	h := newPublishHarness(t)
	pem := "-----BEGIN PRIVATE KEY-----\nMIIE\n-----END PRIVATE KEY-----\n"
	cases := map[string]string{
		"pem-private-key": pem,
		"github-token":    "ghp_" + strings.Repeat("a", 36),
		"aws-access-key":  "AKIAIOSFODNN7EXAMPLE",
		"slack-token":     "xoxb-1234567890-abcdefghijkl",
		"jwt":             "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.dozjgNryP4J3jVmNHl0w5N",
		"zrok-token":      "zrok enable abcDEF123xyz",
	}
	i := 0
	for rule, body := range cases {
		i++
		// as content: what an agent does after reading .env itself
		_, err := h.publish(publishInput{Title: "c " + rule, Content: "notes\n" + body})
		if r := refusalOf(t, err); r.Status != 422 || r.Rule != rule {
			t.Errorf("content %s: %+v", rule, r)
		}
		// as a path with an innocent name
		name := "innocent" + string(rune('a'+i)) + ".md"
		h.write(name, body)
		_, err = h.publish(publishInput{Title: "p " + rule, Path: name})
		if r := refusalOf(t, err); r.Status != 422 || r.Rule != rule {
			t.Errorf("path %s: %+v", rule, r)
		}
	}
	h.none()
}

// slug ADDS A VERSION, AND AN UNKNOWN ONE IS 404.
func TestPublishWithASlugAddsAVersion(t *testing.T) {
	h := newPublishHarness(t)
	a, _ := h.publish(publishInput{Title: "Daily", Content: "one"})
	b, err := h.publish(publishInput{Slug: a.Slug, Content: "two"})
	if err != nil {
		t.Fatal(err)
	}
	if b.Slug != a.Slug || b.Version != 2 {
		t.Fatalf("%+v", b)
	}
	if d, _ := h.st.DocGet(a.Slug); len(d.Versions) != 2 || d.Title != "Daily" {
		t.Fatalf("%+v", d)
	}
	_, err = h.publish(publishInput{Slug: "no-such-doc", Content: "x"})
	if r := refusalOf(t, err); r.Status != 404 {
		t.Fatalf("%+v", r)
	}
}

func TestPublishInputRules(t *testing.T) {
	h := newPublishHarness(t)
	for name, in := range map[string]publishInput{
		"both":       {Title: "t", Content: "a", Path: "b"},
		"neither":    {Title: "t"},
		"no title":   {Content: "a"},
		"bad slug":   {Title: "t", Content: "a", Slug: "Not A Slug"},
		"empty file": {Title: "t", Path: "empty.md"},
	} {
		h.write("empty.md", "")
		_, err := h.publish(in)
		if r := refusalOf(t, err); r.Status != 400 {
			t.Errorf("%s: %+v", name, r)
		}
	}
	h.write("missing-ok.md", "x")
	_, err := h.publish(publishInput{Title: "t", Path: "nope.md"})
	if r := refusalOf(t, err); r.Status != 404 {
		t.Errorf("missing: %+v", r)
	}
	// No identity header: the hub cannot say which card is publishing.
	_, _, err = h.c.publishHandler(context.Background(), ctlReq("", ""), publishInput{Title: "t", Content: "a"})
	if err == nil {
		t.Fatal("an anonymous publish was accepted")
	}
	_, _, err = h.c.publishHandler(context.Background(), ctlReq("ghost", "sg4"), publishInput{Title: "t", Content: "a"})
	if err == nil || !strings.Contains(err.Error(), "no card") {
		t.Fatalf("an unknown card: %v", err)
	}
	h.none()
}

// PER-CARD, 30 PUBLISHES AN HOUR. The 31st is 429, another card is not held up, and a board upload
// is not counted.
func TestPublishIsLimitedPerCard(t *testing.T) {
	h := newPublishHarness(t)
	if _, err := h.st.DocAdd(hubstore.DocInput{Title: "upload", Name: "u.md", Data: []byte("board"),
		Origin: "local", By: "operator"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		if _, err := h.publish(publishInput{Title: "doc " + strings.Repeat("x", i), Content: "body " + strings.Repeat("y", i)}); err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
	}
	_, err := h.publish(publishInput{Title: "one too many", Content: "late"})
	if r := refusalOf(t, err); r.Status != 429 {
		t.Fatalf("%+v", r)
	}
	_, _, err = h.c.publishHandler(context.Background(), ctlReq("r-032", "sg4"), publishInput{Title: "other card", Content: "mine"})
	if err != nil {
		t.Fatalf("another card was held up: %v", err)
	}
}

// THE TOTAL CAP, AND THE SWITCH, SURFACE WITH THEIR STATUS.
func TestPublishReportsTheCapAndTheSwitch(t *testing.T) {
	h := newPublishHarness(t)
	small := int64(20)
	if _, err := h.st.SetDocSettings(hubstore.DocSettingsPatch{Caps: &struct {
		Text        *int64 `json:"text"`
		Other       *int64 `json:"other"`
		Total       *int64 `json:"total"`
		PerCardHour *int   `json:"per_card_hour"`
	}{Total: &small}}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.publish(publishInput{Title: "a", Content: strings.Repeat("a", 15)}); err != nil {
		t.Fatal(err)
	}
	_, err := h.publish(publishInput{Title: "b", Content: strings.Repeat("b", 15)})
	if r := refusalOf(t, err); r.Status != 507 {
		t.Fatalf("%+v", r)
	}
	off := false
	h.st.SetDocSettings(hubstore.DocSettingsPatch{Enabled: &off})
	_, err = h.publish(publishInput{Title: "c", Content: "c"})
	if r := refusalOf(t, err); r.Status != 503 {
		t.Fatalf("%+v", r)
	}
}

// A SIXTH MIB OF TEXT IS REFUSED, 413, on both content and path.
func TestPublishRefusesASixthMiBOfText(t *testing.T) {
	h := newPublishHarness(t)
	big := strings.Repeat("a", 5<<20+1)
	_, err := h.publish(publishInput{Title: "big", Content: big})
	if r := refusalOf(t, err); r.Status != 413 {
		t.Fatalf("content: %+v", r)
	}
	h.write("big.txt", big)
	_, err = h.publish(publishInput{Title: "big", Path: "big.txt"})
	if r := refusalOf(t, err); r.Status != 413 {
		t.Fatalf("path: %+v", r)
	}
	h.write("five.txt", strings.Repeat("a", 5<<20))
	if _, err := h.publish(publishInput{Title: "five", Path: "five.txt"}); err != nil {
		t.Fatalf("exactly five: %v", err)
	}
}

// THE TOOL IS IN THE WORKER SET AND ITS DESCRIPTION SAYS THE CHECKS ARE A SPEED BUMP.
func TestPublishIsAWorkerToolAndSaysItIsASpeedBump(t *testing.T) {
	if !inClass("atrium_publish", classWorker) || !inClass("atrium_publish", classFull) {
		t.Fatal("atrium_publish is not in both sets")
	}
	h := newClassHarness(t, classTasks)
	h.c.docs = func() *hubstore.Store { return nil }
	for _, who := range []string{"worker1", "boss"} {
		s := h.connect(t, who, "alpha")
		res, err := s.ListTools(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		var found *mcp.Tool
		for _, tl := range res.Tools {
			if tl.Name == "atrium_publish" {
				found = tl
			}
		}
		if found == nil {
			t.Fatalf("%s is not offered atrium_publish", who)
		}
		for _, want := range []string{"speed bump", "not a guarantee", "/d/<slug>", "30 publishes an hour"} {
			if !strings.Contains(strings.ToLower(found.Description), strings.ToLower(want)) {
				t.Errorf("the description does not say %q", want)
			}
		}
	}
}
