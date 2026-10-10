//go:build integration

package api

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/cardproc"
	"github.com/dovholuknf/atrium/internal/store"
)

// fakeOverlay makes what an up leaves behind without ziti: the folder, the state file, the row, an identity, and a
// process standing in for the quickstart.
func fakeOverlay(t *testing.T, oh *openHarness, card, name string) (home string, proc *exec.Cmd) {
	t.Helper()
	home, err := overlayHome(card, name)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "identities"), 0o700); err != nil {
		t.Fatal(err)
	}
	ident := filepath.Join(home, "identities", "alice.json")
	if err := os.WriteFile(ident, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := oh.srv.st.AddResource(card, store.ResOverlay, filepath.ToSlash(home), "https://127.0.0.1:1"); err != nil {
		t.Fatal(err)
	}
	proc = longChild(t)
	rec := post(t, oh.h, "/v1/tasks/"+card+"/own", map[string]any{"kind": "proc", "ref": strconv.Itoa(proc.Process.Pid)})
	if rec.Code != 201 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var own struct {
		Resource store.CardResource `json:"resource"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &own)
	idRow, err := oh.srv.st.AddResource(card, store.ResIdentity, filepath.ToSlash(ident), filepath.ToSlash(home))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeOverlayState(home, &overlayState{Name: name, Rows: []int{own.Resource.Seq, idRow.Seq}}); err != nil {
		t.Fatal(err)
	}
	return home, proc
}

func withCardsDir(t *testing.T) {
	t.Helper()
	was := CardsDir
	CardsDir = t.TempDir()
	t.Cleanup(func() { CardsDir = was })
}

func liveKinds(t *testing.T, oh *openHarness, card string) map[string]int {
	t.Helper()
	rows, err := oh.srv.st.Resources(card)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]int{}
	for _, r := range rows {
		if r.Live() {
			out[r.Kind]++
		}
	}
	return out
}

// CLOSING THE CARD STOPS ITS OVERLAY, then deletes the folder, PKI and identities with it.
func TestCloseStopsAndDeletesTheCardsOverlay(t *testing.T) {
	withCardsDir(t)
	oh := newOpenHarness(t, &fakeForge{})
	card := cardIn(t, oh.srv.st, t.TempDir())
	home, proc := fakeOverlay(t, oh, card.ID, "o1")

	code, out := closeCall(t, oh, card.ID, map[string]any{"confirm": true})
	if code != 200 || out.Left != 0 {
		t.Fatalf("%d %+v", code, out)
	}
	if !gone(proc.Process.Pid) {
		t.Error("the overlay's process is still running")
	}
	if _, err := os.Stat(home); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the overlay's folder is still there: %v", err)
	}
}

// A DOWN STOPS ONE OVERLAY and leaves the card's others running.
func TestOverlayDownStopsOnlyThatOne(t *testing.T) {
	withCardsDir(t)
	oh := newOpenHarness(t, &fakeForge{})
	card := cardIn(t, oh.srv.st, t.TempDir())
	home1, p1 := fakeOverlay(t, oh, card.ID, "o1")
	home2, p2 := fakeOverlay(t, oh, card.ID, "o2")

	if rec := post(t, oh.h, "/v1/tasks/"+card.ID+"/overlay", map[string]any{"action": "down"}); rec.Code != 400 ||
		!strings.Contains(rec.Body.String(), "more than one") {
		t.Fatalf("a down that names none of two: %d %s", rec.Code, rec.Body.String())
	}
	if rec := post(t, oh.h, "/v1/tasks/"+card.ID+"/overlay", map[string]any{"action": "down", "overlay": "o1"}); rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if !gone(p1.Process.Pid) {
		t.Error("o1's process is still running")
	}
	if _, err := os.Stat(home1); !errors.Is(err, os.ErrNotExist) {
		t.Error("o1's folder is still there")
	}
	if _, err := cardproc.StartTime(p2.Process.Pid); err != nil {
		t.Errorf("o2's process stopped too: %v", err)
	}
	if _, err := os.Stat(home2); err != nil {
		t.Errorf("o2's folder went too: %v", err)
	}
	if k := liveKinds(t, oh, card.ID); k[store.ResOverlay] != 1 || k[store.ResIdentity] != 1 || k[store.ResProc] != 1 {
		t.Errorf("live rows after the down: %v", k)
	}
}

// A TUNNELER IN TUN MODE IS REFUSED with the command to ask clint to run elevated. atrium never elevates.
func TestATunModeTunnelerIsRefusedWithTheCommand(t *testing.T) {
	withCardsDir(t)
	oh := newOpenHarness(t, &fakeForge{})
	card := cardIn(t, oh.srv.st, t.TempDir())
	fakeOverlay(t, oh, card.ID, "o1")
	rec := post(t, oh.h, "/v1/tasks/"+card.ID+"/overlay", map[string]any{"action": "tunnel", "identity": "alice", "mode": "tun"})
	var out struct {
		Code    string `json:"code"`
		Command string `json:"command"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != 400 || out.Code != "needs_elevation" || !strings.Contains(out.Command, "alice.json") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if rec := post(t, oh.h, "/v1/tasks/"+card.ID+"/overlay", map[string]any{"action": "tunnel", "identity": "bob"}); rec.Code != 400 {
		t.Errorf("an identity the overlay does not have: %d", rec.Code)
	}
}

// A name is a folder name, and a card with no cards folder holds no overlay.
func TestOverlayNamesAreFolderNames(t *testing.T) {
	withCardsDir(t)
	oh := newOpenHarness(t, &fakeForge{})
	card := cardIn(t, oh.srv.st, t.TempDir())
	for _, name := range []string{"../up", "A", "a b", strings.Repeat("x", 33)} {
		if rec := post(t, oh.h, "/v1/tasks/"+card.ID+"/overlay", map[string]any{"action": "up", "name": name}); rec.Code != 400 {
			t.Errorf("%q: %d", name, rec.Code)
		}
	}
	CardsDir = ""
	if rec := post(t, oh.h, "/v1/tasks/"+card.ID+"/overlay", map[string]any{"action": "up"}); rec.Code != 503 {
		t.Errorf("no cards folder: %d", rec.Code)
	}
}

// The sweep frees an overlay and an identity whose files are gone.
func TestSweepFreesAGoneOverlay(t *testing.T) {
	withCardsDir(t)
	oh := newOpenHarness(t, &fakeForge{})
	card := cardIn(t, oh.srv.st, t.TempDir())
	home, proc := fakeOverlay(t, oh, card.ID, "o1")
	_ = proc.Process.Kill()
	if err := os.RemoveAll(home); err != nil {
		t.Fatal(err)
	}
	if err := oh.srv.st.SetStatusBecause(card.ID, store.StatusDone, "test"); err != nil {
		t.Fatal(err)
	}
	rep := sweepNow(t, oh)
	kinds := map[string]bool{}
	for _, f := range rep.Freed {
		kinds[f.Kind] = true
	}
	if !kinds[store.ResOverlay] || !kinds[store.ResIdentity] {
		t.Fatalf("freed %+v", rep.Freed)
	}
}

// THE REAL THING, where ziti is installed and TEST_ZITI_QUICKSTART=1 (an ATRIUM_ name is scrubbed by TestMain): up,
// an identity, a host tunneler, then the close.
func TestARealOverlayComesUpAndGoesWithTheCard(t *testing.T) {
	if os.Getenv("TEST_ZITI_QUICKSTART") != "1" {
		t.Skip("set TEST_ZITI_QUICKSTART=1 to run a real quickstart")
	}
	if _, err := zitiBin(); err != nil {
		t.Skip("ziti is not on the PATH")
	}
	withCardsDir(t)
	oh := newOpenHarness(t, &fakeForge{})
	card := cardIn(t, oh.srv.st, t.TempDir())
	rec := post(t, oh.h, "/v1/tasks/"+card.ID+"/overlay", map[string]any{"action": "up"})
	var up struct {
		Home  string `json:"home"`
		Ready bool   `json:"ready"`
		PID   int    `json:"pid"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &up)
	// A failure below must not leave a quickstart running.
	t.Cleanup(func() {
		if up.PID > 0 {
			_ = cardproc.StopTree(up.PID)
		}
	})
	if rec.Code != 201 || !up.Ready {
		t.Fatalf("up: %d %s", rec.Code, rec.Body.String())
	}
	rec = post(t, oh.h, "/v1/tasks/"+card.ID+"/overlay", map[string]any{"action": "identity", "name": "alice", "roles": []string{"test"}})
	if rec.Code != 201 {
		t.Fatalf("identity: %d %s", rec.Code, rec.Body.String())
	}
	rec = post(t, oh.h, "/v1/tasks/"+card.ID+"/overlay", map[string]any{"action": "tunnel", "identity": "alice"})
	var tun struct {
		PID int `json:"pid"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &tun)
	t.Cleanup(func() {
		if tun.PID > 0 {
			_ = cardproc.StopTree(tun.PID)
		}
	})
	if rec.Code != 201 {
		t.Fatalf("tunnel: %d %s", rec.Code, rec.Body.String())
	}
	code, out := closeCall(t, oh, card.ID, map[string]any{"confirm": true})
	if code != 200 || out.Left != 0 {
		t.Fatalf("close: %d %+v", code, out)
	}
	if !gone(up.PID) || !gone(tun.PID) {
		t.Error("a process outlived the close")
	}
	if _, err := os.Stat(filepath.FromSlash(up.Home)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the folder outlived the close: %v", err)
	}
}
