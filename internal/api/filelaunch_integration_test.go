//go:build integration

package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func launchReq(id, p string) *http.Request {
	r := httptest.NewRequest("POST", "/v1/tasks/"+id+"/files/launch", strings.NewReader(`{"path":`+jsonString(p)+`}`))
	r.SetPathValue("id", id)
	return r
}

func jsonString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\`, `"`, `\"`).Replace(s) + `"`
}

func TestLaunchOpensADocumentInTheCardAndNothingElse(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	var launched []string

	s.hk.launchFile = func(p, dir string) error { launched = append(launched, p); return nil }

	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.png")
	for _, f := range []string{filepath.Join(work, "shot.png"), filepath.Join(work, "notes.md"),
		filepath.Join(work, "tool.exe"), filepath.Join(work, "run.js"), secret} {
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(work, "d.png"), 0o700); err != nil {
		t.Fatal(err)
	}
	linked := os.Symlink(filepath.Join(work, "tool.exe"), filepath.Join(work, "pic.png")) == nil

	cases := []struct {
		path string
		want int
	}{
		{"shot.png", 200},
		{"notes.md", 200},
		{filepath.ToSlash(filepath.Join(work, "shot.png")), 200},
		{"tool.exe", 400},
		{"run.js", 400},
		{"d.png", 400},
		{"missing.png", 403},
		{filepath.ToSlash(secret), 403},
		{"../" + filepath.Base(outside) + "/secret.png", 403},
		{"", 400},
	}
	if linked {
		cases = append(cases, struct {
			path string
			want int
		}{"pic.png", 400})
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		s.launchFile(w, launchReq(task.ID, c.path))
		if w.Code != c.want {
			t.Errorf("%q answered %d, want %d: %s", c.path, w.Code, c.want, w.Body.String())
		}
	}
	want := []string{filepath.Join(work, "shot.png"), filepath.Join(work, "notes.md"), filepath.Join(work, "shot.png")}
	if strings.Join(launched, "|") != strings.Join(want, "|") {
		t.Fatalf("launched %q, want %q", launched, want)
	}
}
