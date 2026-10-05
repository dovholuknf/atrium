package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dovholuknf/atrium/internal/forge"
)

// fakeForgeCLI stands in for the forge CLI: what it prints, whether it exits non-zero, and whether it is on PATH.
type fakeForgeCLI struct {
	mu      sync.Mutex
	runs    [][]string
	out     string
	fail    bool
	missing bool
}

func (f *fakeForgeCLI) install(t *testing.T) {
	oldLook, oldRun := preflightLook, preflightRun
	t.Cleanup(func() { preflightLook, preflightRun = oldLook, oldRun })
	preflightLook = func(name string) (string, error) {
		if f.missing {
			return "", errors.New("not found")
		}
		return "/fake/bin/" + name, nil
	}
	preflightRun = func(ctx context.Context, exe string, args []string, out io.Writer) error {
		f.mu.Lock()
		f.runs = append(f.runs, append([]string{exe}, args...))
		f.mu.Unlock()
		_, _ = io.WriteString(out, f.out)
		if f.fail {
			return errors.New("exit status 1")
		}
		return nil
	}
}

const ghOK = "github.com\n  ✓ Logged in to github.com account me (keyring)\n  - Token: gho_************************************\n  - Token scopes: 'read:org', 'repo'\n"

func forgeCheck(t *testing.T, d *Daemon, body string) forgeStatus {
	t.Helper()
	req := httptest.NewRequest("POST", "/v1/preflight", strings.NewReader(body))
	rec := httptest.NewRecorder()
	d.BoardHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d: %s", rec.Code, rec.Body.String())
	}
	var a preflightAnswer
	if err := json.Unmarshal(rec.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	for _, st := range a.Forges {
		return st
	}
	t.Fatalf("no forge in the answer: %s", rec.Body.String())
	return forgeStatus{}
}

func TestForgeOKRunsTheStatusCommandAndClearsTheAlert(t *testing.T) {
	d := testDaemon(t)
	f := &fakeForgeCLI{out: ghOK}
	f.install(t)
	st := forgeCheck(t, d, `{"forges":[{"tool":"gh","host":"github.com","scopes":["repo","read:org"]}]}`)
	if st.State != forgeOK {
		t.Fatalf("state %q: %+v", st.State, st)
	}
	if got := strings.Join(f.runs[0], " "); got != "/fake/bin/gh auth status --hostname github.com" {
		t.Errorf("ran %q", got)
	}
	if len(d.ForgeAlerts().([]ForgeAlert)) != 0 {
		t.Errorf("an ok check raised an alert")
	}
}

func TestForgeLoggedOutRaisesTheAlertWithTheCommandToRun(t *testing.T) {
	d := testDaemon(t)
	d.opts.Room = "sg3"
	f := &fakeForgeCLI{out: "You are not logged into any GitHub hosts.\n", fail: true}
	f.install(t)
	st := forgeCheck(t, d, `{"forges":[{"tool":"gh","host":"github.com"}]}`)
	want := "gh is not logged in on sg3: run `gh auth login --hostname github.com` on sg3"
	if st.State != forgeLoggedOut || st.Message != want {
		t.Fatalf("got %+v, want state logged_out and %q", st, want)
	}
	al := d.ForgeAlerts().([]ForgeAlert)
	if len(al) != 1 || al[0].Key != "gh@github.com" || al[0].Message != want {
		t.Fatalf("alerts %+v", al)
	}
	// Logging in and asking again ends it.
	f.fail, f.out = false, ghOK
	forgeCheck(t, d, `{"forges":[{"tool":"gh","host":"github.com"}]}`)
	if len(d.ForgeAlerts().([]ForgeAlert)) != 0 {
		t.Errorf("the alert stayed after a good check")
	}
}

func TestForgeNotInstalled(t *testing.T) {
	d := testDaemon(t)
	d.opts.Room = "sg3"
	f := &fakeForgeCLI{missing: true}
	f.install(t)
	st := forgeCheck(t, d, `{"forges":[{"tool":"gh","host":"github.com"}]}`)
	if st.State != forgeNotInstalled || !strings.Contains(st.Message, "gh is not installed on sg3") {
		t.Fatalf("got %+v", st)
	}
	if len(f.runs) != 0 {
		t.Errorf("ran %v though the command is not there", f.runs)
	}
}

func TestForgeMissingScopeNamesTheScope(t *testing.T) {
	d := testDaemon(t)
	d.opts.Room = "sg3"
	f := &fakeForgeCLI{out: "  - Token scopes: 'repo'\n"}
	f.install(t)
	st := forgeCheck(t, d, `{"forges":[{"tool":"gh","host":"github.com","scopes":["repo","read:org"]}]}`)
	want := "gh on sg3 is missing the scope read:org for github.com: run `gh auth refresh --hostname github.com --scopes read:org` on sg3"
	if st.State != forgeMissingScope || st.Message != want {
		t.Fatalf("got %+v, want %q", st, want)
	}
}

// A fine-grained token prints no scope list, so nothing can be said to be missing.
func TestForgeNoScopeLineIsNotAFailure(t *testing.T) {
	d := testDaemon(t)
	f := &fakeForgeCLI{out: "  ✓ Logged in to github.com\n"}
	f.install(t)
	if st := forgeCheck(t, d, `{"forges":[{"tool":"gh","host":"github.com","scopes":["repo"]}]}`); st.State != forgeOK {
		t.Fatalf("got %+v", st)
	}
}

// The command is the room's own setting. A body cannot name one, and a setting cannot be a path.
func TestForgeCommandOverrideIsTheRoomsAndABareName(t *testing.T) {
	d := testDaemon(t)
	if err := d.st.SetForgeConfig("gh", "github.com", "gh-work"); err != nil {
		t.Fatal(err)
	}
	f := &fakeForgeCLI{out: ghOK}
	f.install(t)
	forgeCheck(t, d, `{"forges":[{"tool":"gh","host":"github.com","command":"rm"}]}`)
	if got := f.runs[0][0]; got != "/fake/bin/gh-work" {
		t.Errorf("ran %q, want the configured name", got)
	}
	for _, bad := range []string{"/usr/bin/gh", `..\gh`, "gh --show-token", "-x"} {
		if err := d.st.SetForgeConfig("gh", "github.com", bad); err == nil {
			t.Errorf("accepted %q as a command name", bad)
		}
	}
	if err := d.st.SetForgeConfig("gh", "https://github.com/x", ""); err == nil {
		t.Errorf("accepted a URL as a host")
	}
	if err := d.st.SetForgeConfig("rm", "", ""); err == nil {
		t.Errorf("accepted an unknown forge")
	}
}

func TestForgeUnknownToolAndBadHostRunNothing(t *testing.T) {
	d := testDaemon(t)
	f := &fakeForgeCLI{out: ghOK}
	f.install(t)
	forgeCheck(t, d, `{"forges":[{"tool":"rm","host":"x.org"}]}`)
	forgeCheck(t, d, `{"forges":[{"tool":"gh","host":"--show-token"}]}`)
	if len(f.runs) != 0 {
		t.Errorf("ran %v", f.runs)
	}
}

// The hook the forge package's AccessError will be wired to.
func TestRaiseForgeAccessRaisesTheSameAlert(t *testing.T) {
	d := testDaemon(t)
	d.opts.Room = "sg3"
	d.RaiseForgeAccess("gh", "github.com", "gh: To get started with GitHub CLI, please run: gh auth login")
	d.RaiseForgeAccess("gh", "github.com", "again")
	al := d.ForgeAlerts().([]ForgeAlert)
	if len(al) != 1 || al[0].State != forgeLoggedOut ||
		al[0].Message != "gh is not logged in on sg3: run `gh auth login --hostname github.com` on sg3" {
		t.Fatalf("alerts %+v", al)
	}
	d.RaiseForgeAccess("gh", "github.com", `exec: "gh": executable file not found in $PATH`)
	if al = d.ForgeAlerts().([]ForgeAlert); al[0].State != forgeNotInstalled {
		t.Fatalf("alerts %+v", al)
	}
	d.RaiseForgeAccess("rm", "x.org", "nope")
	if len(d.ForgeAlerts().([]ForgeAlert)) != 1 {
		t.Errorf("an unknown tool raised an alert")
	}
}

func TestForgeScopesIn(t *testing.T) {
	have, ok := forgeScopesIn("x\n  - Token scopes: 'gist', 'read:org', 'repo'\r\n")
	if !ok || !have["repo"] || !have["read:org"] || !have["gist"] || have["admin"] {
		t.Fatalf("%v %v", have, ok)
	}
	if _, ok := forgeScopesIn("Logged in\n"); ok {
		t.Fatalf("read scopes from nothing")
	}
}

func TestForgeAccessFromRaisesAndForgeWorkedClears(t *testing.T) {
	d := testDaemon(t)
	d.opts.Room = "sg3"
	if d.prr.onAccess == nil || d.prr.onWorked == nil || d.ap.ForgeFailed == nil || d.ap.ForgeWorked == nil {
		t.Fatal("the daemon did not fill the forge seams")
	}
	if d.ForgeAccessFrom("github", errors.New("boom")) || len(d.ForgeAlerts().([]ForgeAlert)) != 0 {
		t.Fatal("a plain error raised an alert")
	}
	// A wrapper command still raises the alert of its tool, and a wrapped error is found.
	err := fmt.Errorf("fetch: %w", &forge.AccessError{Tool: "ghw", Host: "ghe.example", Detail: "gh auth login"})
	if !d.ForgeAccessFrom("github", err) {
		t.Fatal("not recognised")
	}
	al := d.ForgeAlerts().([]ForgeAlert)
	if len(al) != 1 || al[0].Key != "gh@ghe.example" {
		t.Fatalf("alerts %+v", al)
	}
	d.ForgeWorked("github", "other.example")
	if len(d.ForgeAlerts().([]ForgeAlert)) != 1 {
		t.Fatal("another host cleared it")
	}
	d.ForgeWorked("github", "ghe.example")
	if len(d.ForgeAlerts().([]ForgeAlert)) != 0 {
		t.Fatal("not cleared")
	}
}
