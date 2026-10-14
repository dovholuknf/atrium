package guard

import (
	"strings"
	"testing"
)

const deployFence = "Run this:\n\n```powershell\npwsh scripts/live/atrium-deploy.ps1 -All\n```\n"

func replyIn(text string) Input { return Input{ToolName: ReplyTool, LastAssistantMessage: text} }

func TestReplyHandingADeployCommandInAFenceIsDenied(t *testing.T) {
	rs, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{
		deployFence,
		"```\nscripts/live/deploy-batch.ps1\n```",
		"```\nStart-Process pwsh -ArgumentList 'x deploy'\n```",
		"```\r\ndeploy-hub-only.ps1\r\n```",
	} {
		d := rs.Evaluate(replyIn(text), env0)
		if d.Action != "deny" || d.Rule != "reply-hands-deploy-command" {
			t.Errorf("%q: got %+v", text, d)
		}
	}
	d := rs.Evaluate(replyIn(deployFence), env0)
	if !strings.Contains(d.Reason, "atrium-deploy.ps1") {
		t.Errorf("the reason does not name what matched: %q", d.Reason)
	}
}

func TestReplyQuotingADeployScriptInProseIsLeftAlone(t *testing.T) {
	rs, _ := Builtin()
	for _, text := range []string{
		"I ran scripts/live/atrium-deploy.ps1 myself and it passed.",
		"the header of `deploy-batch.ps1` explains it",
		"```\ngo test ./...\n```\nthen atrium-deploy.ps1 later",
		"",
	} {
		if d := rs.Evaluate(replyIn(text), env0); d.Action != "" {
			t.Errorf("%q: got %+v", text, d)
		}
	}
}

// A reply rule never answers a tool call, and a tool call rule never a reply.
func TestReplyRulesAndToolRulesStayApart(t *testing.T) {
	rs, _ := Builtin()
	in := Input{ToolName: "Bash"}
	in.ToolInput.Command = "echo atrium-deploy.ps1"
	in.LastAssistantMessage = deployFence
	if d := rs.Evaluate(in, env0); d.Rule == "reply-hands-deploy-command" {
		t.Errorf("a Bash call was answered by a reply rule: %+v", d)
	}
	r := replyIn("")
	r.ToolInput.Command = "mkdir D:/zz-new"
	if d := rs.Evaluate(r, env0); d.Action != "" {
		t.Errorf("a reply was answered by a tool rule: %+v", d)
	}
}

func TestReplyRuleMixedWithAToolCallFailsTheLoad(t *testing.T) {
	for name, raw := range map[string]string{
		"mixed tools":              `{"rules":[{"id":"x","check":"reply-pattern","tools":["reply","Bash"],"reason":"r","params":{"patterns":["a"]}}]}`,
		"reply-pattern on a tool":  `{"rules":[{"id":"x","check":"reply-pattern","tools":["Bash"],"reason":"r","params":{"patterns":["a"]}}]}`,
		"reply with another check": `{"rules":[{"id":"x","check":"phrase","tools":["reply"],"reason":"r","params":{"phrase":"a"}}]}`,
	} {
		if _, err := Load([]byte(raw)); err == nil {
			t.Errorf("%s: loaded", name)
		}
	}
}

func TestReplyExceptLetsALineGo(t *testing.T) {
	raw := `{"rules":[{"id":"x","check":"reply-pattern","tools":["reply"],"reason":"r {name}",
		"params":{"patterns":["foo\\d"],"except":["# fine"]}}]}`
	rs, err := Load([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if d := rs.Evaluate(replyIn("foo1 # fine"), env0); d.Action != "" {
		t.Errorf("except ignored: %+v", d)
	}
	if d := rs.Evaluate(replyIn("x\nfoo2"), env0); d.Action != "deny" || d.Reason != "r foo2" {
		t.Errorf("whole-reply scope: %+v", d)
	}
}

func TestReplyCheckThatPanicsFailsOpen(t *testing.T) {
	rs, _ := Builtin()
	rs.table = map[string]checkFunc{"reply-pattern": func(*evalCtx, *Rule) (bool, string) { panic("boom") }}
	if d := rs.Evaluate(replyIn(deployFence), env0); d.Action != "" {
		t.Errorf("a panic denied a reply: %+v", d)
	}
	in := Input{ToolName: "Bash"}
	in.ToolInput.Command = "x"
	rs2, _ := Load([]byte(`{"rules":[{"id":"x","check":"phrase","tools":["shell"],"reason":"r","params":{"phrase":"x"}}]}`))
	rs2.table = map[string]checkFunc{"phrase": func(*evalCtx, *Rule) (bool, string) { panic("boom") }}
	if d := rs2.Evaluate(in, env0); d.Action != "deny" {
		t.Errorf("a tool call must still fail closed: %+v", d)
	}
}
