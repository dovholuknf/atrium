//go:build integration

package api

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/cardproc"
	"github.com/dovholuknf/atrium/internal/store"
)

// longChild starts a process that runs for a minute, and reaps it when it ends so its pid really goes.
func longChild(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	if runtime.GOOS == "windows" {
		cmd = exec.Command("ping", "-n", "60", "127.0.0.1")
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = cmd.Wait() }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	return cmd
}

func gone(pid int) bool {
	for i := 0; i < 50; i++ {
		if _, err := cardproc.StartTime(pid); errors.Is(err, cardproc.ErrGone) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func askPorts(t *testing.T, oh *openHarness, card string, n int) (int, []int) {
	t.Helper()
	rec := post(t, oh.h, "/v1/tasks/"+card+"/ports", map[string]any{"count": n})
	var out struct {
		Ports []int `json:"ports"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out.Ports
}

// A PORT IS HANDED OUT ONCE: in the room's range, not reserved, not held by a card, binding free. Closing the card
// gives it back.
func TestPortsAreHandedOutOnceAndComeBackOnClose(t *testing.T) {
	oh := newOpenHarness(t, &fakeForge{})
	// Five free ports in a row, in a quiet area, not next to the ephemeral ones where neighbours come and go.
	lo := 0
	for p := 31000; p < 39000 && lo == 0; p += 5 {
		if bindsFree(p) && bindsFree(p+1) && bindsFree(p+2) && bindsFree(p+3) && bindsFree(p+4) {
			lo = p
		}
	}
	if lo == 0 {
		t.Skip("no five free ports in a row in 31000-39000")
	}
	busy := lo + 2
	l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(busy))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := oh.srv.st.SetSetting(SettingCardPorts, strconv.Itoa(lo)+"-"+strconv.Itoa(busy+2)); err != nil {
		t.Fatal(err)
	}
	was := excludedPorts
	excludedPorts = func() []cardproc.Range { return []cardproc.Range{{Lo: lo + 1, Hi: lo + 1}} }
	defer func() { excludedPorts = was }()

	a := cardIn(t, oh.srv.st, t.TempDir())
	code, got := askPorts(t, oh, a.ID, 2)
	if code != 200 || len(got) != 2 || got[0] != lo || got[1] != busy+1 {
		t.Fatalf("%d %v (lo %d, reserved %d, busy %d)", code, got, lo, lo+1, busy)
	}
	b, _, err := oh.srv.st.Register(store.Observed{WireName: "other", Worktree: filepath.ToSlash(t.TempDir()), Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if code, got := askPorts(t, oh, b.ID, 2); code != 409 || len(got) != 1 || got[0] != busy+2 {
		t.Fatalf("the second card: %d %v", code, got)
	}
	if code, out := closeCall(t, oh, a.ID, map[string]any{"confirm": true}); code != 200 || out.Left != 0 {
		t.Fatalf("the close: %d %+v", code, out)
	}
	if code, got := askPorts(t, oh, b.ID, 2); code != 200 || len(got) != 2 || got[0] != lo || got[1] != busy+1 {
		t.Errorf("after the close: %d %v", code, got)
	}
}

// The default range is a block picked from the room's name, below the dynamic range and clear of the previews.
func TestTheDefaultPortRangeIsTheRoomsOwnBlock(t *testing.T) {
	oh := newOpenHarness(t, &fakeForge{})
	seen := map[string]bool{}
	for _, room := range []string{"sg4", "sg4-control", "sa89", ""} {
		oh.srv.Room = room
		r, err := oh.srv.cardPortRange()
		if err != nil || r.Lo < cardPortBase || r.Hi >= 49152 || r.Hi-r.Lo != cardPortBlock-1 {
			t.Fatalf("%q: %v %v", room, r, err)
		}
		seen[r.String()] = true
	}
	if len(seen) < 3 {
		t.Errorf("the rooms share blocks: %v", seen)
	}
}

// A PROC IS STOPPED BY THE CLOSE, the tree, and only while its pid is the process recorded.
func TestCloseStopsAProcessTheCardOwns(t *testing.T) {
	oh := newOpenHarness(t, &fakeForge{})
	card := cardIn(t, oh.srv.st, t.TempDir())
	child := longChild(t)
	rec := post(t, oh.h, "/v1/tasks/"+card.ID+"/own", map[string]any{"kind": "proc", "ref": strconv.Itoa(child.Process.Pid)})
	if rec.Code != 201 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if rec := post(t, oh.h, "/v1/tasks/"+card.ID+"/own", map[string]any{"kind": "proc", "ref": strconv.Itoa(os.Getpid())}); rec.Code != 400 {
		t.Errorf("atrium's own pid: %d", rec.Code)
	}
	code, out := closeCall(t, oh, card.ID, map[string]any{"confirm": true})
	if code != 200 || out.Left != 0 {
		t.Fatalf("%d %+v", code, out)
	}
	if !gone(child.Process.Pid) {
		t.Error("the process is still running")
	}
}

// The sweep marks a proc row freed once its process exited.
func TestSweepFreesAProcessThatExited(t *testing.T) {
	oh := newOpenHarness(t, &fakeForge{})
	card := cardIn(t, oh.srv.st, t.TempDir())
	child := longChild(t)
	if rec := post(t, oh.h, "/v1/tasks/"+card.ID+"/own", map[string]any{"kind": "proc", "ref": strconv.Itoa(child.Process.Pid)}); rec.Code != 201 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	_ = child.Process.Kill()
	if !gone(child.Process.Pid) {
		t.Fatal("the child did not exit")
	}
	rep := sweepNow(t, oh)
	if len(rep.Freed) != 1 || rep.Freed[0].Kind != store.ResProc {
		t.Fatalf("freed %+v", rep.Freed)
	}
}

// A card owns only folders inside its own worktree (or the scratch folder), and not the worktree itself.
func TestOwnTakesADirOnlyInsideTheCardsWorktree(t *testing.T) {
	oh := newOpenHarness(t, &fakeForge{})
	wt := t.TempDir()
	card := cardIn(t, oh.srv.st, wt)
	inside := filepath.Join(wt, "build")
	mkDir(t, inside)
	for _, c := range []struct {
		ref  string
		want int
	}{{inside, 201}, {wt, 400}, {t.TempDir(), 400}, {"relative", 400}} {
		if rec := post(t, oh.h, "/v1/tasks/"+card.ID+"/own", map[string]any{"kind": "dir", "ref": c.ref}); rec.Code != c.want {
			t.Errorf("%s: %d %s", c.ref, rec.Code, rec.Body.String())
		}
	}
	if rec := post(t, oh.h, "/v1/tasks/"+card.ID+"/own", map[string]any{"kind": "worktree", "ref": wt}); rec.Code != 400 {
		t.Errorf("a worktree by hand: %d", rec.Code)
	}
}

func TestParseRangeRefusesNonsense(t *testing.T) {
	for _, bad := range []string{"", "5", "100-200", "3000-2000", "1024-70000", "a-b"} {
		if _, err := cardproc.ParseRange(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
	if r, err := cardproc.ParseRange(" 41000 - 41199 "); err != nil || r.Lo != 41000 || r.Hi != 41199 {
		t.Errorf("%v %v", r, err)
	}
}
