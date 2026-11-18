package cli

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dovholuknf/atrium/internal/base/nowindow"
	"github.com/dovholuknf/atrium/internal/link/wire"
)

// POST /v1/restart-into is a deploy asking, through the hub, for this room to restart into a build it copied here.
// It is on the link's handler only, and the token in restart.token says the asker is this machine's operator: the
// hub cannot read it, ssh as the room's user can. The detached restarter is the old binary, so a new build that
// never answers is put aside as .failed and the restarter runs the room itself.

const (
	restartTokenName = "restart.token"
	swapProbeWait    = 30 * time.Second
	swapUpWait       = 90 * time.Second
)

var (
	swapIn  string
	swapPid int
)

// swapExeName is the only name a build may be swapped in from: the deploy's atrium.new.exe or atrium-zrok.new.exe.
var swapExeName = regexp.MustCompile(`(?i)^atrium[a-z0-9_-]*\.new(\.exe)?$`)

type restartIntoAsk struct {
	Exe       string `json:"exe"`
	Why       string `json:"why,omitempty"`
	Force     bool   `json:"force,omitempty"`
	Immediate bool   `json:"immediate,omitempty"`
}

// restartIntoRoute serves POST /v1/restart-into ahead of the link's handler and passes everything else on.
func restartIntoRoute(next http.Handler, boardURL string, l roomLaunch, stop func()) http.Handler {
	token, err := writeRestartToken(l.dir)
	if err != nil {
		restartLog(l.dir, "no restart token, so restart-into is refused: %v", err)
	}
	var busy atomic.Bool
	dirs := swapDirs()
	h := restartInto(token, dirs, probeSwapExe, func(exe string, ask restartIntoAsk) error {
		if serviceRestart != nil {
			return errors.New("this room runs as a service, which restarts its own binary: swap it through the service")
		}
		if !busy.CompareAndSwap(false, true) {
			return errors.New("a restart-into is already in progress")
		}
		into := l
		into.swapIn = exe
		go func() {
			defer busy.Store(false)
			onHubRestart(boardURL, into, stop)(wire.RestartAsk{Why: ask.Why, Force: ask.Force, Immediate: ask.Immediate})
		}()
		return nil
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/restart-into" {
			h(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func restartInto(token string, dirs []string, probe func(string) error,
	start func(string, restartIntoAsk) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		say := func(code int, msg string) {
			w.WriteHeader(code)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": msg})
		}
		if r.Method != http.MethodPost {
			say(http.StatusMethodNotAllowed, "restart-into is a POST")
			return
		}
		got := r.Header.Get("X-Atrium-Token")
		if token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			say(http.StatusForbidden, "restart-into takes the token in "+restartTokenName+" beside the room's keys")
			return
		}
		var ask restartIntoAsk
		if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&ask); err != nil {
			say(http.StatusBadRequest, "the body is {\"exe\": \"<path>\"}: "+err.Error())
			return
		}
		exe, err := allowedSwapExe(ask.Exe, dirs)
		if err != nil {
			say(http.StatusBadRequest, err.Error())
			return
		}
		if err := probe(exe); err != nil {
			say(http.StatusUnprocessableEntity, "the new build did not run, nothing was stopped: "+err.Error())
			return
		}
		if err := start(exe, ask); err != nil {
			say(http.StatusConflict, err.Error())
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{"scheduled": true, "exe": filepath.ToSlash(exe)})
	}
}

func writeRestartToken(dir string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(b)
	if err := os.WriteFile(filepath.Join(dir, restartTokenName), []byte(tok), 0o600); err != nil {
		return "", err
	}
	return tok, nil
}

// swapDirs is where a build may be swapped in from: the home folder the deploy copies to, and the installed binary's.
func swapDirs() []string {
	var dirs []string
	if h, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, h)
	}
	if self, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(self))
	}
	return dirs
}

// allowedSwapExe answers the resolved path of raw, or why it may not be swapped in. Both the path as given and where
// its links lead must be an atrium*.new name directly in one of dirs.
func allowedSwapExe(raw string, dirs []string) (string, error) {
	if strings.TrimSpace(raw) == "" || !filepath.IsAbs(raw) {
		return "", fmt.Errorf("exe must be an absolute path, got %q", raw)
	}
	p := filepath.Clean(raw)
	real, err := swapRealPath(p)
	if err != nil {
		return "", fmt.Errorf("exe %s: %w", p, err)
	}
	trusted := ""
	for _, c := range []string{p, real} {
		if !swapExeName.MatchString(filepath.Base(c)) {
			return "", fmt.Errorf("exe %s is not an atrium*.new build", c)
		}
		t, ok := inSwapDir(filepath.Dir(c), dirs)
		if !ok {
			return "", fmt.Errorf("exe %s is not in the home folder or the installed binary's folder", c)
		}
		trusted = t
	}
	// Stat a path built from the trusted folder and a local name, not the request string.
	name := filepath.Base(real)
	if !filepath.IsLocal(name) {
		return "", fmt.Errorf("exe %s is not a plain file name", real)
	}
	st, err := os.Stat(filepath.Join(trusted, name))
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", fmt.Errorf("exe %s is not a file", real)
	}
	return real, nil
}

func inSwapDir(dir string, dirs []string) (string, bool) {
	for _, d := range dirs {
		cands := []string{filepath.Clean(d)}
		if r, err := swapRealPath(d); err == nil {
			cands = append(cands, filepath.Clean(r))
		}
		for _, c := range cands {
			if dir == c || (runtime.GOOS == "windows" && strings.EqualFold(dir, c)) {
				return c, true
			}
		}
	}
	return "", false
}

func probeSwapExe(exe string) error {
	ctx, cancel := context.WithTimeout(context.Background(), swapProbeWait)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "version")
	nowindow.Hide(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s version: %v: %s", exe, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// swapRoomBinary is the restarter's part: wait for the old room, swap newExe in, start it detached and wait for it to
// answer. True means the new room is up and this process is done. False means this process, the old binary, runs the
// room, and the old binary is back under its own name.
func swapRoomBinary(l roomLaunch, newExe string, oldPid int, after time.Duration) bool {
	self, err := os.Executable()
	if err != nil {
		restartLog(l.dir, "restart-into: cannot name this binary (%v), starting the old room", err)
		return false
	}
	return (&swapper{self: self, start: startRoomFrom, up: roomCameUp}).swap(l, newExe, oldPid, after)
}

type swapper struct {
	self  string
	start func(string, roomLaunch) (*os.Process, string, error)
	up    func(p *os.Process, human string) bool
}

func (s *swapper) swap(l roomLaunch, newExe string, oldPid int, after time.Duration) bool {
	restartLog(l.dir, "restart-into: waiting for the previous room to release %s", l.human)
	if !waitForRoomRestart(after, l.human) {
		restartLog(l.dir, "restart-into: %s still held after %s, the new build was not swapped in", l.human, roomStopGrace)
		return false
	}
	for deadline := time.Now().Add(roomStopGrace); oldPid > 0 && pidAlive(oldPid) && time.Now().Before(deadline); {
		time.Sleep(250 * time.Millisecond)
	}
	prev := swapSibling(s.self, "prev")
	setAside(prev)
	if err := os.Rename(s.self, prev); err != nil {
		restartLog(l.dir, "restart-into: could not move %s aside (%v), starting the old room", s.self, err)
		return false
	}
	if err := os.Rename(newExe, s.self); err != nil {
		restartLog(l.dir, "restart-into: could not put %s in place (%v), starting the old room", newExe, err)
		s.putBack(l, prev)
		return false
	}
	restartLog(l.dir, "restart-into: swapped %s in, old kept as %s, starting it", newExe, prev)
	p, logPath, err := s.start(s.self, l)
	if err == nil && s.up(p, l.human) {
		restartLog(l.dir, "restart-into: the new room is up as pid %d, logging to %s", p.Pid, logPath)
		_ = p.Release()
		return true
	}
	if err != nil {
		restartLog(l.dir, "restart-into: the new build would not start (%v), going back to the old one", err)
	} else {
		restartLog(l.dir, "restart-into: the new room (pid %d) did not answer at %s within %s, going back to the old one",
			p.Pid, l.human, swapUpWait)
		_ = p.Kill()
		_ = waitForRoomRestart(0, l.human)
	}
	failed := swapSibling(s.self, "failed")
	_ = os.Remove(failed)
	if err := os.Rename(s.self, failed); err != nil {
		restartLog(l.dir, "restart-into: could not move the failed build aside (%v)", err)
	}
	s.putBack(l, prev)
	return false
}

func (s *swapper) putBack(l roomLaunch, prev string) {
	if err := os.Rename(prev, s.self); err != nil {
		restartLog(l.dir, "restart-into: could not rename %s back to %s (%v), this process runs the room anyway",
			prev, s.self, err)
	}
}

// swapSibling is atrium.<tag>.exe beside atrium.exe, or atrium.<tag> beside atrium.
func swapSibling(self, tag string) string {
	ext := filepath.Ext(self)
	return strings.TrimSuffix(self, ext) + "." + tag + ext
}

// setAside clears the way for a new prev. A running one cannot be removed on Windows but can be renamed, so it is
// moved to a stamped name, and stamped ones no longer running are swept.
func setAside(prev string) {
	ext := filepath.Ext(prev)
	stem := strings.TrimSuffix(prev, ext)
	if err := os.Remove(prev); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = os.Rename(prev, stem+"-"+time.Now().Format("20060102150405")+ext)
	}
	old, _ := filepath.Glob(stem + "-*" + ext)
	for _, o := range old {
		_ = os.Remove(o)
	}
}

func roomCameUp(p *os.Process, human string) bool {
	health := "http://" + human + "/v1/health"
	for deadline := time.Now().Add(swapUpWait); time.Now().Before(deadline); {
		if answers(health) {
			return true
		}
		if !pidAlive(p.Pid) {
			return false
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}
