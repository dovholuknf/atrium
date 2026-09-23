package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// A gemini row on a room whose home is a temp directory and whose one
// provider root is the work directory. Never the real ~/.gemini.
func setupServer(t *testing.T) (*Server, string, string) {
	t.Helper()
	srv, st, work := fileServer(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("GEMINI_CLI_HOME", "")
	t.Setenv("GEMINI_CLI_TRUSTED_FOLDERS_PATH", "")
	t.Setenv("GEMINI_CLI_TRUST_WORKSPACE", "")
	if _, err := st.SaveProvider(store.Provider{Name: "gh", Kind: "git", Root: work, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveHarness(store.Harness{ID: "gem", Cmd: "gemini", LaunchMode: store.LaunchPTY}); err != nil {
		t.Fatal(err)
	}
	return srv, home, work
}

func postFix(t *testing.T, srv *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/harnesses/gem/setup/fix",
		strings.NewReader(body)))
	return rec
}

func TestHarnessListCarriesTheSetupReport(t *testing.T) {
	srv, _, work := setupServer(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/harnesses", nil))
	var body struct {
		Harnesses []harnessView `json:"harnesses"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	var gem *harnessView
	for i := range body.Harnesses {
		if body.Harnesses[i].ID == "gem" {
			gem = &body.Harnesses[i]
		}
		if body.Harnesses[i].ID == "ollama" && body.Harnesses[i].Setup != nil {
			t.Fatal("a runner with no adapter carries a setup report")
		}
	}
	if gem == nil || gem.Setup == nil || gem.Setup.Adapter != "gemini" {
		t.Fatalf("the gemini row has no setup report: %+v", gem)
	}
	trust := gem.Setup.Checks[0]
	if trust.ID != "trust" || trust.State != "fail" || len(trust.Targets) != 1 ||
		!strings.EqualFold(filepath.ToSlash(trust.Targets[0]), filepath.ToSlash(work)) {
		t.Fatalf("want the provider root offered for trust, got %+v", trust)
	}
}

func TestFixTrustsTheRootInTheRoomsHome(t *testing.T) {
	srv, home, work := setupServer(t)
	body, _ := json.Marshal(map[string]string{"check": "trust", "target": work})
	rec := postFix(t, srv, string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("fix answered %d: %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(filepath.Join(home, ".gemini", "trustedFolders.json")); err != nil {
		t.Fatal("the fix did not write the trust file in the room's home")
	}
}

func TestFixRefusesExplainOnlyAndUnofferedTargets(t *testing.T) {
	srv, home, _ := setupServer(t)
	if rec := postFix(t, srv, `{"check":"auth"}`); rec.Code != http.StatusConflict {
		t.Fatalf("an explain-only check should be 409, got %d", rec.Code)
	}
	body, _ := json.Marshal(map[string]string{"check": "trust", "target": home})
	if rec := postFix(t, srv, string(body)); rec.Code != http.StatusConflict {
		t.Fatalf("a target that is not a root should be 409, got %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(home, ".gemini", "trustedFolders.json")); err == nil {
		t.Fatal("a refused fix wrote the trust file")
	}
}
