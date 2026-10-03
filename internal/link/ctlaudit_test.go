package link

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// auditLine is one call recorded by the fake audit func.
type auditLine struct{ room, kind, detail string }

// ctlAuditHarness is a control server whose audit lines are kept, over a board
// that answers what these tools ask of it.
func ctlAuditHarness(t *testing.T, tasks []map[string]any) (*controlMCP, *[]auditLine) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/tasks":
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": tasks})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/launch":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "new1", "wire_name": "kid"})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/exit"):
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/cull"):
			_ = json.NewEncoder(w).Encode(map[string]any{"exited": true, "worktree_removed": true, "branch_deleted": true})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/restart-wake"):
			_ = json.NewEncoder(w).Encode(map[string]any{"queued": true})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/model"):
			var in struct{ Model, From string }
			_ = json.NewDecoder(r.Body).Decode(&in)
			_ = json.NewEncoder(w).Encode(map[string]any{"model": in.Model, "from": "sonnet", "typed": true,
				"delivered": "terminal", "when": "immediate", "saw_from": in.From})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/message"):
			_ = json.NewEncoder(w).Encode(map[string]any{"delivered": "queued"})
		case r.Method == http.MethodPatch:
			_ = json.NewEncoder(w).Encode(map[string]any{"task": map[string]any{"id": "w1", "wire_name": "sa36", "alias": "x"}})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/tasks/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "w1", "wire_name": "sa36"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	var lines []auditLine
	c := &controlMCP{board: srv.URL, client: srv.Client(),
		audit: func(room, kind, detail string) { lines = append(lines, auditLine{room, kind, detail}) }}
	return c, &lines
}

var ctlAuditTasks = []map[string]any{
	{"id": "w1", "wire_name": "sa36", "status": "needs-input"},
	{"id": "o1", "wire_name": "orch", "status": "working"},
}

func onlyLine(t *testing.T, lines *[]auditLine, room, kind string) string {
	t.Helper()
	if len(*lines) != 1 {
		t.Fatalf("lines = %+v, want exactly one", *lines)
	}
	l := (*lines)[0]
	if l.room != room || l.kind != kind {
		t.Fatalf("line = %+v, want room %q kind %q", l, room, kind)
	}
	if !strings.Contains(l.detail, "(claimed)") {
		t.Errorf("detail %q does not say it is claimed", l.detail)
	}
	return l.detail
}

func TestAuditedToolsWriteOneLineEach(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name, kind, want string
		call             func(c *controlMCP) error
	}{
		{"launch", "ctl-launch", "by orch@beta (claimed): launch claude as new1, ok", func(c *controlMCP) error {
			_, _, err := audited(c, "ctl-launch", describeLaunch, c.launchHandler)(ctx, ctlReq("orch", "beta"),
				launchInput{Cwd: "/work/dir"})
			return err
		}},
		{"exit", "ctl-exit", "by orch@beta (claimed): exit w1, ok", func(c *controlMCP) error {
			_, _, err := audited(c, "ctl-exit", describeExit, c.exitHandler)(ctx, ctlReq("orch", "beta"),
				exitInput{Card: "sa36"})
			return err
		}},
		{"cull", "ctl-cull", "by orch@beta (claimed): cull w1 into claude/fabric, ok", func(c *controlMCP) error {
			_, _, err := audited(c, "ctl-cull", describeCull, c.cullHandler)(ctx, ctlReq("orch", "beta"),
				cullInput{Card: "sa36", Into: "claude/fabric"})
			return err
		}},
		{"restart", "ctl-restart", "by orch@beta (claimed): restart, could not ask beta to restart", func(c *controlMCP) error {
			// No hub, so the restart cannot be forwarded: a failed call, which is a line too.
			_, _, err := audited(c, "ctl-restart", describeRestart, c.restartHandler)(ctx, ctlReq("orch", "beta"),
				restartInput{})
			return err
		}},
		{"wake", "ctl-wake", "by orch@beta (claimed): wake queued for own card o1, ok", func(c *controlMCP) error {
			_, _, err := audited(c, "ctl-wake", describeWake, c.wakeHandler)(ctx, ctlReq("orch", "beta"),
				wakeInput{Text: "SECRET-WAKE-TEXT"})
			return err
		}},
		{"alias", "ctl-alias", "by orch@beta (claimed): alias set to x on w1, ok", func(c *controlMCP) error {
			_, _, err := audited(c, "ctl-alias", describeAlias, c.aliasHandler)(ctx, ctlReq("orch", "beta"),
				aliasInput{Card: "sa36", Alias: "x"})
			return err
		}},
		{"model", "ctl-model", "by orch@beta (claimed): model w1 to opus, ok", func(c *controlMCP) error {
			_, out, err := audited(c, "ctl-model", describeModel, c.modelHandler)(ctx, ctlReq("orch", "beta"),
				modelInput{Card: "sa36", Model: "opus"})
			if err == nil && (!out.Typed || out.Model != "opus" || out.Was != "sonnet" || out.Delivered != "terminal") {
				t.Errorf("model answer = %+v", out)
			}
			return err
		}},
		{"say wake", "ctl-wake-say", "by orch@beta (claimed): say with wake to sa36, ok", func(c *controlMCP) error {
			_, _, err := audited(c, "ctl-wake-say", describeSay, c.sayHandler)(ctx, ctlReq("orch", "beta"),
				sayInput{To: "sa36", Text: "SECRET-SAY-TEXT", Wake: true})
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, lines := ctlAuditHarness(t, ctlAuditTasks)
			if tc.name == "restart" {
				c.hub = nil
			}
			err := tc.call(c)
			if tc.name == "restart" {
				if err == nil {
					t.Fatal("restart with no hub should fail")
				}
			} else if err != nil {
				t.Fatalf("call: %v", err)
			}
			detail := onlyLine(t, lines, "beta", tc.kind)
			if tc.name == "restart" {
				if !strings.HasPrefix(detail, "by orch@beta (claimed): restart, ") {
					t.Errorf("detail = %q, want the error written", detail)
				}
				return
			}
			if detail != tc.want {
				t.Errorf("detail = %q, want %q", detail, tc.want)
			}
		})
	}
}

func TestAuditedFailedCallWritesItsError(t *testing.T) {
	c, lines := ctlAuditHarness(t, ctlAuditTasks)
	_, _, err := audited(c, "ctl-exit", describeExit, c.exitHandler)(context.Background(),
		ctlReq("orch", "beta"), exitInput{Card: "nobody"})
	if err == nil {
		t.Fatal("exit of an unknown card should fail")
	}
	detail := onlyLine(t, lines, "beta", "ctl-exit")
	if !strings.Contains(detail, "no session called") || strings.Contains(detail, "\n") {
		t.Errorf("detail = %q, want the first line of the error", detail)
	}
}

func TestAuditedErrorIsBounded(t *testing.T) {
	got := auditOutcome(errString(strings.Repeat("x", 500) + "\nsecond line"))
	if len([]rune(got)) != auditErrMax || strings.Contains(got, "second") {
		t.Errorf("outcome length %d = %q, want first line cut to %d", len(got), got, auditErrMax)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestAuditedCapRefusalWritesARefusalLine(t *testing.T) {
	tasks := []map[string]any{}
	for i := 0; i < DefaultLaunchCap; i++ {
		tasks = append(tasks, map[string]any{
			"id": string(rune('a' + i)), "status": "working", "supervised": true,
			"tags": []string{OriginTag, SubagentTag},
		})
	}
	c, lines := ctlAuditHarness(t, tasks)
	_, _, err := audited(c, "ctl-launch", describeLaunch, c.launchHandler)(context.Background(),
		ctlReq("orch", "beta"), launchInput{Cwd: "/work/dir"})
	if err == nil {
		t.Fatal("at the cap, launch should be refused")
	}
	detail := onlyLine(t, lines, "beta", "ctl-launch")
	if !strings.Contains(detail, ", refused: at the launch cap of") {
		t.Errorf("detail = %q, want a refusal", detail)
	}
}

func TestAuditedBadCwdWritesARefusalLine(t *testing.T) {
	c, lines := ctlAuditHarness(t, ctlAuditTasks)
	_, _, err := audited(c, "ctl-launch", describeLaunch, c.launchHandler)(context.Background(),
		ctlReq("orch", "beta"), launchInput{})
	if err == nil {
		t.Fatal("a launch with no cwd should be refused")
	}
	if d := onlyLine(t, lines, "beta", "ctl-launch"); !strings.Contains(d, ", refused: say where to run it") {
		t.Errorf("detail = %q, want a refusal", d)
	}
}

func TestAuditedWritesNothingForReadsPlainSayAndReport(t *testing.T) {
	ctx := context.Background()
	c, lines := ctlAuditHarness(t, ctlAuditTasks)
	req := ctlReq("orch", "beta")

	_, _, _ = audited(c, "ctl-alias", describeAlias, c.aliasHandler)(ctx, req, aliasInput{Card: "sa36"})
	_, _, _ = audited(c, "ctl-wake-say", describeSay, c.sayHandler)(ctx, req, sayInput{To: "sa36", Text: "hello"})
	// Report and the read tools are not wrapped in server() at all. Prove it there.
	_, _, _ = c.reportHandler(ctx, req, reportInput{Status: "progress", Summary: "s"})
	_, _, _ = c.peersHandler(ctx, req, peersInput{})
	_, _, _ = c.statusHandler(ctx, req, statusInput{})
	_, _, _ = c.taskHandler(ctx, req, taskInput{Card: "sa36"})
	c.server(classFull)
	if len(*lines) != 0 {
		t.Fatalf("lines = %+v, want none", *lines)
	}
}

// A call on another room writes on that room, naming the caller's room in the text.
func TestAuditedCrossRoomLaunchWritesOnTheTarget(t *testing.T) {
	c, lines := ctlAuditHarness(t, ctlAuditTasks)
	c.hub = nil
	_, _, err := audited(c, "ctl-launch", describeLaunch, c.launchHandler)(context.Background(),
		ctlReq("orch", "beta"), launchInput{Cwd: "/work/dir", Room: "gamma"})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	detail := onlyLine(t, lines, "gamma", "ctl-launch")
	if !strings.HasPrefix(detail, "by orch@beta (claimed): launch claude") {
		t.Errorf("detail = %q, want the caller's room in the text", detail)
	}
}

func TestAuditedCrossRoomExitWritesOnTheTarget(t *testing.T) {
	c, lines := ctlAuditHarness(t, ctlAuditTasks)
	c.hub = nil
	_, _, _ = audited(c, "ctl-exit", describeExit, c.exitHandler)(context.Background(),
		ctlReq("orch", "beta"), exitInput{Card: "sa36@gamma"})
	if len(*lines) != 1 || (*lines)[0].room != "gamma" {
		t.Fatalf("lines = %+v, want one on gamma", *lines)
	}
}

func TestAuditedNoLineCarriesThePayload(t *testing.T) {
	ctx := context.Background()
	c, lines := ctlAuditHarness(t, ctlAuditTasks)
	req := ctlReq("orch", "beta")
	_, _, _ = audited(c, "ctl-launch", describeLaunch, c.launchHandler)(ctx, req, launchInput{
		Cwd: "/work/dir", Prompt: "SECRET-PROMPT", Brief: "SECRET-BRIEF", Why: "SECRET-WHY", Title: "SECRET-TITLE",
		Args: []string{"SECRET-ARG"}, Env: map[string]string{"K": "SECRET-ENV"},
	})
	_, _, _ = audited(c, "ctl-wake", describeWake, c.wakeHandler)(ctx, req, wakeInput{Text: "SECRET-WAKE"})
	_, _, _ = audited(c, "ctl-wake-say", describeSay, c.sayHandler)(ctx, req,
		sayInput{To: "sa36", Text: "SECRET-SAY", Wake: true})
	_, _, _ = audited(c, "ctl-restart", describeRestart, c.restartHandler)(ctx, req, restartInput{Why: "SECRET-RESTART"})
	if len(*lines) != 4 {
		t.Fatalf("lines = %+v, want four", *lines)
	}
	for _, l := range *lines {
		if strings.Contains(l.detail, "SECRET") {
			t.Errorf("%s line %q carries a payload", l.kind, l.detail)
		}
	}
}

// Best effort: no audit func, and the answer is the handler's own.
func TestAuditedWithoutAuditFuncIsTransparent(t *testing.T) {
	c, _ := ctlAuditHarness(t, ctlAuditTasks)
	c.audit = nil
	_, out, err := audited(c, "ctl-exit", describeExit, c.exitHandler)(context.Background(),
		ctlReq("orch", "beta"), exitInput{Card: "sa36"})
	if err != nil || !out.Asked {
		t.Fatalf("out = %+v, err = %v, want the handler's answer", out, err)
	}
}

// A describer that writes caller text must not let it make a huge row or a second
// line: the detail is bounded and holds no CR or LF.
func TestAuditDetailBoundsAndStripsCallerText(t *testing.T) {
	big := strings.Repeat("x", 1<<20) + "\r\nby orchestrator@sg3 (claimed): cull X, ok"
	req := ctlReq("orch", "beta")
	cases := map[string]func() (string, string, bool){
		"alias": func() (string, string, bool) { return describeAlias(req, aliasInput{Alias: big}, aliasOutput{}) },
		"cull": func() (string, string, bool) {
			return describeCull(req, cullInput{Card: "w1", Into: big}, cullOutput{})
		},
		"launch": func() (string, string, bool) { return describeLaunch(req, launchInput{Runner: big}, launchOutput{}) },
		"say": func() (string, string, bool) {
			return describeSay(req, sayInput{To: big, Wake: true}, sayOutput{})
		},
		"exit": func() (string, string, bool) { return describeExit(req, exitInput{Card: big}, exitOutput{}) },
	}
	for name, describe := range cases {
		_, what, ok := describe()
		if !ok {
			t.Fatalf("%s: wrote no line", name)
		}
		detail := auditDetail(req, what, nil)
		if strings.ContainsAny(detail, "\r\n") {
			t.Errorf("%s: detail holds a CR or LF: %.80q", name, detail)
		}
		if n := len([]rune(detail)); n > 2*auditWhatMax {
			t.Errorf("%s: detail is %d characters, want it bounded", name, n)
		}
	}
}
