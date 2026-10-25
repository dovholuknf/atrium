//go:build integration

package api

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/base/cardproc"
)

func startProc(t *testing.T, oh *openHarness, card string, body map[string]any) (int, struct {
	Pid int    `json:"pid"`
	Seq int    `json:"seq"`
	Log string `json:"log"`
}) {
	t.Helper()
	rec := post(t, oh.h, "/v1/tasks/"+card+"/proc", body)
	var out struct {
		Pid int    `json:"pid"`
		Seq int    `json:"seq"`
		Log string `json:"log"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func sleeper() []string {
	if runtime.GOOS == "windows" {
		return []string{"ping", "-n", "60", "127.0.0.1"}
	}
	return []string{"sleep", "60"}
}

// A PROCESS THE ROOM STARTS IS THE CARD'S: it is in the inventory at once, and closing the card stops it.
func TestCloseStopsAProcessTheRoomStartedForTheCard(t *testing.T) {
	oh := newOpenHarness(t, &fakeForge{})
	card := cardIn(t, oh.srv.st, t.TempDir())
	code, out := startProc(t, oh, card.ID, map[string]any{"argv": sleeper(), "name": "ctl"})
	if code != 201 || out.Pid == 0 || out.Seq == 0 {
		t.Fatalf("%d %+v", code, out)
	}
	t.Cleanup(func() { _ = cardproc.StopTree(out.Pid) })
	if !strings.Contains(filepath.Base(out.Log), cardShortID(card.ID)+"-ctl") {
		t.Errorf("the log %s is not named for the card", out.Log)
	}
	rows, err := oh.srv.st.Resources(card.ID)
	if err != nil || len(rows) != 1 || rows[0].Kind != "proc" || !rows[0].Live() || rows[0].Detail == "" {
		t.Fatalf("%+v %v", rows, err)
	}
	if code, res := closeCall(t, oh, card.ID, map[string]any{"confirm": true}); code != 200 || res.Left != 0 {
		t.Fatalf("%d %+v", code, res)
	}
	if !gone(out.Pid) {
		t.Error("the process is still running")
	}
}

// What cannot start is refused, and nothing is recorded.
func TestProcStartRefusals(t *testing.T) {
	oh := newOpenHarness(t, &fakeForge{})
	wt := t.TempDir()
	card := cardIn(t, oh.srv.st, wt)
	for name, body := range map[string]map[string]any{
		"no argv":      {"argv": []string{}},
		"no such prog": {"argv": []string{"atrium-no-such-program-xyz"}},
		"outside":      {"argv": sleeper(), "cwd": t.TempDir()},
		"relative":     {"argv": sleeper(), "cwd": "sub"},
	} {
		if code, _ := startProc(t, oh, card.ID, body); code != 400 {
			t.Errorf("%s: %d", name, code)
		}
	}
	if rows, _ := oh.srv.st.Resources(card.ID); len(rows) != 0 {
		t.Errorf("recorded %+v", rows)
	}
}
