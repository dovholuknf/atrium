package daemon

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

const stewardAgent = "---\nname: codebase-steward\ndescription: Keeps the codebase honest\ntools: Read, Grep, Glob\nmodel: opus\ncolor: red\nskills: [review-panel]\n---\n\nYou are the steward.\nBe brief.\n"

// agentsDir is a temp agents directory holding three definitions, and stands in
// for the operator's ~/.claude/agents. The real one is never read.
func agentsDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{
		"codebase-steward":     stewardAgent,
		"go-security-reviewer": "---\ndescription: Go security\n---\nReview Go.",
		"unnamed-secret-agent": "---\ndescription: not asked for\n---\nSecret.",
	} {
		if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := userAgentsDir
	userAgentsDir = func() string { return dir }
	t.Cleanup(func() { userAgentsDir = old })
	return dir
}

func TestLeanArgsAgentToolIsDisallowedOnlyWithoutLeanAgents(t *testing.T) {
	agentsDir(t)
	in := []string{"--model", "m", "go"}
	without, err := leanArgs(in, nil, "", nil, nil, os.ReadFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(","+flagValue(t, without, "--disallowedTools")+",", ",Agent,") || count(without, "--agents") != 0 {
		t.Fatalf("a plain lean launch keeps Agent disallowed and has no --agents: %q", without)
	}
	with, err := leanArgs(in, nil, "", nil, []string{"codebase-steward"}, os.ReadFile)
	if err != nil {
		t.Fatal(err)
	}
	dis := "," + flagValue(t, with, "--disallowedTools") + ","
	if strings.Contains(dis, ",Agent,") || strings.Contains(dis, ",Task,") {
		t.Fatalf("Agent should be allowed with lean_agents: %s", dis)
	}
	// Everything else lean drops stays dropped, and the prompt stays last.
	for _, still := range []string{"Skill", "Workflow", "AskUserQuestion"} {
		if !strings.Contains(dis, ","+still+",") {
			t.Fatalf("%s should stay disallowed: %s", still, dis)
		}
	}
	if with[len(with)-1] != "go" || flagValue(t, with, "--setting-sources") != "project,local" {
		t.Fatalf("prompt last and user source still cut: %q", with)
	}
}

func TestLeanAgentsExposeOnlyTheNamedFiles(t *testing.T) {
	agentsDir(t)
	got, err := leanArgs([]string{"go"}, nil, "", nil, []string{"codebase-steward", "go-security-reviewer"}, os.ReadFile)
	if err != nil {
		t.Fatal(err)
	}
	v := flagValue(t, got, "--agents")
	for _, must := range []string{`"codebase-steward"`, `"go-security-reviewer"`, "You are the steward.", `"tools":["Read","Grep","Glob"]`,
		`"model":"opus"`, `"description":"Keeps the codebase honest"`} {
		if !strings.Contains(v, must) {
			t.Fatalf("--agents lacks %s: %s", must, v)
		}
	}
	for _, mustNot := range []string{"unnamed-secret-agent", "Secret.", "color", "skills", "review-panel", "---"} {
		if strings.Contains(v, mustNot) {
			t.Fatalf("--agents carries %q: %s", mustNot, v)
		}
	}
}

func TestLeanAgentsRefusesAnUnknownOrUnsafeName(t *testing.T) {
	agentsDir(t)
	_, err := leanArgs([]string{"go"}, nil, "", nil, []string{"codebase-steward", "no-such-agent"}, os.ReadFile)
	if err == nil || !strings.Contains(err.Error(), "no-such-agent") {
		t.Fatalf("an unknown agent should be refused by name, got %v", err)
	}
	for _, bad := range []string{"../settings", "a/b", `a\b`, ".hidden"} {
		if _, err := leanArgs([]string{"go"}, nil, "", nil, []string{bad}, os.ReadFile); err == nil {
			t.Fatalf("%q should be refused", bad)
		}
	}
}

func TestLeanAgentsRefusesTooMuchForACommandLine(t *testing.T) {
	dir := agentsDir(t)
	big := "---\ndescription: big\n---\n" + strings.Repeat("x", leanAgentsMaxBytes)
	if err := os.WriteFile(filepath.Join(dir, "big.md"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := leanArgs([]string{"go"}, nil, "", nil, []string{"big"}, os.ReadFile); err == nil {
		t.Fatal("an oversize --agents should be refused")
	}
}

func TestLeanOptionsCarryTheAgentsOnTheCardAndTheRequestReplacesThem(t *testing.T) {
	lean, _, agents := leanOptions(LaunchRequest{LeanAgents: []string{" b", "a", "a"}}, nil)
	if !lean || strings.Join(agents, ",") != "a,b" {
		t.Fatalf("lean_agents implies lean, got %v %q", lean, agents)
	}
	card := &store.Task{Tags: mergeTags([]string{"origin:agent"}, leanTags([]string{"ziti"}, []string{"a", "b"}))}
	lean, mcp, agents := leanOptions(LaunchRequest{}, card)
	if !lean || strings.Join(agents, ",") != "a,b" || len(mcp) != 1 {
		t.Fatalf("a restart keeps the list, got %v %q %q", lean, mcp, agents)
	}
	if _, _, agents = leanOptions(LaunchRequest{LeanAgents: []string{"c"}}, card); strings.Join(agents, ",") != "c" {
		t.Fatalf("a request naming agents replaces the card's, got %q", agents)
	}
	off := false
	if lean, _, agents = leanOptions(LaunchRequest{Lean: &off}, card); lean || agents != nil {
		t.Fatalf("lean: false drops them, got %v %q", lean, agents)
	}
	got, cut := withoutLeanTags(card.Tags)
	if !cut || strings.Join(got, ",") != "origin:agent" {
		t.Fatalf("lean off takes the agent tags too, got %q", got)
	}
	if got := withoutAgentTags(card.Tags); strings.Contains(strings.Join(got, ","), "atrium:agent:") || !hasTag(got, LeanTag) {
		t.Fatalf("got %q", got)
	}
}

// agentLaunchDaemon adds a runner named claude whose command is a sleep, so a
// launch that gets past the checks starts something harmless.
func agentLaunchDaemon(t *testing.T) (*Daemon, http.Handler) {
	t.Helper()
	d, _, h := unleanDaemon(t)
	cmd, args := "sh", []string{"-c", "sleep 60"}
	if _, err := d.st.SaveHarness(store.Harness{
		ID: "claude", Label: "claude code", Enabled: true,
		Cmd: cmd, Args: args, LaunchMode: store.LaunchPTY,
	}); err != nil {
		t.Fatal(err)
	}
	return d, h
}

func TestLaunchRefusesLeanAgentsOnACodexRunnerAndWithLeanFalse(t *testing.T) {
	agentsDir(t)
	_, card, h := unleanDaemon(t)
	w := launchOnto(h, card, `,"lean_agents":["codebase-steward"]`)
	if w.Code == http.StatusOK || !strings.Contains(w.Body.String(), "lean_agents is a claude launch option") {
		t.Fatalf("a runner that is not claude should refuse lean_agents, answered %d: %s", w.Code, w.Body)
	}
	_, h = agentLaunchDaemon(t)
	w = send(h, "POST", "/v1/launch", `{"harness":"claude","cwd":"`+filepath.ToSlash(t.TempDir())+
		`","lean":false,"lean_agents":["codebase-steward"]}`)
	if w.Code == http.StatusOK || !strings.Contains(w.Body.String(), "lean: false") {
		t.Fatalf("lean_agents with lean false should be refused, answered %d: %s", w.Code, w.Body)
	}
}

func TestLaunchRefusesAnUnknownAgentBeforeStartingAnything(t *testing.T) {
	agentsDir(t)
	d, h := agentLaunchDaemon(t)
	cwd := filepath.ToSlash(t.TempDir())
	w := send(h, "POST", "/v1/launch", `{"harness":"claude","cwd":"`+cwd+`","lean_agents":["codebase-steward","ghost"]}`)
	if w.Code == http.StatusOK || !strings.Contains(w.Body.String(), "ghost") {
		t.Fatalf("an unknown agent should refuse the launch by name, answered %d: %s", w.Code, w.Body)
	}
	tasks, err := d.st.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range tasks {
		if tk.Worktree == cwd {
			t.Fatalf("a refused launch left a card: %+v", tk)
		}
	}
}

func TestLaunchKeepsLeanAgentsOnTheCardForARestart(t *testing.T) {
	agentsDir(t)
	d, h := agentLaunchDaemon(t)
	cwd := filepath.ToSlash(t.TempDir())
	w := send(h, "POST", "/v1/launch", `{"harness":"claude","cwd":"`+cwd+`","lean_agents":["go-security-reviewer","codebase-steward"]}`)
	if w.Code != http.StatusOK {
		t.Skipf("could not start a runner, answered %d: %s", w.Code, w.Body)
	}
	tasks, err := d.st.List()
	if err != nil {
		t.Fatal(err)
	}
	var card *store.Task
	for i := range tasks {
		if tasks[i].Worktree == cwd {
			card = tasks[i]
		}
	}
	if card == nil {
		t.Fatal("no card")
	}
	t.Cleanup(func() { _ = d.StopRunner(card.ID) })
	lean, _, agents := leanOptions(LaunchRequest{}, card)
	if !lean || strings.Join(agents, ",") != "codebase-steward,go-security-reviewer" {
		t.Fatalf("the card should keep the list for a restart, tags %q -> %v %q", card.Tags, lean, agents)
	}
}

func TestKeepaliveForkOfALeanAgentsCardKeepsAgentToolAndDefinitions(t *testing.T) {
	agentsDir(t)
	f := newKAFix(t)
	if err := f.st.SetTags(f.task.ID, []string{LeanTag, OriginAgentTag, leanAgentTagPrefix + "codebase-steward"}); err != nil {
		t.Fatal(err)
	}
	old := readUserSettings
	readUserSettings = func() []byte { return []byte(`{}`) }
	t.Cleanup(func() { readUserSettings = old })
	f.reply(f.now.Add(-56*time.Minute), replyOpt{})
	f.tick()
	if f.forks() != 1 {
		t.Fatalf("forks = %d, want 1", f.forks())
	}
	args := f.specs[0].Args
	if strings.Contains(","+flagValue(t, args, "--disallowedTools")+",", ",Agent,") || !strings.Contains(flagValue(t, args, "--agents"), "codebase-steward") {
		t.Fatalf("the fork must carry the same tool list and agents: %q", args)
	}
}
