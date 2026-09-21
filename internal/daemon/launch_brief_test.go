package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// briefHarness registers a runner whose command does not exist, so a launch
// reaches the point where the brief is written and then fails to spawn without
// leaving a real process holding the test's temp directory open.
func briefHarness(t *testing.T, d *Daemon) string {
	t.Helper()
	if _, err := d.st.SaveHarness(store.Harness{
		ID: "brieftest", Label: "brief test", Enabled: true,
		Cmd: "atrium-no-such-binary-xyz", LaunchMode: store.LaunchPTY,
		PromptArgs: []string{"{prompt}"},
		ResumeArgs: []string{"--resume", "{resume}"},
	}); err != nil {
		t.Fatal(err)
	}
	return "brieftest"
}

// A brief is written into the runner's directory before it starts, so it is
// already there when the session reads it. The write happens before the spawn,
// so even a launch that fails to spawn on a bare test database has left the file.
func TestLaunchWritesBriefFile(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	dir := t.TempDir()
	h := briefHarness(t, d)
	// The launch fails at the spawn: the command does not exist. The brief is
	// written before that, which is the point.
	_, _ = d.Launch(LaunchRequest{
		Harness: h, Cwd: dir, Brief: "hello from the orchestrator",
	})

	raw, err := os.ReadFile(filepath.Join(dir, briefFileName))
	if err != nil {
		t.Fatalf("%s was not written: %v", briefFileName, err)
	}
	if !strings.Contains(string(raw), "hello from the orchestrator") {
		t.Fatalf("brief content missing: %q", raw)
	}
}

// A resume continues a conversation and takes no first prompt, so it must not
// rewrite the briefing under a session that already read it.
func TestLaunchSkipsBriefOnResume(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	dir := t.TempDir()
	h := briefHarness(t, d)
	_, _ = d.Launch(LaunchRequest{
		Harness: h, Cwd: dir, Brief: "should not land", Resume: "sess-x",
	})

	if _, err := os.Stat(filepath.Join(dir, briefFileName)); !os.IsNotExist(err) {
		t.Fatalf("a brief was written on a resume, err=%v", err)
	}
}

// The read instruction comes first, so a session does not start answering the
// task before it has read what it was handed. The task still has to be present,
// or the session reads a briefing and waits to be told what to do with it.
func TestBriefPromptPutsTheReadFirst(t *testing.T) {
	p := briefPrompt("do the thing")
	if !strings.HasPrefix(p, "Read "+briefFileName) {
		t.Fatalf("the read instruction is not first: %q", p)
	}
	if !strings.Contains(p, "do the thing") {
		t.Fatalf("the task was dropped: %q", p)
	}

	empty := briefPrompt("")
	if !strings.Contains(empty, "do what it asks") {
		t.Fatalf("an empty prompt should still tell it to act on the brief: %q", empty)
	}
}
