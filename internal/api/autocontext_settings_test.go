package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// The automatic new context settings save, read back, and say what is in force.
func TestAutoNewContextSettingsSaveAndReadBack(t *testing.T) {
	srv, _, _ := fileServer(t)

	out := settingsGet(t, srv)
	if out["auto_new_context_now"] != "off" || out["auto_new_context_k_now"] != float64(300) ||
		out["auto_new_context_idle_s_now"] != float64(45) {
		t.Fatalf("the defaults read %v %v %v", out["auto_new_context_now"], out["auto_new_context_k_now"], out["auto_new_context_idle_s_now"])
	}

	body := `{"auto_new_context":"tagged","auto_new_context_k":"200","auto_new_context_idle_s":"30"}`
	if rec := settingsPost(t, srv, body); rec.Code != http.StatusOK {
		t.Fatalf("saving answered %d: %s", rec.Code, rec.Body.String())
	}
	out = settingsGet(t, srv)
	if out["auto_new_context"] != "tagged" || out["auto_new_context_now"] != "tagged" ||
		out["auto_new_context_k"] != "200" || out["auto_new_context_k_now"] != float64(200) ||
		out["auto_new_context_idle_s"] != "30" || out["auto_new_context_idle_s_now"] != float64(30) {
		t.Fatalf("read back %v", out)
	}

	// Cleared is the default again.
	if rec := settingsPost(t, srv, `{"auto_new_context":"","auto_new_context_k":"","auto_new_context_idle_s":""}`); rec.Code != http.StatusOK {
		t.Fatalf("clearing answered %d: %s", rec.Code, rec.Body.String())
	}
	out = settingsGet(t, srv)
	if out["auto_new_context_now"] != "off" || out["auto_new_context_k_now"] != float64(300) {
		t.Fatalf("cleared reads %v %v", out["auto_new_context_now"], out["auto_new_context_k_now"])
	}
}

func TestAutoNewContextSettingsRefuseWhatIsOutOfRange(t *testing.T) {
	srv, _, _ := fileServer(t)
	for _, body := range []string{
		`{"auto_new_context":"always"}`,
		`{"auto_new_context_k":"49"}`,
		`{"auto_new_context_k":"2001"}`,
		`{"auto_new_context_idle_s":"9"}`,
	} {
		if rec := settingsPost(t, srv, body); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s answered %d, want 400", body, rec.Code)
		}
	}
}

// The launcher is told at context_threshold_k, so the automatic line may not sit below it,
// whether that number is stored or typed in the same request.
func TestAutoContextRefusesThresholdBelowNotice(t *testing.T) {
	srv, st, _ := fileServer(t)

	if rec := settingsPost(t, srv, `{"auto_new_context_k":"100"}`); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "context_threshold_k") {
		t.Fatalf("100 under the default 150 notice answered %d: %s", rec.Code, rec.Body.String())
	}
	if rec := settingsPost(t, srv, `{"auto_new_context_k":"150"}`); rec.Code != http.StatusOK {
		t.Fatalf("equal to the notice answered %d: %s", rec.Code, rec.Body.String())
	}

	// Typed together: the new notice is the one it is checked against.
	if rec := settingsPost(t, srv, `{"context_threshold_k":"400","auto_new_context_k":"300"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("300 under a typed 400 notice answered %d: %s", rec.Code, rec.Body.String())
	}
	if rec := settingsPost(t, srv, `{"context_threshold_k":"100","auto_new_context_k":"100"}`); rec.Code != http.StatusOK {
		t.Fatalf("a lowered notice and a line at it answered %d: %s", rec.Code, rec.Body.String())
	}

	// The stored notice.
	if err := st.SetSetting(SettingContextThresholdK, "500"); err != nil {
		t.Fatal(err)
	}
	if rec := settingsPost(t, srv, `{"auto_new_context_k":"400"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("400 under a stored 500 notice answered %d: %s", rec.Code, rec.Body.String())
	}

	// A notice raised later than the line leaves the line in force at the notice.
	if err := st.SetSetting(store.SettingAutoNewContextK, "200"); err != nil {
		t.Fatal(err)
	}
	if got := EffectiveAutoNewContextK(st); got != 500 {
		t.Fatalf("the line in force is %dk under a 500k notice", got)
	}
}
