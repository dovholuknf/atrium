//go:build integration

package link

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"testing"
)

type fakeAccess struct{ calls []string }

func (f *fakeAccess) View() any                         { return map[string]any{"entries": []any{}} }
func (f *fakeAccess) SaveAuth([]byte) error             { f.calls = append(f.calls, "auth"); return nil }
func (f *fakeAccess) Add(context.Context, []byte) error { f.calls = append(f.calls, "add"); return nil }
func (f *fakeAccess) OpenUp(id, account string) error {
	f.calls = append(f.calls, "open "+id+" "+account)
	return nil
}
func (f *fakeAccess) Approve(room string) error {
	f.calls = append(f.calls, "approve "+room)
	return nil
}
func (f *fakeAccess) Revoke(id string) error {
	f.calls = append(f.calls, "revoke "+id)
	if id == "gone" {
		return ErrAccessNotFound
	}
	return errors.New("never")
}

func TestTheHubAnswersTheAccessScreen(t *testing.T) {
	front, _, done := two(t, settingsRoom("alpha", "moss"), settingsRoom("beta", "ember"))
	defer done()
	fake := &fakeAccess{}
	front.Config.Handler.(*Proxy).SetHubAccess(fake)

	do := func(method, path, body string, forwarded bool) int {
		req, _ := http.NewRequest(method, front.URL+path, bytes.NewReader([]byte(body)))
		if forwarded {
			req.Header.Set("X-Forwarded-For", "203.0.113.9")
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if got := getJSON(t, front.URL+"/v1/access"); got["entries"] == nil {
		t.Fatalf("the list read %v", got)
	}
	if c := do(http.MethodPost, "/v1/access/entries", `{}`, false); c != http.StatusOK {
		t.Fatalf("an add answered %d", c)
	}
	if c := do(http.MethodPost, "/v1/access/rooms/lab/approve", "", false); c != http.StatusOK {
		t.Fatalf("an approve answered %d", c)
	}
	if c := do(http.MethodPost, "/v1/access/entries/e1/open", `{"account":"x@y.z"}`, false); c != http.StatusOK {
		t.Fatalf("an open answered %d", c)
	}
	if c := do(http.MethodDelete, "/v1/access/entries/gone", "", false); c != http.StatusNotFound {
		t.Fatalf("revoking a missing entry answered %d", c)
	}
	n := len(fake.calls)
	if c := do(http.MethodPost, "/v1/access/entries", `{}`, true); c != http.StatusForbidden || len(fake.calls) != n {
		t.Fatalf("a write from off the machine answered %d and ran %v", c, fake.calls)
	}
	if c := do(http.MethodPost, "/v1/access/nonsense", "", false); c != http.StatusNotFound {
		t.Fatalf("an unknown route answered %d", c)
	}
}
