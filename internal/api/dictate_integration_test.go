//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func dictateReq(id, body string) *http.Request {
	r := httptest.NewRequest("POST", "/v1/tasks/"+id+"/dictate", strings.NewReader(body))
	r.SetPathValue("id", id)
	return r
}

const fakeWAV = "RIFF\x24\x00\x00\x00WAVEfmt " + "\x10\x00\x00\x00\x01\x00\x01\x00\x80\x3e\x00\x00\x00\x7d\x00\x00\x02\x00\x10\x00data\x00\x00\x00\x00"

func TestDictateOffAnswers503(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	w := httptest.NewRecorder()
	s.dictate(w, dictateReq(task.ID, fakeWAV))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", w.Code)
	}
}

func TestDictateRefusesNotWAV(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	s.Dictate = func(context.Context, []byte, []string) (string, error) { return "never", nil }
	w := httptest.NewRecorder()
	s.dictate(w, dictateReq(task.ID, "OggS not a wav at all, padded past forty four bytes"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", w.Code)
	}
}

func TestDictatePassesTheClipAndTheCardWords(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	var gotLen int
	var gotWords []string
	s.Dictate = func(_ context.Context, wav []byte, words []string) (string, error) {
		gotLen, gotWords = len(wav), words
		return "merge claude/main", nil
	}
	w := httptest.NewRecorder()
	s.dictate(w, dictateReq(task.ID, fakeWAV))
	if w.Code != http.StatusOK {
		t.Fatalf("got %d: %s", w.Code, w.Body)
	}
	var out struct{ Text string }
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out.Text != "merge claude/main" || gotLen != len(fakeWAV) {
		t.Fatalf("text %q, clip %d bytes", out.Text, gotLen)
	}
	if task.WireName != "" && !slices.Contains(gotWords, task.WireName) {
		t.Fatalf("words %v lack the card's handle %q", gotWords, task.WireName)
	}
}
