//go:build integration

package daemon

import (
	"encoding/json"
	"github.com/dovholuknf/atrium/internal/daemon/cycle"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"github.com/dovholuknf/atrium/internal/small"
	"github.com/dovholuknf/atrium/internal/store"
)

func superleanFlags(t *testing.T, in []string, kit leanKit, gateway string, mcp ...string) []string {
	t.Helper()
	kit.Superlean = true
	got, err := leanArgs(in, nil, "", mcp, kit, gateway,
		leanTestRead(map[string]string{"C:/t/mcp.json": leanGatewayMCP}), testHooks())
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestSuperleanFlagSetHasTheAllowListAndTheShortPrompt(t *testing.T) {
	t.Parallel()
	got := superleanFlags(t, []string{"--mcp-config", "C:/t/mcp.json", "--strict-mcp-config", "go"}, leanKit{}, "")
	if flagValue(t, got, "--tools") != "Read,Edit,Write,Grep,Glob,Bash,PowerShell,ToolSearch" {
		t.Fatalf("tools = %q", flagValue(t, got, "--tools"))
	}
	if flagValue(t, got, "--append-system-prompt") != superleanSystemPrompt || got[len(got)-1] != "go" {
		t.Fatalf("prompt or position wrong: %q", got)
	}
	if flagValue(t, got, "--disallowedTools") != strings.Join(leanDisallowedFor(leanKit{}), ",") {
		t.Fatalf("disallowed changed: %q", got)
	}
	var doc struct {
		MCPServers map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(flagValue(t, got, "--mcp-config")), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.MCPServers) != 1 || doc.MCPServers["atrium-control"] == nil {
		t.Fatalf("superlean keeps atrium-control alone, got %v", doc.MCPServers)
	}
}

func TestSuperleanKeepsMercuriusOnlyWhenMCPNamesIt(t *testing.T) {
	t.Parallel()
	servers := func(gateway string, mcp ...string) map[string]map[string]string {
		got := superleanFlags(t, []string{"--mcp-config", "C:/t/mcp.json"}, leanKit{}, gateway, mcp...)
		var doc struct {
			MCPServers map[string]map[string]string `json:"mcpServers"`
		}
		if err := json.Unmarshal([]byte(flagValue(t, got, "--mcp-config")), &doc); err != nil {
			t.Fatal(err)
		}
		return doc.MCPServers
	}
	if s := servers("mercurius-worker"); len(s) != 1 || s["atrium-control"] == nil {
		t.Fatalf("the gateway must not be added, got %v", s)
	}
	if s := servers("mercurius-worker", "mercurius"); s["mercurius"]["url"] != "http://127.0.0.1:8088/mcp" || len(s) != 2 {
		t.Fatalf("named mercurius is the wide one, got %v", s)
	}
}

func TestSuperleanToolsGrowOnlyWithAgentsAndSkills(t *testing.T) {
	hk, _, _, _ := kitDirs(t)
	run := func(kit leanKit) string {
		kit.Superlean = true
		got, err := leanArgs([]string{"go"}, nil, "", nil, kit, "", os.ReadFile, hk)
		if err != nil {
			t.Fatal(err)
		}
		return flagValue(t, got, "--tools")
	}
	base := "Read,Edit,Write,Grep,Glob,Bash,PowerShell,ToolSearch"
	if got := run(leanKit{}); got != base {
		t.Fatalf("got %s", got)
	}
	if got := run(leanKit{Agents: []string{"codebase-steward"}}); got != base+",Agent" {
		t.Fatalf("got %s", got)
	}
	if got := run(leanKit{Skills: []string{"review-panel"}}); got != base+",Skill" {
		t.Fatalf("got %s", got)
	}
	if got := run(leanKit{Agents: []string{"codebase-steward"}, Skills: []string{"review-panel"}}); got != base+",Agent,Skill" {
		t.Fatalf("got %s", got)
	}
}

func TestSuperleanLeavesACallersOwnToolsAlone(t *testing.T) {
	t.Parallel()
	for _, in := range [][]string{
		{"--tools", "Bash,ToolSearch", "go"},
		{"--tools=Bash,ToolSearch", "go"},
	} {
		got := superleanFlags(t, in, leanKit{}, "")
		if count(got, "--tools") > 1 || strings.Count(strings.Join(got, " "), "--tools") != 1 {
			t.Fatalf("two --tools on one line: %q", got)
		}
		if !hasToolsFlag(in) {
			t.Fatalf("hasToolsFlag missed %q", in)
		}
	}
	got := superleanFlags(t, []string{"--tools", "Bash,ToolSearch", "go"}, leanKit{}, "")
	if flagValue(t, got, "--tools") != "Bash,ToolSearch" {
		t.Fatalf("the caller's list was replaced: %q", got)
	}
	if hasToolsFlag([]string{"--model", "m"}) {
		t.Fatal("no --tools there")
	}
}

func TestPlainLeanHasNoToolsFlag(t *testing.T) {
	t.Parallel()
	got, err := leanArgs([]string{"go"}, nil, "", nil, leanKit{}, "", os.ReadFile, testHooks())
	if err != nil {
		t.Fatal(err)
	}
	if count(got, "--tools") != 0 {
		t.Fatalf("lean must not add --tools: %q", got)
	}
}

func TestSuperleanOptionsFromTheRequestAndFromTheCard(t *testing.T) {
	t.Parallel()
	on, off := true, false
	lean, _, kit, sl := leanOptions(shared.LaunchRequest{Superlean: &on}, nil, "")
	if !lean || !sl || !kit.Superlean {
		t.Fatalf("superlean:true starts lean and superlean: %v %v %+v", lean, sl, kit)
	}
	card := &store.Task{Tags: leanTags(nil, kit)}
	if !small.HasTag(card.Tags, LeanTag) || !small.HasTag(card.Tags, SuperleanTag) {
		t.Fatalf("tags %v", card.Tags)
	}
	if lean, _, _, sl = leanOptions(shared.LaunchRequest{}, card, ""); !lean || !sl {
		t.Fatalf("a reopen reads the tag: %v %v", lean, sl)
	}
	if lean, _, _, sl = leanOptions(shared.LaunchRequest{Superlean: &off}, card, ""); !lean || sl {
		t.Fatalf("superlean:false comes back lean: %v %v", lean, sl)
	}
	if lean, _, _, sl = leanOptions(shared.LaunchRequest{Lean: &off}, card, ""); lean || sl {
		t.Fatalf("lean:false is neither: %v %v", lean, sl)
	}
	lean, _, _, sl = leanOptions(shared.LaunchRequest{}, &store.Task{Tags: []string{LeanTag}}, "")
	if !lean || sl {
		t.Fatalf("a plain lean card is not superlean: %v %v", lean, sl)
	}
	if _, _, _, sl = leanOptions(shared.LaunchRequest{}, &store.Task{Tags: []string{SuperleanTag}}, ""); sl {
		t.Fatal("the superlean tag alone, without the lean tag, is not read: lean is the card's gate")
	}
}

func TestSuperleanTagsComeOffTheWayTheDesignSays(t *testing.T) {
	t.Parallel()
	tags := append([]string{"docs"}, leanTags([]string{"ziti"}, leanKit{Superlean: true})...)
	got, cut := withoutLeanTags(tags)
	if !cut || strings.Join(got, ",") != "docs" {
		t.Fatalf("lean:false takes both off, got %v", got)
	}
	only := withoutSuperleanTag(tags)
	if small.HasTag(only, SuperleanTag) || !small.HasTag(only, LeanTag) || !small.HasTag(only, leanMCPTagPrefix+"ziti") {
		t.Fatalf("superlean:false takes only its own tag off, got %v", only)
	}
}

func TestForkOfASuperleanCardCarriesToolsAndMatchesTheLaunch(t *testing.T) {
	hk, _, _, _ := kitDirs(t)
	hk.readUserSettings = func() []byte { return []byte(`{}`) }
	f := newKAFix(t)
	if err := f.st.SetTags(f.task.ID, mergeTags([]string{OriginAgentTag}, leanTags(nil, leanKit{Superlean: true}))); err != nil {
		t.Fatal(err)
	}
	card, err := f.st.Get(f.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	h := &store.Harness{ID: "claude", Label: "claude", Cmd: "claude", LaunchMode: store.LaunchPTY,
		Args: []string{"go"}, ResumeArgs: []string{"--resume", "{resume}"}}
	fork, err := forkCardArgs(f.st, h, card, hk)
	if err != nil {
		t.Fatal(err)
	}
	if flagValue(t, fork, "--tools") != "Read,Edit,Write,Grep,Glob,Bash,PowerShell,ToolSearch" ||
		flagValue(t, fork, "--append-system-prompt") != superleanSystemPrompt {
		t.Fatalf("fork argv misses the superlean flags: %q", fork)
	}
	lean, mcp, kit, _ := leanOptions(shared.LaunchRequest{}, card, f.st.LeanWorkerGateway())
	if !lean {
		t.Fatal("the card is lean")
	}
	args, _, err := runnerArgsWith(h, card.ResumeID, "", launchOptions{Args: card.LaunchArgs})
	if err != nil {
		t.Fatal(err)
	}
	launch, err := leanArgs(args, hk.readUserSettings(), "", mcp, kit, f.st.LeanWorkerGateway(), os.ReadFile, hk)
	if err != nil {
		t.Fatal(err)
	}
	if want := cycle.KeepFlags(launch); !reflect.DeepEqual(fork, want) {
		t.Fatalf("fork and launch differ:\n%q\n%q", fork, want)
	}
}

// A launch that asks for something superlean cannot be is refused before anything starts.
func TestSuperleanRefusals(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	if _, err := d.st.SaveHarness(store.Harness{ID: "sh", Label: "sh", Enabled: true, Cmd: "sh",
		Args: []string{"-c", "sleep 60"}, LaunchMode: store.LaunchPTY}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.st.SaveHarness(store.Harness{ID: "claude", Label: "claude", Enabled: true, Cmd: "claude",
		ModelArgs: []string{"--model", "{model}"}, LaunchMode: store.LaunchPTY}); err != nil {
		t.Fatal(err)
	}
	h := d.BoardHandler()
	cwd := strings.ReplaceAll(t.TempDir(), `\`, `/`)
	for _, c := range []struct{ harness, extra, says string }{
		{"sh", `"superlean":true`, "cannot start superlean"},
		{"claude", `"superlean":true,"lean":false`, "lean: false"},
		{"claude", `"superlean":true,"tags":["` + InterviewTag + `"]`, "interview"},
	} {
		w := send(h, "POST", "/v1/launch", `{"harness":"`+c.harness+`","cwd":"`+cwd+`",`+c.extra+`}`)
		if w.Code == http.StatusOK || !strings.Contains(w.Body.String(), c.says) {
			t.Errorf("%s %s: answered %d: %s", c.harness, c.extra, w.Code, w.Body)
		}
	}
}

// A launch onto a superlean card goes through the retag block: `superlean: false` leaves the card lean and takes
// only atrium:superlean off, `lean: false` takes both.
func TestSuperleanFalseOnALaunchTakesOnlyTheSuperleanTagOff(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	for _, c := range []struct {
		extra string
		want  []string
	}{
		{`,"superlean":false`, []string{LeanTag}},
		{`,"lean":false`, nil},
		{``, []string{LeanTag, SuperleanTag}},
	} {
		d := testDaemon(t)
		dir := t.TempDir()
		// A stand-in named claude, so the launch is a claude launch that starts something that waits.
		exe, body := "claude", "#!/bin/sh\nsleep 60\n"
		if runtime.GOOS == "windows" {
			exe, body = "claude.cmd", "@echo off\r\nping -n 60 127.0.0.1 >nul\r\n"
		}
		if err := os.WriteFile(filepath.Join(dir, exe), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := d.st.SaveHarness(store.Harness{ID: "slx", Label: "slx", Enabled: true,
			Cmd: filepath.ToSlash(filepath.Join(dir, exe)), ModelArgs: []string{"--model", "{model}"},
			LaunchMode: store.LaunchPTY}); err != nil {
			t.Fatal(err)
		}
		card, _, err := d.st.Register(store.Observed{WireName: "sl-card", Worktree: t.TempDir(), Runner: "slx"})
		if err != nil {
			t.Fatal(err)
		}
		if err := d.st.SetTags(card.ID, mergeTags([]string{"docs"}, leanTags(nil, leanKit{Superlean: true}))); err != nil {
			t.Fatal(err)
		}
		w := send(d.BoardHandler(), "POST", "/v1/launch", `{"harness":"slx","cwd":"`+
			strings.ReplaceAll(card.Worktree, `\`, `/`)+`","task_id":"`+card.ID+`"`+c.extra+`}`)
		if w.Code != http.StatusOK {
			t.Skipf("could not start onto the card, answered %d: %s", w.Code, w.Body)
		}
		got, err := d.st.Get(card.ID)
		if err != nil {
			t.Fatal(err)
		}
		_ = d.StopRunner(card.ID)
		want := append([]string{"docs"}, c.want...)
		if len(got.Tags) != len(want) {
			t.Fatalf("%q: tags are %q, want %q", c.extra, got.Tags, want)
		}
		for _, tag := range want {
			if !small.HasTag(got.Tags, tag) {
				t.Fatalf("%q: tags are %q, want %q", c.extra, got.Tags, want)
			}
		}
	}
}
