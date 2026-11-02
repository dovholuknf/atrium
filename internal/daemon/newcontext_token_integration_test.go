//go:build integration

package daemon

import (
	"github.com/dovholuknf/atrium/internal/daemon/cycle"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// handoffBody is a handoff long enough to clear the 200 byte floor.
var handoffBody = []byte(strings.Repeat("what is done and what is left. ", 10))

// aged writes body to the card's file and makes it an hour old.
func writeAged(t *testing.T, path string, body []byte, age time.Duration) {
	t.Helper()
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-age)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
}

func withLine(token string, body []byte) []byte {
	return append([]byte(cycle.CaptureLine(token)+"\n"), body...)
}

// A file from before the capture passes when it carries this run's line.
func TestHandoffWrittenAcceptsAnOldFileWithTheToken(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	task, _, dir := ncCard(t, d)
	writeAged(t, filepath.Join(dir, cycle.HandoffName(task)), withLine("7-abc", handoffBody), time.Minute)
	if err := d.HandoffWritten(task.ID, cycle.HandoffName(task), time.Now(), "7-abc"); err != nil {
		t.Fatalf("refused a fresh handoff carrying the token: %v", err)
	}
}

// Markdown dressing around the line does not stop it counting.
func TestHandoffWrittenAcceptsTheTokenInMarkdown(t *testing.T) {
	t.Parallel()
	for name, line := range map[string]string{
		"backticks": "`" + cycle.CaptureLine("7-abc") + "`",
		"bullet":    "- " + cycle.CaptureLine("7-abc"),
		"bold":      "**" + cycle.CaptureLine("7-abc") + "**",
	} {
		t.Run(name, func(t *testing.T) {
			d := testDaemon(t)
			task, _, dir := ncCard(t, d)
			writeAged(t, filepath.Join(dir, cycle.HandoffName(task)), append([]byte(line+"\n"), handoffBody...), time.Minute)
			if err := d.HandoffWritten(task.ID, cycle.HandoffName(task), time.Now(), "7-abc"); err != nil {
				t.Fatalf("refused the token as %s: %v", name, err)
			}
		})
	}
}

// The same file without it fails, and says what to do.
func TestHandoffWrittenNamesTheMissingToken(t *testing.T) {
	d := testDaemon(t)
	task, _, dir := ncCard(t, d)
	writeAged(t, filepath.Join(dir, cycle.HandoffName(task)), handoffBody, time.Minute)
	err := d.HandoffWritten(task.ID, cycle.HandoffName(task), time.Now(), "7-abc")
	if err == nil {
		t.Fatal("an old file without the token passed")
	}
	for _, want := range []string{cycle.HandoffName(task), filepath.ToSlash(dir), "atrium-capture: 7-abc"} {
		if !strings.Contains(filepath.ToSlash(err.Error()), want) {
			t.Fatalf("the message does not name %q: %v", want, err)
		}
	}
}

// Written during the capture turn passes without the token: the old rule.
func TestHandoffWrittenAcceptsAFileWrittenDuringTheCapture(t *testing.T) {
	d := testDaemon(t)
	task, _, dir := ncCard(t, d)
	since := time.Now().Add(-time.Second)
	if err := os.WriteFile(filepath.Join(dir, cycle.HandoffName(task)), handoffBody, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := d.HandoffWritten(task.ID, cycle.HandoffName(task), since, "7-abc"); err != nil {
		t.Fatalf("refused a file written during the capture: %v", err)
	}
}

// A token from an earlier run is not this run's.
func TestHandoffWrittenRefusesAnEarlierRunsToken(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	task, _, dir := ncCard(t, d)
	writeAged(t, filepath.Join(dir, cycle.HandoffName(task)), withLine("6-old", handoffBody), time.Minute)
	if err := d.HandoffWritten(task.ID, cycle.HandoffName(task), time.Now(), "7-abc"); err == nil {
		t.Fatal("an earlier run's token passed")
	}
}

// A token alone is not a handoff, by either rule.
func TestHandoffWrittenRefusesAFileUnderTheFloor(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	task, _, dir := ncCard(t, d)
	small := []byte(cycle.CaptureLine("7-abc") + "\nshort\n")
	if err := os.WriteFile(filepath.Join(dir, cycle.HandoffName(task)), small, 0o644); err != nil {
		t.Fatal(err)
	}
	err := d.HandoffWritten(task.ID, cycle.HandoffName(task), time.Now().Add(-time.Second), "7-abc")
	if err == nil || !strings.Contains(err.Error(), "bytes") {
		t.Fatalf("a file under the floor passed or was not named: %v", err)
	}
}

// Only the first 4 KB is looked at.
func TestHandoffWrittenDoesNotFindATokenBeyond4KB(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	task, _, dir := ncCard(t, d)
	body := append([]byte(strings.Repeat("x", cycle.HandoffHead)+"\n"), withLine("7-abc", handoffBody)...)
	writeAged(t, filepath.Join(dir, cycle.HandoffName(task)), body, time.Minute)
	if err := d.HandoffWritten(task.ID, cycle.HandoffName(task), time.Now(), "7-abc"); err == nil {
		t.Fatal("found a token past 4 KB")
	}
}

// The capture prompt carries the token, and a file already written with it passes.
// The idle parking's and the move's capture, which is not a context cycle.
func TestNewContextCapturePromptCarriesTheToken(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, dir := ncCard(t, d)
	writeAged(t, filepath.Join(dir, cycle.HandoffName(task)), handoffBody, time.Minute)
	gen, ok := d.Nctx.BeginCaptureOnly(task.ID, cycle.HandoffName(task), "")
	if !ok {
		t.Fatal("could not begin")
	}
	done := make(chan error, 1)
	go func() {
		_, err := d.NcCapture(task.ID, gen, cycle.HandoffName(task))
		done <- err
	}()
	until(t, "the capture prompt", func() bool { return strings.Contains(f.written(), "atrium-capture: ") })
	m := regexp.MustCompile(`atrium-capture: (\S+?)\.? `).FindStringSubmatch(f.written())
	if m == nil {
		t.Fatalf("no token in the prompt: %q", f.written())
	}
	d.act.set(task.ID, ActivityThinking, "")
	writeAged(t, filepath.Join(dir, cycle.HandoffName(task)), withLine(m[1], handoffBody), time.Minute)
	time.Sleep(20 * time.Millisecond) // wait: the file must age before the turn ends, no event marks it
	ncTurnEnds(d, task.ID)
	if err := <-done; err != nil {
		t.Fatalf("the capture was refused: %v", err)
	}
}
