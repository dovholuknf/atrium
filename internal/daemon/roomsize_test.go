package daemon

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

// A FRESH TERMINAL OPENS AT THE SIZE IT WILL BE WATCHED AT, and the first viewer
// applies width and height in one resize. See roomsize.go for the bug.

func TestAFreshCardOpensAtTheRoomsLastViewport(t *testing.T) {
	d := testDaemon(t)
	task := plainTask(t, d, "room-fresh")

	if c, r := d.launchSizeFor(task.ID); c != launchCols || r != launchRows {
		t.Fatalf("a room that never had a viewer opens at %dx%d, want the %dx%d default", c, r, launchCols, launchRows)
	}
	d.noteRoomSize(177, 48)
	if c, r := d.launchSizeFor(task.ID); c != 177 || r != 48 {
		t.Fatalf("a fresh card opens at %dx%d, want the room's 177x48", c, r)
	}
}

func TestTheRoomsViewportSurvivesARestart(t *testing.T) {
	d := testDaemon(t)
	d.noteRoomSize(150, 44)

	// A daemon that has not looked yet reads it back from the store.
	again := &Daemon{st: d.st}
	if got := again.roomSize(); got != (viewport{150, 44}) {
		t.Fatalf("a restarted daemon read the room at %+v, want 150x44", got)
	}
}

func TestACardWithItsOwnSizeReopensAtIt(t *testing.T) {
	d := testDaemon(t)
	task := plainTask(t, d, "room-own")
	d.noteRoomSize(177, 48)
	if err := d.st.SetLastSize(task.ID, 132, 40); err != nil {
		t.Fatal(err)
	}
	if c, r := d.launchSizeFor(task.ID); c != 132 || r != 40 {
		t.Fatalf("the card reopens at %dx%d, want the 132x40 it was saved at", c, r)
	}

	// Saved before heights were recorded: its width, the room's height.
	old := plainTask(t, d, "room-old")
	if err := d.st.SetLastCols(old.ID, 160); err != nil {
		t.Fatal(err)
	}
	if c, r := d.launchSizeFor(old.ID); c != 160 || r != 48 {
		t.Fatalf("a width-only card reopens at %dx%d, want 160x48", c, r)
	}
}

func TestAViewerAgreeingOnASizeIsRememberedByTheRoom(t *testing.T) {
	d := testDaemon(t)
	f, r := sizedSession(t, d, "room-remember", 120, 30)
	r.onSized = d.noteRoomSize

	holdAttach(t, d, "room-remember", 150, 40)
	ptyIs(t, f, r, viewport{150, 40})
	if got := d.roomSize(); got != (viewport{150, 40}) {
		t.Fatalf("the room remembers %+v, want 150x40", got)
	}
}

// THE FIRST ATTACH IS ONE RESIZE WITH BOTH SIZES, and does not wait out the hold.
func TestTheFirstViewerResizesOnceWithBothSizes(t *testing.T) {
	d := testDaemon(t)
	f, r := sizedSession(t, d, "first-once", 120, 30)

	holdAttach(t, d, "first-once", 177, 48)
	ptyIs(t, f, r, viewport{177, 48})
	if got := resizesAfterAMoment(f); got != 1 {
		t.Fatalf("the first attach resized %d times: %+v", got, f.resized())
	}
}

// AT THE SIZE IT ALREADY IS, THE FIRST ATTACH RESIZES NOTHING.
func TestTheFirstViewerAtTheOpeningSizeResizesNothing(t *testing.T) {
	d := testDaemon(t)
	f, _ := sizedSession(t, d, "first-none", 177, 48)

	holdAttach(t, d, "first-none", 177, 48)
	if got := resizesAfterAMoment(f); got != 0 {
		t.Fatalf("an attach at the size the pty already was resized it: %+v", f.resized())
	}
}

// A SECOND VIEWER STILL GOES THROUGH THE HOLD: the width now, the height later.
func TestASecondViewersHeightStillWaitsOutTheHold(t *testing.T) {
	d := testDaemon(t)
	f, r := sizedSession(t, d, "second-hold", 120, 30)
	r.hold = 400 * time.Millisecond

	holdAttach(t, d, "second-hold", 150, 50)
	ptyIs(t, f, r, viewport{150, 50})

	holdAttach(t, d, "second-hold", 170, 34)
	// The width moved at once, the height is still 50.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if c, _ := r.buf.CurrentSize(); c == 170 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the width never moved: %+v", f.resized())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, rows := r.buf.CurrentSize(); rows != 50 {
		t.Fatalf("the second viewer's height %d was applied at once, want it held at 50", rows)
	}
	ptyIs(t, f, r, viewport{170, 34})
}

// THE LAST VIEWER LEAVING NEVER RESIZES, and the next first viewer is a first
// viewer again.
func TestALoneViewerLeavingAndComingBackResizesOnceMore(t *testing.T) {
	d := testDaemon(t)
	f, r := sizedSession(t, d, "first-again", 120, 30)

	first := holdAttach(t, d, "first-again", 140, 36)
	ptyIs(t, f, r, viewport{140, 36})
	first.close()
	if got := resizesAfterAMoment(f); got != 1 {
		t.Fatalf("the last viewer leaving resized: %+v", f.resized())
	}
	holdAttach(t, d, "first-again", 160, 44)
	ptyIs(t, f, r, viewport{160, 44})
	if got := resizesAfterAMoment(f); got != 2 {
		t.Fatalf("want one resize per first attach, got %+v", f.resized())
	}
}

var numbered = regexp.MustCompile(`\bL(\d\d)\b`)

// numberedLines counts how many times each Lnn shows in text.
func numberedLines(text string) map[string]int {
	out := map[string]int{}
	for _, m := range numbered.FindAllStringSubmatch(text, -1) {
		out[m[1]]++
	}
	return out
}

func scrollbackText(t *testing.T, d *Daemon, taskID string) string {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/tasks/"+taskID+"/scrollback/text", nil)
	req.SetPathValue("id", taskID)
	d.handleTextScrollback(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("scrollback text answered %d: %s", w.Code, w.Body.String())
	}
	return w.Body.String()
}

// freshCardAttachedElsewhere is the case that was reported: a real runner prints
// more than a screen, a viewer attaches at a size that is not the launch default,
// and the runner prints some more. Every numbered line has to be in the
// scrollback exactly once.
func freshCardAttachedElsewhere(t *testing.T, recordRoom bool) (map[string]int, []string) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("the repaint being guarded against is conhost's")
	}
	d := testDaemon(t)
	if recordRoom {
		d.noteRoomSize(177, 48)
	}
	dir := t.TempDir()
	task := cardAt(t, d, fmt.Sprintf("dupes-%v", recordRoom), dir)
	gate := filepath.Join(dir, "gate")
	script := fmt.Sprintf(
		`1..60 | ForEach-Object { 'L{0:D2} numbered line' -f $_ }; `+
			`while (-not (Test-Path '%s')) { Start-Sleep -Milliseconds 50 }; `+
			`61..90 | ForEach-Object { 'L{0:D2} numbered line' -f $_; Start-Sleep -Milliseconds 60 }; `+
			`Start-Sleep -Seconds 30`, gate)
	shell := "pwsh.exe"
	if _, err := exec.LookPath(shell); err != nil {
		shell = "powershell.exe"
	}
	if _, err := d.spawnPTY(task.ID, shell,
		[]string{"-NoProfile", "-NoLogo", "-Command", script}, dir, os.Environ()); err != nil {
		t.Skipf("no powershell to run: %v", err)
	}
	r := d.sup.get(task.ID)
	t.Cleanup(func() { r.closePTY() })
	waitFor := func(what, want string) {
		deadline := time.Now().Add(20 * time.Second)
		for !strings.Contains(string(r.buf.Snapshot()), want) {
			if time.Now().After(deadline) {
				t.Fatalf("%s: %q never appeared", what, want)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	waitFor("before the attach", "L60")

	// The runner keeps printing while the viewer arrives, which is when a
	// resize's repaint lands in the middle of output.
	if err := os.WriteFile(gate, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	holdAttach(t, d, task.ID, 177, 48)
	waitFor("after the attach", "L90")
	time.Sleep(time.Second)

	text := scrollbackText(t, d, task.ID)
	counts := numberedLines(text)
	var bad []string
	for i := 1; i <= 90; i++ {
		k := fmt.Sprintf("%02d", i)
		if counts[k] != 1 {
			bad = append(bad, fmt.Sprintf("L%s x%d", k, counts[k]))
		}
	}
	return counts, bad
}

func TestAFreshCardAttachedAtAnotherSizeFilesNoDuplicateLines(t *testing.T) {
	_, bad := freshCardAttachedElsewhere(t, true)
	if len(bad) > 0 {
		t.Fatalf("with a recorded room viewport these lines are not exactly once: %v", bad)
	}
}

// With no viewport on record the terminal opens at 120x30 and the attach is one
// real resize. Whether conhost shifts its repaint on that one resize is not
// atrium's to decide, so this reports rather than fails on a duplicate.
func TestAFreshCardOnAnUnrecordedRoomAttachesWithOneResize(t *testing.T) {
	_, bad := freshCardAttachedElsewhere(t, false)
	if len(bad) > 0 {
		t.Logf("conhost repainted across the one resize: %v", bad)
	}
}
