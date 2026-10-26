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

func TestExecutableRefusesEveryProgramAndScriptType(t *testing.T) {
	t.Parallel()
	for _, ext := range strings.Fields(".exe .msi .bat .cmd .com .ps1 .psm1 .vbs .js .jse .wsf .scr .lnk .url .reg " +
		".sh .command .app .pkg .dmg .jar .EXE .Ps1") {
		if !Executable(ext) {
			t.Errorf("%s is not refused", ext)
		}
	}
	for _, ext := range []string{".png", ".pdf", ".md", ".txt", ".html"} {
		if Executable(ext) {
			t.Errorf("%s is refused", ext)
		}
	}
}

func TestOpenExternalAnywhereIsSwitchedOnlyByALocalOperator(t *testing.T) {
	t.Parallel()
	srv, _, _ := fileServer(t)
	post := func(remote, host, body string) int {
		r := httptest.NewRequest(http.MethodPost, "/v1/settings", strings.NewReader(body))
		r.RemoteAddr, r.Host = remote, host
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, r)
		return w.Code
	}
	if got := settingsGet(t, srv)["open_external_anywhere"]; got != false {
		t.Fatalf("a new room reads open_external_anywhere=%v", got)
	}
	if c := post("192.0.2.1:1234", "atrium.example", `{"open_external_anywhere":true,"usage_cache_reads":true}`); c != 403 {
		t.Fatalf("a remote switch answered %d, want 403", c)
	}
	if got := settingsGet(t, srv); got["open_external_anywhere"] != false || got["usage_cache_reads"] != false {
		t.Fatalf("a refused body was half applied: %v %v", got["open_external_anywhere"], got["usage_cache_reads"])
	}
	if c := post("127.0.0.1:1234", "localhost:9", `{"open_external_anywhere":true}`); c != 200 {
		t.Fatalf("a local switch answered %d", c)
	}
	if got := settingsGet(t, srv)["open_external_anywhere"]; got != true {
		t.Fatalf("settings read back open_external_anywhere=%v", got)
	}
}

func TestLaunchOutsideTheCardNeedsTheSettingAndNeverAProgram(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	var launched []string
	s.hk.launchFile = func(p, dir string) error { launched = append(launched, p); return nil }

	outside := t.TempDir()
	pic, tool, script := filepath.Join(outside, "pic.png"), filepath.Join(outside, "tool.exe"), filepath.Join(outside, "run.ps1")
	for _, f := range []string{pic, tool, script, filepath.Join(work, "inside.bat")} {
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	try := func(p string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.launchFile(w, launchReq(task.ID, p))
		return w
	}

	if w := try(filepath.ToSlash(pic)); w.Code != 403 {
		t.Fatalf("outside with the setting off answered %d", w.Code)
	}
	if err := st.SetSetting(SettingOpenExternalAnywhere, "on"); err != nil {
		t.Fatal(err)
	}
	if w := try(filepath.ToSlash(pic)); w.Code != 200 {
		t.Fatalf("outside with the setting on answered %d: %s", w.Code, w.Body.String())
	}
	for _, p := range []string{filepath.ToSlash(tool), filepath.ToSlash(script), "inside.bat"} {
		w := try(p)
		if w.Code != 400 || !strings.Contains(w.Body.String(), "never opens") {
			t.Errorf("%s answered %d %s, want the refusal with its reason", p, w.Code, w.Body.String())
		}
	}
	if w := try(filepath.ToSlash(filepath.Join(outside, "missing.png"))); w.Code != 403 {
		t.Errorf("a missing outside file answered %d", w.Code)
	}
	if w := try("../" + filepath.Base(outside) + "/pic.png"); w.Code != 403 {
		t.Errorf("a relative path out of the card answered %d", w.Code)
	}
	real, _ := filepath.EvalSymlinks(pic)
	if strings.Join(launched, "|") != real {
		t.Fatalf("launched %q, want only %q", launched, real)
	}
}
