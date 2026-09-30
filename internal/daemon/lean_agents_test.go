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

const stewardAgent = "---\nname: codebase-steward\ndescription: Keeps the codebase honest\ntools: Read, Grep, Glob\n---\n\nYou are the steward.\r\nBe brief.\n"

func write(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// kitDirs points the operator's agents and skills, and the plugin root, at temp
// directories. The real ~/.claude is never read or written.
func kitDirs(t *testing.T) (agents, skills, root string) {
	t.Helper()
	agents, skills, root = t.TempDir(), t.TempDir(), filepath.Join(t.TempDir(), "lean-plugins")
	write(t, filepath.Join(agents, "codebase-steward.md"), stewardAgent)
	write(t, filepath.Join(agents, "go-security-reviewer.md"), "---\ndescription: Go security\n---\n"+strings.Repeat("Review Go. ", 3000))
	write(t, filepath.Join(agents, "unnamed-secret-agent.md"), "---\ndescription: not asked for\n---\nSecret.")
	write(t, filepath.Join(skills, "review-panel", "SKILL.md"), "---\nname: review-panel\ndescription: Panel\n---\nRun the panel.")
	write(t, filepath.Join(skills, "review-panel", "refs", "rubric.md"), "rubric")
	write(t, filepath.Join(skills, "other-skill", "SKILL.md"), "---\nname: other-skill\ndescription: no\n---\nNo.")
	oa, os_, or := userAgentsDir, userSkillsDir, leanPluginRoot
	userAgentsDir = func() string { return agents }
	userSkillsDir = func() string { return skills }
	leanPluginRoot = root
	t.Cleanup(func() { userAgentsDir, userSkillsDir, leanPluginRoot = oa, os_, or })
	return
}

func TestLeanArgsAgentToolIsDisallowedOnlyWithoutLeanAgents(t *testing.T) {
	kitDirs(t)
	in := []string{"--model", "m", "go"}
	without, err := leanArgs(in, nil, "", nil, leanKit{}, "", os.ReadFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(","+flagValue(t, without, "--disallowedTools")+",", ",Agent,") || count(without, "--plugin-dir") != 0 {
		t.Fatalf("a plain lean launch keeps Agent disallowed and has no --plugin-dir: %q", without)
	}
	with, err := leanArgs(in, nil, "", nil, leanKit{Agents: []string{"codebase-steward"}}, "", os.ReadFile)
	if err != nil {
		t.Fatal(err)
	}
	dis := "," + flagValue(t, with, "--disallowedTools") + ","
	if strings.Contains(dis, ",Agent,") || strings.Contains(dis, ",Task,") {
		t.Fatalf("Agent should be allowed with lean_agents: %s", dis)
	}
	// Everything else lean drops stays dropped, the Skill tool included, and the
	// prompt stays last.
	for _, still := range []string{"Skill", "Workflow", "AskUserQuestion"} {
		if !strings.Contains(dis, ","+still+",") {
			t.Fatalf("%s should stay disallowed: %s", still, dis)
		}
	}
	if with[len(with)-1] != "go" || flagValue(t, with, "--setting-sources") != "project,local" || count(with, "--agents") != 0 {
		t.Fatalf("prompt last, user source still cut, no inline --agents: %q", with)
	}
	sk, err := leanArgs(in, nil, "", nil, leanKit{Skills: []string{"review-panel"}}, "", os.ReadFile)
	if err != nil {
		t.Fatal(err)
	}
	dis = "," + flagValue(t, sk, "--disallowedTools") + ","
	if strings.Contains(dis, ",Skill,") || !strings.Contains(dis, ",Agent,") {
		t.Fatalf("lean_skills allows Skill and leaves Agent out: %s", dis)
	}
}

func TestLeanPluginHoldsOnlyTheNamedFilesByteForByte(t *testing.T) {
	agents, _, root := kitDirs(t)
	got, err := leanArgs([]string{"go"}, nil, "", nil,
		leanKit{Agents: []string{"codebase-steward", "go-security-reviewer"}, Skills: []string{"review-panel"}}, "", os.ReadFile)
	if err != nil {
		t.Fatal(err)
	}
	dir := flagValue(t, got, "--plugin-dir")
	if filepath.Dir(dir) != root {
		t.Fatalf("the plugin belongs under atrium's state %s, not %s", root, dir)
	}
	var have []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, _ error) error {
		if !d.IsDir() {
			r, _ := filepath.Rel(dir, p)
			have = append(have, filepath.ToSlash(r))
		}
		return nil
	})
	want := ".claude-plugin/plugin.json,agents/codebase-steward.md,agents/go-security-reviewer.md," +
		"skills/review-panel/SKILL.md,skills/review-panel/refs/rubric.md"
	if strings.Join(have, ",") != want {
		t.Fatalf("plugin holds %v, want %s", have, want)
	}
	for _, n := range []string{"codebase-steward", "go-security-reviewer"} {
		a, _ := os.ReadFile(filepath.Join(agents, n+".md"))
		b, err := os.ReadFile(filepath.Join(dir, "agents", n+".md"))
		if err != nil || string(a) != string(b) {
			t.Fatalf("%s is not a byte for byte copy: %v", n, err)
		}
	}
	m, _ := os.ReadFile(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	if !strings.Contains(string(m), `"name":"atrium"`) {
		t.Fatalf("manifest: %s", m)
	}
	// The same kit finds the same directory and leaves it alone.
	again, err := leanArgs([]string{"go"}, nil, "", nil,
		leanKit{Agents: []string{"go-security-reviewer", "codebase-steward"}, Skills: []string{"review-panel"}}, "", os.ReadFile)
	if err != nil || flagValue(t, again, "--plugin-dir") != dir {
		t.Fatalf("same kit, different directory: %v", err)
	}
	// A changed file is a different directory, so a live session's is never rewritten.
	write(t, filepath.Join(agents, "codebase-steward.md"), "---\ndescription: changed\n---\nNew.")
	changed, err := leanArgs([]string{"go"}, nil, "", nil,
		leanKit{Agents: []string{"codebase-steward"}}, "", os.ReadFile)
	if err != nil || flagValue(t, changed, "--plugin-dir") == dir {
		t.Fatalf("a changed kit should get its own directory: %v", err)
	}
}

func TestLeanPluginFollowsLinks(t *testing.T) {
	agents, _, _ := kitDirs(t)
	real := filepath.Join(t.TempDir(), "steward-real.md")
	write(t, real, stewardAgent)
	link := filepath.Join(agents, "linked-agent.md")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if _, err := os.ReadFile(link); err != nil {
		t.Skipf("a symlink is made but cannot be followed here (Windows untrusted mount point): %v", err)
	}
	dir, err := leanPluginDir(leanKit{Agents: []string{"linked-agent"}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "agents", "linked-agent.md"))
	if err != nil || string(b) != stewardAgent {
		t.Fatalf("a linked agent should be copied through: %v", err)
	}
}

// A hard link works where a symlink is not allowed, so the copy-through is
// covered on every platform.
func TestLeanPluginCopiesAHardLinkedAgent(t *testing.T) {
	agents, _, _ := kitDirs(t)
	real := filepath.Join(t.TempDir(), "steward-real.md")
	write(t, real, stewardAgent)
	if err := os.Link(real, filepath.Join(agents, "hard-agent.md")); err != nil {
		t.Skipf("no hard links here: %v", err)
	}
	dir, err := leanPluginDir(leanKit{Agents: []string{"hard-agent"}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "agents", "hard-agent.md"))
	if err != nil || string(b) != stewardAgent {
		t.Fatalf("a linked agent should be copied byte for byte: %v", err)
	}
}

func TestLeanKitRefusesAnUnknownOrUnsafeName(t *testing.T) {
	kitDirs(t)
	_, err := leanArgs([]string{"go"}, nil, "", nil, leanKit{Agents: []string{"codebase-steward", "no-such-agent"}}, "", os.ReadFile)
	if err == nil || !strings.Contains(err.Error(), "no-such-agent") {
		t.Fatalf("an unknown agent should be refused by name, got %v", err)
	}
	_, err = leanArgs([]string{"go"}, nil, "", nil, leanKit{Skills: []string{"no-such-skill"}}, "", os.ReadFile)
	if err == nil || !strings.Contains(err.Error(), "no-such-skill") {
		t.Fatalf("an unknown skill should be refused by name, got %v", err)
	}
	for _, bad := range []string{"../settings", "a/b", `a\b`, ".hidden"} {
		if _, err := leanArgs([]string{"go"}, nil, "", nil, leanKit{Agents: []string{bad}}, "", os.ReadFile); err == nil {
			t.Fatalf("agent %q should be refused", bad)
		}
		if _, err := leanArgs([]string{"go"}, nil, "", nil, leanKit{Skills: []string{bad}}, "", os.ReadFile); err == nil {
			t.Fatalf("skill %q should be refused", bad)
		}
	}
}

func TestLeanOptionsCarryTheKitOnTheCardAndTheRequestReplacesIt(t *testing.T) {
	lean, _, kit := leanOptions(LaunchRequest{LeanAgents: []string{" b", "a", "a"}, LeanSkills: []string{"s"}}, nil, "")
	if !lean || strings.Join(kit.Agents, ",") != "a,b" || strings.Join(kit.Skills, ",") != "s" {
		t.Fatalf("lean_agents implies lean, got %v %+v", lean, kit)
	}
	card := &store.Task{Tags: mergeTags([]string{"origin:agent"}, leanTags([]string{"ziti"}, leanKit{Agents: []string{"a", "b"}, Skills: []string{"s"}}))}
	lean, mcp, kit := leanOptions(LaunchRequest{}, card, "")
	if !lean || strings.Join(kit.Agents, ",") != "a,b" || strings.Join(kit.Skills, ",") != "s" || len(mcp) != 1 {
		t.Fatalf("a restart keeps the lists, got %v %q %+v", lean, mcp, kit)
	}
	if _, _, kit = leanOptions(LaunchRequest{LeanAgents: []string{"c"}}, card, ""); strings.Join(kit.Agents, ",") != "c" || strings.Join(kit.Skills, ",") != "s" {
		t.Fatalf("a request naming agents replaces the card's agents only, got %+v", kit)
	}
	off := false
	if lean, _, kit = leanOptions(LaunchRequest{Lean: &off}, card, ""); lean || !kit.empty() {
		t.Fatalf("lean: false drops them, got %v %+v", lean, kit)
	}
	got, cut := withoutLeanTags(card.Tags)
	if !cut || strings.Join(got, ",") != "origin:agent" {
		t.Fatalf("lean off takes the kit tags too, got %q", got)
	}
	if got := withoutKitTags(card.Tags); strings.Contains(strings.Join(got, ","), "atrium:agent:") ||
		strings.Contains(strings.Join(got, ","), "atrium:skill:") || !hasTag(got, LeanTag) {
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
	kitDirs(t)
	_, card, h := unleanDaemon(t)
	w := launchOnto(h, card, `,"lean_agents":["codebase-steward"]`)
	if w.Code == http.StatusOK || !strings.Contains(w.Body.String(), "claude launch options") {
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
	kitDirs(t)
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

func TestLaunchKeepsTheKitOnTheCardForARestartAndWritesNothingInTheWorktree(t *testing.T) {
	kitDirs(t)
	d, h := agentLaunchDaemon(t)
	// New pointed the plugin root at this daemon's state.
	cwd := t.TempDir()
	w := send(h, "POST", "/v1/launch", `{"harness":"claude","cwd":"`+filepath.ToSlash(cwd)+
		`","lean_agents":["go-security-reviewer","codebase-steward"],"lean_skills":["review-panel"]}`)
	if w.Code != http.StatusOK {
		t.Skipf("could not start a runner, answered %d: %s", w.Code, w.Body)
	}
	tasks, err := d.st.List()
	if err != nil {
		t.Fatal(err)
	}
	var card *store.Task
	for i := range tasks {
		if tasks[i].Worktree == filepath.ToSlash(cwd) {
			card = tasks[i]
		}
	}
	if card == nil {
		t.Fatal("no card")
	}
	t.Cleanup(func() { _ = d.StopRunner(card.ID) })
	lean, _, kit := leanOptions(LaunchRequest{}, card, "")
	if !lean || strings.Join(kit.Agents, ",") != "codebase-steward,go-security-reviewer" || strings.Join(kit.Skills, ",") != "review-panel" {
		t.Fatalf("the card should keep the lists for a restart, tags %q -> %v %+v", card.Tags, lean, kit)
	}
	if ents, _ := os.ReadDir(cwd); len(ents) != 0 {
		t.Fatalf("the worktree must stay clean for the cull, it holds %v", ents)
	}
}

func TestKeepaliveForkOfALeanKitCardKeepsToolsAndPlugin(t *testing.T) {
	kitDirs(t)
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
	if strings.Contains(","+flagValue(t, args, "--disallowedTools")+",", ",Agent,") ||
		!strings.Contains(flagValue(t, args, "--plugin-dir"), "lean-plugins") {
		t.Fatalf("the fork must carry the same tool list and plugin: %q", args)
	}
}
