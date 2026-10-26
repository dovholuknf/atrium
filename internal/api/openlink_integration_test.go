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
)

func linkReq(path, id, body string) *http.Request {
	r := httptest.NewRequest("POST", "http://127.0.0.1:7778"+path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:50000"
	r.SetPathValue("id", id)
	return r
}

func TestAnOpenLinkIsIssuedForOneLaunchableFileAndRedeemedOnce(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	for _, f := range []string{"notes.md", "tool.exe"} {
		if err := os.WriteFile(filepath.Join(work, f), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	issue := func(p string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.launchLink(w, linkReq("/v1/tasks/"+task.ID+"/files/launch-link", task.ID, `{"path":"`+p+`"}`))
		return w
	}
	if w := issue("tool.exe"); w.Code != 400 {
		t.Fatalf("a program was issued a link: %d", w.Code)
	}
	if w := issue("../x.md"); w.Code != 403 {
		t.Fatalf("a path outside the card was issued a link: %d", w.Code)
	}
	remote := linkReq("/x", task.ID, `{"path":"notes.md"}`)
	remote.Host = "board.example.com"
	if w := httptest.NewRecorder(); func() int { s.launchLink(w, remote); return w.Code }() != 409 {
		t.Fatal("a board that is not on this machine was issued a link")
	}
	w := issue("notes.md")
	if w.Code != 200 {
		t.Fatalf("issue answered %d: %s", w.Code, w.Body.String())
	}
	var out struct{ URL, Install string }
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	addr, card, tok, err := ParseOpenLink(out.URL)
	if err != nil || addr != "127.0.0.1:7778" || card != task.ID {
		t.Fatalf("link %q parsed as %q %q: %v", out.URL, addr, card, err)
	}
	if !filepath.IsAbs(strings.Trim(strings.TrimSuffix(out.Install, " open-handler install"), `"`)) {
		t.Fatalf("install command %q does not name the binary by its full path", out.Install)
	}
	redeem := func(tk string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.redeemLink(w, linkReq("/x", task.ID, `{"t":"`+tk+`"}`))
		return w
	}
	if w := redeem("guess"); w.Code != 403 {
		t.Fatalf("a token nobody issued answered %d", w.Code)
	}
	if w := redeem(tok); w.Code != 200 || !strings.Contains(w.Body.String(), "notes.md") {
		t.Fatalf("redeem answered %d: %s", w.Code, w.Body.String())
	}
	if w := redeem(tok); w.Code != 403 {
		t.Fatalf("a token worked twice: %d", w.Code)
	}
}

func TestAnOpenLinkNamesOnlyThisMachine(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"atrium-open://open?addr=evil.example.com%3A80&id=a&t=b",
		"atrium-open://open?addr=127.0.0.1&id=a&t=b",
		"atrium-open://open?addr=127.0.0.1%3A7778&t=b",
		"https://open?addr=127.0.0.1%3A7778&id=a&t=b",
	} {
		if _, _, _, err := ParseOpenLink(raw); err == nil {
			t.Errorf("%q was accepted", raw)
		}
	}
}
