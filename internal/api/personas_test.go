package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func personaCall(t *testing.T, srv *Server, method, path, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// The catalog and the lessons view over HTTP, against a pack in a temp dir.
func TestPersonaEndpoints(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	st := openStore(t)
	srv := New(st)

	// Off: an answer that says so, not an error.
	code, out := personaCall(t, srv, http.MethodGet, "/v1/personas", "")
	if code != http.StatusOK || out["pack"] != "" || out["setting"] != store.SettingPersonaPackPath {
		t.Fatalf("off: %d %v", code, out)
	}
	if code, _ := personaCall(t, srv, http.MethodGet, "/v1/personas/x/lessons", ""); code != http.StatusConflict {
		t.Fatalf("lessons with no pack: %d", code)
	}

	root := filepath.Join(t.TempDir(), "dotagents")
	pack := filepath.Join(root, "personas")
	mk := func(rel, body string) {
		p := filepath.Join(pack, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("rev/persona.yaml", "id: rev\nname: Reviewer\ndescription: reviews\nrunners: claude\nreviews:\n  paths: [\"**/*.go\"]\n")
	mk("rev/render/claude/rev.md", "---\nname: rev\n---\nbody\n")
	mk("rev/memory/MEMORY.md", "- [L](l.md) a lesson\n")
	mk("rev/memory/l.md", "---\nname: l\ndescription: a lesson\nrepo: general\n---\n\n**Why:** because\n")
	for _, args := range [][]string{{"init"}, {"add", "-A"}, {"commit", "-m", "pack"}} {
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t",
			"-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	if err := st.SetSetting(store.SettingPersonaPackPath, filepath.ToSlash(pack)); err != nil {
		t.Fatal(err)
	}

	code, out = personaCall(t, srv, http.MethodGet, "/v1/personas", "")
	list, _ := out["personas"].([]any)
	if code != http.StatusOK || len(list) != 1 {
		t.Fatalf("catalog: %d %v", code, out)
	}
	row := list[0].(map[string]any)
	if row["id"] != "rev" || row["description"] != "reviews" || row["renders"].([]any)[0] != "claude" {
		t.Fatalf("row %v", row)
	}

	code, out = personaCall(t, srv, http.MethodGet, "/v1/personas/rev/lessons", "")
	if code != http.StatusOK || len(out["lessons"].([]any)) != 1 {
		t.Fatalf("lessons: %d %v", code, out)
	}
	code, out = personaCall(t, srv, http.MethodPost, "/v1/personas/rev/lessons",
		`{"file":"memory/l.md","action":"promote"}`)
	if code != http.StatusOK || !strings.Contains(out["commit"].(string), "1 promoted") {
		t.Fatalf("promote: %d %v", code, out)
	}
	if _, err := os.Stat(filepath.Join(pack, "rev", "knowledge", "_general.md")); err != nil {
		t.Fatalf("promote wrote no knowledge: %v", err)
	}
	// A file that is not under review is refused, whatever it names.
	if code, _ := personaCall(t, srv, http.MethodPost, "/v1/personas/rev/lessons",
		`{"file":"persona.yaml","action":"delete"}`); code != http.StatusBadRequest {
		t.Fatalf("delete of persona.yaml answered %d", code)
	}
	if _, err := os.Stat(filepath.Join(pack, "rev", "persona.yaml")); err != nil {
		t.Fatal("persona.yaml was deleted")
	}
}

func TestPersonaReviewIsHandedToTheDaemon(t *testing.T) {
	srv := New(openStore(t))
	if code, _ := personaCall(t, srv, http.MethodPost, "/v1/tasks/abc/persona-review",
		`{"persona":"rev"}`); code != http.StatusNotImplemented {
		t.Fatalf("with no daemon: %d", code)
	}
	var got []string
	srv.PersonaReview = func(taskID, id, runner string) (*store.Task, error) {
		got = []string{taskID, id, runner}
		return &store.Task{ID: "new"}, nil
	}
	code, out := personaCall(t, srv, http.MethodPost, "/v1/tasks/abc/persona-review", `{"persona":"rev"}`)
	if code != http.StatusOK || out["id"] != "new" || strings.Join(got, ",") != "abc,rev,claude" {
		t.Fatalf("%d %v %v", code, out, got)
	}
}
