package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// What the skin setting refuses, and why refusing matters more here than
// anywhere else in settings.
//
// Every other setting in this file stores what it is given. A browse root that
// does not exist yet is a list somebody is preparing. An editor command that is
// not installed fails when it is used, where the failure means something.
//
// A skin is different: an unknown name SUCCEEDS at everything except the part
// that matters. It saves, the board puts it on the body element, no rule
// matches it, and every variable falls through to `:root`. The operator sees a
// setting that took their answer and did nothing at all.

func settingsPost(t *testing.T, srv *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/settings", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func settingsGet(t *testing.T, srv *Server) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/settings", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("settings answered %d: %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestASkinNobodyShipsIsRefused(t *testing.T) {
	srv, _, _ := fileServer(t)

	rec := settingsPost(t, srv, `{"board_skin":"midnight-hacker-pro"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("an unknown skin answered %d, wanted 400: %s", rec.Code, rec.Body.String())
	}
	// The message has to name what WOULD have worked. A refusal that only says
	// no leaves the operator opening the stylesheet to find out.
	if !strings.Contains(rec.Body.String(), "graphite") {
		t.Errorf("the refusal does not list the skins there are: %s", rec.Body.String())
	}
	if got := settingsGet(t, srv)["board_skin"]; got != DefaultSkin {
		t.Errorf("a refused skin still moved the setting to %v", got)
	}
}

func TestASkinThatShipsIsKeptAndReported(t *testing.T) {
	srv, _, _ := fileServer(t)

	if rec := settingsPost(t, srv, `{"board_skin":"noir"}`); rec.Code != http.StatusOK {
		t.Fatalf("noir answered %d: %s", rec.Code, rec.Body.String())
	}
	if got := settingsGet(t, srv)["board_skin"]; got != "noir" {
		t.Fatalf("read back %v, wanted noir", got)
	}
}

func TestClearingTheSkinIsARequest(t *testing.T) {
	srv, _, _ := fileServer(t)

	settingsPost(t, srv, `{"board_skin":"vapor"}`)
	// Empty is not "no opinion". It is somebody choosing the one it came with,
	// and it has to be able to undo the line above.
	if rec := settingsPost(t, srv, `{"board_skin":""}`); rec.Code != http.StatusOK {
		t.Fatalf("clearing answered %d: %s", rec.Code, rec.Body.String())
	}
	if got := settingsGet(t, srv)["board_skin"]; got != DefaultSkin {
		t.Fatalf("clearing left %v behind", got)
	}
}

// A name stored by a build that shipped a skin this one does not.
//
// Written straight to the store rather than through the endpoint, because the
// endpoint is what stops it. The case is a downgrade, or a skin withdrawn, and
// the wrong answer is to keep reporting the missing name: the board would set
// an attribute nothing matches and look exactly like the default while the
// picker showed something else.
func TestASkinThatIsNoLongerShippedReadsAsTheDefault(t *testing.T) {
	srv, st, _ := fileServer(t)

	if err := st.SetSetting(SettingBoardSkin, "a-skin-from-the-future"); err != nil {
		t.Fatal(err)
	}
	if got := settingsGet(t, srv)["board_skin"]; got != DefaultSkin {
		t.Fatalf("a withdrawn skin read back as %v", got)
	}
}

// The picker is built from what this answers, so an empty or short list is a
// board with no way to choose.
func TestTheSkinListIsSentToTheBoard(t *testing.T) {
	srv, _, _ := fileServer(t)

	raw, ok := settingsGet(t, srv)["board_skins"].([]any)
	if !ok {
		t.Fatal("no board_skins in the settings answer")
	}
	if len(raw) != len(Skins) {
		t.Fatalf("sent %d skins, there are %d", len(raw), len(Skins))
	}
	// The default has to be first. The board decides whether to set the body
	// attribute at all by comparing against the head of this list, so a
	// reordering here would make the default the one skin that cannot be worn.
	if raw[0] != DefaultSkin {
		t.Fatalf("the list starts with %v, not the default", raw[0])
	}
}
