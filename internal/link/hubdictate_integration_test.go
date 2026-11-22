//go:build integration

package link

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeDict struct {
	on    bool
	words []string
}

func (f *fakeDict) On() bool { return f.on }

func (f *fakeDict) Transcribe(_ context.Context, _ []byte, words []string) (string, error) {
	f.words = words
	return "hello hub", nil
}

func clip(t *testing.T, p *Proxy, words string) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	wav := "RIFF\x00\x00\x00\x00WAVE" + strings.Repeat("x", 40)
	r := httptest.NewRequest(http.MethodPost, "/v1/tasks/abc/dictate", strings.NewReader(wav))
	r.Header.Set(dictateWords, words)
	w := httptest.NewRecorder()
	return w, p.hubDictate(w, r)
}

// The hub answers a clip itself once it has speech to text, and hands the board's card names to whisper.
func TestHubDictateAnswersFromTheHub(t *testing.T) {
	f := &fakeDict{on: true}
	p := &Proxy{}
	p.SetDictation(f)
	w, handled := clip(t, p, "alpha,my%20card")
	if !handled || w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "hello hub") {
		t.Fatalf("handled=%v code=%d body=%s", handled, w.Code, w.Body.String())
	}
	if len(f.words) != 2 || f.words[1] != "my card" {
		t.Fatalf("words = %q", f.words)
	}
}

// With no hub speech to text the clip is left for the card's room, as before.
func TestHubDictateFallsThroughWhenOff(t *testing.T) {
	p := &Proxy{}
	if _, handled := clip(t, p, ""); handled {
		t.Fatal("a hub with no dictation took the clip")
	}
	p.SetDictation(&fakeDict{on: false})
	if _, handled := clip(t, p, ""); handled {
		t.Fatal("a hub with dictation off took the clip")
	}
}

type urlStock struct {
	Inventory
	m map[string]string
}

func (s *urlStock) HubSetting(n string) (string, error) { return s.m[n], nil }
func (s *urlStock) SetHubSetting(n, v string) error     { s.m[n] = v; return nil }

func saveURL(t *testing.T, stock *urlStock, body map[string]any, remote string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/v1/settings", nil)
	r.RemoteAddr = remote
	w := httptest.NewRecorder()
	(&Proxy{}).saveHubDictate(w, r, body, stock)
	return w
}

// A plain http url off this machine needs the operator's confirmation, and only the hub's machine may set one.
func TestSaveDictateURL(t *testing.T) {
	stock := &urlStock{m: map[string]string{}}
	if w := saveURL(t, stock, map[string]any{"dictate_url": "http://gpu.corp:8080"}, "127.0.0.1:5000"); w.Code != 400 {
		t.Fatalf("unconfirmed plain http: %d", w.Code)
	}
	if w := saveURL(t, stock, map[string]any{"dictate_url": "http://gpu.corp:8080", "confirm_plain_http": true},
		"127.0.0.1:5000"); w.Code != 200 || stock.m["dictate_url"] != "http://gpu.corp:8080" {
		t.Fatalf("confirmed: %d %v", w.Code, stock.m)
	}
	if w := saveURL(t, stock, map[string]any{"dictate_url": "https://x"}, "10.0.0.5:5000"); w.Code != 403 {
		t.Fatalf("remote caller: %d", w.Code)
	}
	if w := saveURL(t, stock, map[string]any{"dictate_url": ""}, "127.0.0.1:5000"); w.Code != 200 || stock.m["dictate_url"] != "" {
		t.Fatalf("clear: %d %v", w.Code, stock.m)
	}
}

// The enable switch asks for the download to be confirmed first, and turning it off clears the two paths.
func TestEnableDictationNeedsConfirmAndOffClearsPaths(t *testing.T) {
	stock := &urlStock{m: map[string]string{"dictate_server": "a", "dictate_model": "b"}}
	p := &Proxy{}
	p.SetDictateInstall(t.TempDir())
	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/v1/settings", nil)
	r.RemoteAddr = "127.0.0.1:5000"
	w := httptest.NewRecorder()
	p.saveHubDictate(w, r, map[string]any{"dictate_enable": true}, stock)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "confirm the download") {
		t.Fatalf("unconfirmed: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	p.saveHubDictate(w, r, map[string]any{"dictate_enable": false}, stock)
	if w.Code != 200 || stock.m["dictate_server"] != "" || stock.m["dictate_model"] != "" {
		t.Fatalf("off: %d %v", w.Code, stock.m)
	}
	r.RemoteAddr = "10.0.0.5:5000"
	w = httptest.NewRecorder()
	p.saveHubDictate(w, r, map[string]any{"dictate_enable": true, "confirm_download": true}, stock)
	if w.Code != 403 {
		t.Fatalf("remote: %d", w.Code)
	}
}

// A hub with one room sends /v1/settings straight to that room, and the board still gets the hub's speech to text.
func TestOneRoomHubStillShowsTheHubSpeechToTextSettings(t *testing.T) {
	proxy, done := oneRoom(t, "alpha", settingsRoom("alpha", "moss"))
	defer done()
	proxy.SetInventory(&urlStock{Inventory: &remembering{}, m: map[string]string{"dictate_url": "https://gpu.example:8080"}})
	front := httptest.NewServer(proxy)
	defer front.Close()
	got := getJSON(t, front.URL+"/v1/settings")
	if got["hub_dictate_url"] != "https://gpu.example:8080" || got["board_skin"] != "moss" {
		t.Fatalf("settings %v", got)
	}
}
