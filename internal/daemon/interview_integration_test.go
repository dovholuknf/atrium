//go:build integration

package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// An interviewer's turn ends on a question to the human, so atrium neither nudges it nor calls it stuck.
func TestAnInterviewerIsNeverSilentlyStopped(t *testing.T) {
	d := testDaemon(t)
	orch := peerCard(t, d, "orchestrator")
	iv := peerCard(t, d, "interviewer")
	if err := d.st.SetTags(iv.ID, []string{OriginAgentTag, SubagentTag, InterviewTag}); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetLineage(iv.ID, "orchestrator", orch.ID); err != nil {
		t.Fatal(err)
	}
	prompt(t, d, iv.ID)
	stopTurn(t, d, "interviewer")
	stuckAgrees(t, d, iv.ID, false)
	if n := len(pendingFrom(t, d, iv.ID)); n != 0 {
		t.Fatalf("the interviewer was typed %d message(s) after asking its question", n)
	}
	if n := len(pendingFrom(t, d, orch.ID)); n != 0 {
		t.Fatalf("the launcher got %d notice(s) for a question", n)
	}
}

func TestAnInterviewerGetsItsOwnSystemPrompt(t *testing.T) {
	in := []string{"--mcp-config", "C:/t/mcp.json"}
	read := func(string) ([]byte, error) { return []byte(`{"mcpServers":{}}`), nil }
	get := func(kit leanKit) string {
		args, err := leanArgs(in, nil, "", nil, kit, "", read)
		if err != nil {
			t.Fatal(err)
		}
		for i, a := range args {
			if a == "--append-system-prompt" {
				return args[i+1]
			}
		}
		t.Fatal("no system prompt")
		return ""
	}
	if got := get(leanKit{Interview: true}); got != interviewSystemPrompt || !strings.Contains(got, "reply IS the question") {
		t.Errorf("interview prompt = %q", got)
	}
	if got := get(leanKit{}); got != leanSystemPrompt {
		t.Errorf("a worker's prompt changed: %q", got)
	}
	lean, _, kit := leanOptions(LaunchRequest{Tags: []string{InterviewTag}}, nil, "")
	if lean || !kit.Interview {
		t.Errorf("leanOptions: lean %v kit %+v", lean, kit)
	}
}

func TestAnInterviewDefaultsToOpusFromTheSetting(t *testing.T) {
	d := testDaemon(t)
	h := &store.Harness{ID: "claude", Label: "Claude"}
	req := LaunchRequest{Tags: []string{OriginAgentTag, InterviewTag}}
	if got := d.workerDefaultModel(h, req, nil, ""); got != "opus" {
		t.Fatalf("default = %q, want opus", got)
	}
	stored, err := store.CheckWorkerPolicy(store.WorkerPolicy{InterviewModel: "sonnet"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetSetting(store.SettingWorkerPolicy, stored); err != nil {
		t.Fatal(err)
	}
	if got := d.workerDefaultModel(h, req, nil, ""); got != "sonnet" {
		t.Fatalf("with interview_model sonnet got %q", got)
	}
	if got := d.workerDefaultModel(h, LaunchRequest{Tags: []string{OriginAgentTag}}, nil, ""); got != "" {
		t.Fatalf("an ordinary worker got %q from the interview setting", got)
	}
}

func TestAnInterviewPromptHasNoReportEnding(t *testing.T) {
	if got := briefPromptEnding("BRIEF.md", "", ""); strings.Contains(got, "atrium_done") || !strings.Contains(got, "BRIEF.md") {
		t.Fatalf("prompt = %q", got)
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestAgentCheckoutRefusal(t *testing.T) {
	plain := t.TempDir()
	if why := agentCheckoutRefusal(plain); !strings.Contains(why, "not a git checkout") {
		t.Errorf("plain dir: %q", why)
	}
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", "main")
	if why := agentCheckoutRefusal(repo); !strings.Contains(why, "main") || !strings.Contains(why, "claude/*") {
		t.Errorf("main: %q", why)
	}
	if err := os.WriteFile(filepath.Join(repo, "f"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "checkout", "-q", "-b", "claude/some-work")
	if why := agentCheckoutRefusal(repo); why != "" {
		t.Errorf("claude branch refused: %q", why)
	}
}

func TestAnAgentLaunchOutsideGitIsRefusedUnlessScratch(t *testing.T) {
	d := testDaemon(t)
	dir := t.TempDir()
	_, err := d.launchLocked(LaunchRequest{Harness: "claude", Cwd: dir, Tags: []string{OriginAgentTag}})
	if err == nil || !strings.Contains(err.Error(), "scratch: true") || !strings.Contains(err.Error(), "not a git checkout") {
		t.Fatalf("err = %v, want a refusal that says why and how out", err)
	}
}
