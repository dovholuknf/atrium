//go:build integration

package link

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// postGate sends one ask or poll and decodes the answer.
func postGate(t *testing.T, url, body string) (int, map[string]any) {
	t.Helper()
	res, err := http.Post(url+"/_hub/restart", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

// A RE-POLLING SCRIPT'S ASK outlives each request. Every request is held for
// at most `hold`, a pause holds the ask past its wait, and the poll after the
// resume collects `go`.
func TestAHeldAskIsPolledThroughAPause(t *testing.T) {
	p := NewProxy(NewHub(Timings{}), nil, "", nil)
	front := httptest.NewServer(p)
	defer front.Close()
	events, shut := listen(t, front.URL+"/v1/events/hub")
	defer shut()

	ask := make(chan map[string]any, 1)
	go func() {
		_, out := postGate(t, front.URL, `{"countdown":5,"idle":0.05,"wait":0.3,"hold":0.2}`)
		ask <- out
	}()
	waitEvent(t, events, restartEvent)
	if _, err := http.Post(front.URL+"/_hub/restart/pause", "application/json", nil); err != nil {
		t.Fatal(err)
	}
	first := <-ask
	if first["answer"] != "waiting" || first["ask"] == "" {
		t.Fatalf("the first request of a held ask answered %v", first)
	}
	id := first["ask"].(string)

	// Well past the 0.3s wait, one short poll at a time.
	started := time.Now()
	for time.Since(started) < time.Second {
		code, out := postGate(t, front.URL, `{"ask":"`+id+`","hold":0.2}`)
		if code != http.StatusOK || out["answer"] != "waiting" || out["paused"] != true {
			t.Fatalf("a poll during the pause answered %d %v", code, out)
		}
	}
	if code, _ := postGate(t, front.URL, `{"countdown":1,"idle":0.05,"wait":1,"hold":0.2}`); code != http.StatusConflict {
		t.Errorf("a second ask while one is held answered %d, not 409", code)
	}
	if _, err := http.Post(front.URL+"/_hub/restart/resume", "application/json", nil); err != nil {
		t.Fatal(err)
	}
	// The countdown is 5s, so this takes a few polls.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		code, out := postGate(t, front.URL, `{"ask":"`+id+`","hold":1}`)
		if code != http.StatusOK {
			t.Fatalf("a poll after the resume answered %d %v", code, out)
		}
		if out["answer"] == "go" {
			// Collected, so the ask is gone and its id is not known any more.
			if code, _ := postGate(t, front.URL, `{"ask":"`+id+`","hold":0.2}`); code != http.StatusGone {
				t.Errorf("a poll after the answer was collected answered %d, not 410", code)
			}
			return
		}
		if out["answer"] != "waiting" {
			t.Fatalf("a poll after the resume answered %v", out)
		}
	}
	t.Fatal("a resumed ask never said go")
}

// An ask whose script stops polling is dropped after the lease, and its
// countdown comes off the boards.
func TestAHeldAskWithNobodyPollingIsDropped(t *testing.T) {
	p := NewProxy(NewHub(Timings{}), nil, "", nil)
	p.restart.lease = 200 * time.Millisecond
	front := httptest.NewServer(p)
	defer front.Close()
	events, shut := listen(t, front.URL+"/v1/events/hub")
	defer shut()

	ask := make(chan map[string]any, 1)
	go func() {
		_, out := postGate(t, front.URL, `{"countdown":30,"idle":0.05,"wait":60,"hold":0.1}`)
		ask <- out
	}()
	if e := waitEvent(t, events, restartEvent); fields(t, e.Data)["state"] != "countdown" {
		t.Fatalf("the first event said %s", e.Data)
	}
	id := (<-ask)["ask"].(string)
	if e := waitEvent(t, events, restartEvent); fields(t, e.Data)["state"] != "cancelled" {
		t.Fatalf("an ask nobody polled said %s, not cancelled", e.Data)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		code, _ := postGate(t, front.URL, `{"ask":"`+id+`","hold":0.1}`)
		if code == http.StatusGone {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("a dropped ask still answered %d", code)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st := p.restart.state(); st["waiting"] != false {
		t.Errorf("a dropped ask still holds the slot: %v", st)
	}
}

// runGateScript runs scripts/hub-restart-gate.ps1 against a hub. It is a PowerShell
// script, so it runs on Windows only and is skipped there when pwsh is not installed.
func runGateScript(t *testing.T, hub string, args ...string) (chan int, chan string) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("the gate script is PowerShell, and pwsh is not started off Windows")
	}
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("pwsh is not installed")
	}
	script, _ := filepath.Abs(filepath.Join("..", "..", "scripts", "hub-restart-gate.ps1"))
	cmd := exec.Command(pwsh, append([]string{"-NoProfile", "-File", script, "-Hub", hub}, args...)...)
	code, said := make(chan int, 1), make(chan string, 1)
	go func() {
		out, err := cmd.CombinedOutput()
		said <- string(out)
		var ee *exec.ExitError
		switch {
		case err == nil:
			code <- 0
		case errors.As(err, &ee):
			code <- ee.ExitCode()
		default:
			code <- -1
		}
	}()
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})
	return code, said
}

func exitOf(t *testing.T, code chan int, said chan string, within time.Duration) (int, string) {
	t.Helper()
	select {
	case c := <-code:
		return c, <-said
	case <-time.After(within):
		t.Fatal("the gate script never exited")
		return 0, ""
	}
}

// THE SCRIPT WAITS OUT A PAUSE LONGER THAN ITS WAIT, re-polling, and exits 0
// once the board resumes.
func TestTheGateScriptHoldsThroughAPauseThenGoes(t *testing.T) {
	p := NewProxy(NewHub(Timings{}), nil, "", nil)
	// Stopped until the pause, so the 0.3s countdown cannot run out and say go
	// while the countdown event is still on its way to this test.
	clock := stopClock(p.restart)
	var polls atomic.Int64
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/_hub/restart" {
			polls.Add(1)
		}
		p.ServeHTTP(w, r)
	}))
	defer front.Close()
	events, shut := listen(t, front.URL+"/v1/events/hub")
	defer shut()

	const wait, hold = time.Second, 300 * time.Millisecond
	code, said := runGateScript(t, front.URL, "-Countdown", "0.3", "-Idle", "0.05", "-Wait", "1", "-Hold", "0.3")
	waitEvent(t, events, restartEvent)
	p.restart.pause()
	clock.start()
	// Held past its wait: the wait and a hold have gone by since the pause, and
	// the script has polled again after that, so it did not stop at its wait.
	paused, before := time.Now(), polls.Load()
	for time.Since(paused) < wait+hold || polls.Load() < before+int64((wait+hold)/hold)+1 {
		select {
		case c := <-code:
			t.Fatalf("the script exited %d during the pause: %s", c, <-said)
		case <-time.After(20 * time.Millisecond):
		}
		if time.Since(paused) > 30*time.Second {
			t.Fatalf("the script polled %d times in 30s of pause", polls.Load()-before)
		}
	}
	p.restart.resume()
	c, out := exitOf(t, code, said, 30*time.Second)
	if c != 0 || !strings.Contains(out, "go:") {
		t.Fatalf("after the resume the script exited %d: %s", c, out)
	}
	if !strings.Contains(out, "paused from the board") {
		t.Errorf("the script did not say it was held: %s", out)
	}
}

// A HUB THAT DIES WHILE THE RESTART IS HELD is not a go. The script says so and
// exits non-zero rather than polling forever.
func TestTheGateScriptExitsWhenTheHubDiesWhileHeld(t *testing.T) {
	p := NewProxy(NewHub(Timings{}), nil, "", nil)
	front := httptest.NewServer(p)
	events, shut := listen(t, front.URL+"/v1/events/hub")
	defer shut()

	code, said := runGateScript(t, front.URL, "-Countdown", "5", "-Idle", "0.05", "-Wait", "1", "-Hold", "0.3")
	waitEvent(t, events, restartEvent)
	p.restart.pause()
	time.Sleep(2 * time.Second)
	front.CloseClientConnections()
	front.Close()
	c, out := exitOf(t, code, said, 30*time.Second)
	if c == 0 {
		t.Fatalf("the script exited 0 after the hub died while held: %s", out)
	}
	if !strings.Contains(out, "went away") {
		t.Errorf("the script did not say the hub went away: %s", out)
	}
}

// A hub that restarted has forgotten the ask. The script is told 410 and exits
// non-zero.
func TestTheGateScriptExitsWhenTheHubForgetsTheAsk(t *testing.T) {
	p := NewProxy(NewHub(Timings{}), nil, "", nil)
	front := httptest.NewServer(p)
	defer front.Close()
	events, shut := listen(t, front.URL+"/v1/events/hub")
	defer shut()

	code, said := runGateScript(t, front.URL, "-Countdown", "5", "-Idle", "0.05", "-Wait", "1", "-Hold", "0.3")
	waitEvent(t, events, restartEvent)
	p.restart.pause()
	time.Sleep(time.Second)
	// What a new hub knows about the old one's ask: nothing.
	p.restart.mu.Lock()
	a := p.restart.held
	p.restart.held = nil
	p.restart.mu.Unlock()
	if a != nil {
		a.cancel()
	}
	c, out := exitOf(t, code, said, 30*time.Second)
	if c == 0 || !strings.Contains(out, "forgot the ask") {
		t.Fatalf("after the hub forgot the ask the script exited %d: %s", c, out)
	}
}
