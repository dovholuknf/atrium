//go:build integration

package cli

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func exeName(stem string) string {
	if runtime.GOOS == "windows" {
		return stem + ".exe"
	}
	return stem
}

func writeFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return string(b)
}

func postInto(h http.HandlerFunc, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/restart-into", strings.NewReader(body))
	if token != "" {
		req.Header.Set("X-Atrium-Token", token)
	}
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func TestRestartIntoTakesOnlyTheTokenAnAllowedBuildThatRuns(t *testing.T) {
	home := t.TempDir()
	good := filepath.Join(home, exeName("atrium.new"))
	writeFile(t, good, "new")
	var started string
	probeErr := error(nil)
	h := restartInto("sekrit", []string{home}, func(string) error { return probeErr },
		func(exe string, _ restartIntoAsk) error { started = exe; return nil })
	body := `{"exe":` + jsonString(good) + `}`

	if rec := postInto(h, "", body); rec.Code != http.StatusForbidden || started != "" {
		t.Fatalf("no token: %d, started %q", rec.Code, started)
	}
	if rec := postInto(h, "wrong", body); rec.Code != http.StatusForbidden || started != "" {
		t.Fatalf("wrong token: %d, started %q", rec.Code, started)
	}
	if rec := postInto(restartInto("", []string{home}, nil, nil), "", body); rec.Code != http.StatusForbidden {
		t.Fatalf("no token on the room: %d", rec.Code)
	}
	other := filepath.Join(home, exeName("calc"))
	writeFile(t, other, "x")
	if rec := postInto(h, "sekrit", `{"exe":`+jsonString(other)+`}`); rec.Code != http.StatusBadRequest || started != "" {
		t.Fatalf("a non-atrium exe: %d, started %q", rec.Code, started)
	}
	probeErr = errors.New("not a program")
	if rec := postInto(h, "sekrit", body); rec.Code != http.StatusUnprocessableEntity || started != "" {
		t.Fatalf("a build that does not run: %d, started %q", rec.Code, started)
	}
	probeErr = nil
	if rec := postInto(h, "sekrit", body); rec.Code != http.StatusAccepted || started == "" {
		t.Fatalf("a good ask: %d %s, started %q", rec.Code, rec.Body.String(), started)
	}
}

func jsonString(s string) string { return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"` }

func TestAllowedSwapExeIsAnAtriumNewBuildInHomeOrTheInstallDir(t *testing.T) {
	home, inst, elsewhere := t.TempDir(), t.TempDir(), t.TempDir()
	dirs := []string{home, inst}
	for _, ok := range []string{
		filepath.Join(home, exeName("atrium.new")), filepath.Join(home, exeName("atrium-zrok.new")),
		filepath.Join(inst, exeName("atrium.new")),
	} {
		writeFile(t, ok, "x")
		if _, err := allowedSwapExe(ok, dirs); err != nil {
			t.Errorf("%s refused: %v", ok, err)
		}
	}
	for _, bad := range []string{
		filepath.Join(elsewhere, exeName("atrium.new")), filepath.Join(home, exeName("atrium")),
		filepath.Join(home, exeName("evil.new")), filepath.Join(home, "sub", exeName("atrium.new")),
	} {
		_ = os.MkdirAll(filepath.Dir(bad), 0o700)
		writeFile(t, bad, "x")
		if _, err := allowedSwapExe(bad, dirs); err == nil {
			t.Errorf("%s allowed", bad)
		}
	}
	if _, err := allowedSwapExe(exeName("atrium.new"), dirs); err == nil {
		t.Error("a relative path allowed")
	}
	if _, err := allowedSwapExe(filepath.Join(home, exeName("atrium.new"))+"/../"+exeName("atrium.new"), dirs); err != nil {
		t.Errorf("a path that cleans to an allowed one refused: %v", err)
	}
}

func TestProbeSwapExeRefusesABrokenBuild(t *testing.T) {
	bad := filepath.Join(t.TempDir(), exeName("atrium.new"))
	writeFile(t, bad, "this is not a program")
	if err := probeSwapExe(bad); err == nil {
		t.Fatal("a broken build passed the probe")
	}
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func swapFixture(t *testing.T) (dir, self, newExe string, l roomLaunch) {
	dir = t.TempDir()
	self = filepath.Join(dir, exeName("atrium"))
	newExe = filepath.Join(dir, exeName("atrium.new"))
	writeFile(t, self, "old")
	writeFile(t, newExe, "new")
	writeFile(t, swapSibling(self, "prev"), "older")
	return dir, self, newExe, roomLaunch{dir: dir, human: freeAddr(t), agent: "a"}
}

func exitedChild(t *testing.T) *os.Process {
	t.Helper()
	c := exec.Command(os.Args[0], "-test.run=^$")
	if err := c.Run(); err != nil {
		t.Fatal(err)
	}
	return c.Process
}

func TestSwapPutsTheNewBuildInPlaceWhenItAnswers(t *testing.T) {
	_, self, newExe, l := swapFixture(t)
	var ran string
	s := &swapper{self: self,
		start: func(exe string, _ roomLaunch) (*os.Process, string, error) {
			ran = readFile(t, exe)
			return exitedChild(t), "room.log", nil
		},
		up: func(*os.Process, string) bool { return true }}
	if !s.swap(l, newExe, 0, 0) {
		t.Fatal("swap said the new room is not up")
	}
	if ran != "new" || readFile(t, self) != "new" || readFile(t, swapSibling(self, "prev")) != "old" {
		t.Fatalf("started %q, installed %q, prev %q", ran, readFile(t, self), readFile(t, swapSibling(self, "prev")))
	}
	if _, err := os.Stat(newExe); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the staged build is still there: %v", err)
	}
}

func TestSwapPutsTheOldBuildBackWhenTheNewOneNeverAnswers(t *testing.T) {
	for _, tc := range []struct {
		name  string
		start func(string, roomLaunch) (*os.Process, string, error)
	}{
		{"never answers", func(string, roomLaunch) (*os.Process, string, error) { return exitedChild(t), "room.log", nil }},
		{"would not start", func(string, roomLaunch) (*os.Process, string, error) { return nil, "", errors.New("no") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, self, newExe, l := swapFixture(t)
			s := &swapper{self: self, start: tc.start, up: func(*os.Process, string) bool { return false }}
			start := time.Now()
			if s.swap(l, newExe, 0, 0) {
				t.Fatal("swap said a build that never answered is up")
			}
			if readFile(t, self) != "old" || readFile(t, swapSibling(self, "failed")) != "new" {
				t.Fatalf("installed %q, failed %q", readFile(t, self), readFile(t, swapSibling(self, "failed")))
			}
			if _, err := os.Stat(swapSibling(self, "prev")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("prev left behind: %v", err)
			}
			if !strings.Contains(readFile(t, filepath.Join(dir, restartLogName)), "going back to the old one") {
				t.Fatal("restart.log does not say it went back")
			}
			if time.Since(start) > 10*time.Second {
				t.Fatal("the rollback waited out a grace it did not need")
			}
		})
	}
}

func TestRestartArgsCarryTheSwap(t *testing.T) {
	got := strings.Join(roomLaunch{dir: "d", human: "h", agent: "a", swapIn: "C:/u/atrium.new.exe"}.restartArgs(), " ")
	if !strings.Contains(got, "--swap-in C:/u/atrium.new.exe") || !strings.Contains(got, "--swap-pid ") {
		t.Fatalf("restart args %q do not carry the swap", got)
	}
	if strings.Contains(strings.Join(roomLaunch{dir: "d", human: "h", agent: "a"}.startArgs(), " "), "--swap") {
		t.Fatal("a plain start carries a swap")
	}
}

func TestWriteRestartTokenIsFreshEachStart(t *testing.T) {
	dir := t.TempDir()
	a, err := writeRestartToken(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := writeRestartToken(dir)
	if a == "" || a == b || readFile(t, filepath.Join(dir, restartTokenName)) != b {
		t.Fatalf("tokens %q then %q, file %q", a, b, readFile(t, filepath.Join(dir, restartTokenName)))
	}
}
