package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestBriefFileCarriesTheGitURLLineOnce(t *testing.T) {
	dir := t.TempDir()
	if _, err := writeBriefFile(dir, "do the thing", true); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, briefFileName))
	if n := strings.Count(string(raw), "atrium_git_url"); n != 1 {
		t.Fatalf("the line appears %d times: %q", n, raw)
	}
	if !strings.Contains(string(raw), "Never ask for a paste.") || !strings.HasPrefix(string(raw), "do the thing") {
		t.Fatalf("brief body wrong: %q", raw)
	}
}

func TestABriefThatAlreadyHasTheLineIsNotDoubled(t *testing.T) {
	dir := t.TempDir()
	once := "task\n\n" + gitURLLine + "\n"
	if _, err := writeBriefFile(dir, once, true); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, briefFileName))
	if n := strings.Count(string(raw), "atrium_git_url"); n != 1 {
		t.Fatalf("doubled: %q", raw)
	}
	// and a second pass (a relaunch writing its own brief again) stays at one
	if got := withGitURLLine(withGitURLLine("x")); strings.Count(got, "atrium_git_url") != 1 {
		t.Fatalf("not idempotent: %q", got)
	}
}

// promptSeenBy launches `req` on a runner that writes the first prompt it was
// given to a file, and returns what it wrote ("" if it was never started).
func promptSeenBy(t *testing.T, d *Daemon, req LaunchRequest) string {
	t.Helper()
	// forward slashes: sh on Windows reads a backslash in the redirect as an escape
	out := filepath.ToSlash(filepath.Join(t.TempDir(), "prompt.txt"))
	if _, err := d.st.SaveHarness(store.Harness{
		ID: "promptcatch", Label: "prompt catch", Enabled: true, Cmd: "sh", LaunchMode: store.LaunchPTY,
		PromptArgs: []string{"-c", `printf %s "$0" > ` + out + `; sleep 5`, "{prompt}"},
		ResumeArgs: []string{"-c", `printf %s "resumed:$0 $1" > ` + out + `; sleep 5`, "{resume}", "{prompt}"},
	}); err != nil {
		t.Fatal(err)
	}
	req.Harness, req.Cwd = "promptcatch", t.TempDir()
	task, err := d.Launch(req)
	if err != nil {
		t.Skipf("could not spawn a test runner here: %v", err)
	}
	t.Cleanup(func() { _ = d.StopRunner(task.ID) })
	for i := 0; i < 100; i++ {
		if raw, err := os.ReadFile(out); err == nil {
			return string(raw)
		}
		time.Sleep(50 * time.Millisecond)
	}
	return ""
}

func TestALaunchWithNoBriefCarriesTheLineOnItsPromptOnce(t *testing.T) {
	d := testDaemon(t)
	got := promptSeenBy(t, d, LaunchRequest{Prompt: "just do it"})
	if !strings.HasPrefix(got, "just do it") || strings.Count(got, "atrium_git_url") != 1 {
		t.Fatalf("prompt %q", got)
	}
	again := promptSeenBy(t, d, LaunchRequest{Prompt: withGitURLLine("already")})
	if strings.Count(again, "atrium_git_url") != 1 {
		t.Fatalf("doubled: %q", again)
	}
}

// A resume is refused when it carries a prompt, and takes none of its own, so
// nothing can carry the line onto one. Pinned here so a change that lets a resume
// take a prompt meets this test.
func TestAResumeTakesNoGitURLLine(t *testing.T) {
	d := testDaemon(t)
	h := briefHarness(t, d)
	dir := t.TempDir()
	_, err := d.Launch(LaunchRequest{Harness: h, Cwd: dir, Prompt: "carry on", Resume: "sess-y"})
	if err == nil || !strings.Contains(err.Error(), "already has its instruction") {
		t.Fatalf("a resume with a prompt was not refused: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, briefFileName)); !os.IsNotExist(err) {
		t.Fatalf("a resume wrote a brief: %v", err)
	}
}

func TestAnOutsideCodeCardsBriefAndPromptCarryNoGitURLLine(t *testing.T) {
	dir := t.TempDir()
	if _, err := writeBriefFile(dir, "task", false); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, briefFileName))
	if strings.Contains(string(raw), "atrium_git_url") {
		t.Fatalf("brief %q", raw)
	}
	d := testDaemon(t)
	got := promptSeenBy(t, d, LaunchRequest{Prompt: "just do it", OutsideCode: true})
	if got == "" || strings.Contains(got, "atrium_git_url") {
		t.Fatalf("prompt %q", got)
	}
	tagged := promptSeenBy(t, d, LaunchRequest{Prompt: "just do it", Tags: []string{OutsideCodeTag}})
	if tagged == "" || strings.Contains(tagged, "atrium_git_url") {
		t.Fatalf("tagged prompt %q", tagged)
	}
}

// A runner with no atrium control MCP cannot see the tool, so the line says "if you have it".
func TestTheGitURLLineIsConditionalOnHavingTheTool(t *testing.T) {
	if !strings.HasPrefix(gitURLLine, "If you have `atrium_git_url`") {
		t.Fatalf("the line is unconditional: %q", gitURLLine)
	}
	if !strings.Contains(gitURLLine, "Never ask for a paste.") {
		t.Fatalf("the line lost its last sentence: %q", gitURLLine)
	}
}

func TestALaunchedOutsideCodeCardsBriefFileHasNoGitURLLine(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	h := briefHarness(t, d)
	for name, req := range map[string]LaunchRequest{
		"flag": {OutsideCode: true},
		"tag":  {Tags: []string{OutsideCodeTag}},
	} {
		dir := t.TempDir()
		req.Harness, req.Cwd, req.Brief = h, dir, "the task"
		_, _ = d.Launch(req)
		raw, err := os.ReadFile(filepath.Join(dir, briefFileName))
		if err != nil || !strings.Contains(string(raw), "the task") {
			t.Fatalf("%s: brief not written: %v %q", name, err, raw)
		}
		if strings.Contains(string(raw), "atrium_git_url") {
			t.Fatalf("%s: outside-code brief carries the line: %q", name, raw)
		}
	}
}
