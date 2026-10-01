package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func TestContextCeilingSettingSavesAndReadsBack(t *testing.T) {
	srv, _, _ := fileServer(t)

	if out := settingsGet(t, srv); out["context_ceiling_k_now"] != float64(150) ||
		out["context_ceiling_k_default"] != float64(150) {
		t.Fatalf("the default reads %v", out["context_ceiling_k_now"])
	}
	if rec := settingsPost(t, srv, `{"context_ceiling_k":"220"}`); rec.Code != http.StatusOK {
		t.Fatalf("saving answered %d: %s", rec.Code, rec.Body.String())
	}
	if out := settingsGet(t, srv); out["context_ceiling_k"] != "220" || out["context_ceiling_k_now"] != float64(220) {
		t.Fatalf("read back %v %v", out["context_ceiling_k"], out["context_ceiling_k_now"])
	}
	if rec := settingsPost(t, srv, `{"context_ceiling_k":""}`); rec.Code != http.StatusOK {
		t.Fatalf("clearing answered %d: %s", rec.Code, rec.Body.String())
	}
	if out := settingsGet(t, srv); out["context_ceiling_k_now"] != float64(150) {
		t.Fatalf("cleared reads %v", out["context_ceiling_k_now"])
	}
}

func TestContextCeilingRefusesOutOfRangeAndBelowNotice(t *testing.T) {
	srv, st, _ := fileServer(t)
	for _, body := range []string{`{"context_ceiling_k":"49"}`, `{"context_ceiling_k":"2001"}`, `{"context_ceiling_k":"x"}`} {
		if rec := settingsPost(t, srv, body); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s answered %d, want 400", body, rec.Code)
		}
	}
	if rec := settingsPost(t, srv, `{"context_ceiling_k":"100"}`); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "context_threshold_k") {
		t.Fatalf("100 under the default 150 notice answered %d: %s", rec.Code, rec.Body.String())
	}
	if rec := settingsPost(t, srv, `{"context_threshold_k":"100","context_ceiling_k":"100"}`); rec.Code != http.StatusOK {
		t.Fatalf("a lowered notice and a ceiling at it answered %d: %s", rec.Code, rec.Body.String())
	}
	// A notice raised later leaves the ceiling in force at the notice.
	if err := st.SetSetting(store.SettingContextCeilingK, "120"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSetting(SettingContextThresholdK, "500"); err != nil {
		t.Fatal(err)
	}
	if got := EffectiveContextCeilingK(st); got != 500 {
		t.Fatalf("the ceiling in force is %dk under a 500k notice", got)
	}
}
