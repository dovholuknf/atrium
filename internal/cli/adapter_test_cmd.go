package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

// The conformance kit for runner contract v1, `docs/runtime/runner-contract.md`.
//
// A fake room answers on a loopback port and ATRIUM_LOCATION points at it. The adapter is run once per scenario and
// has to leave the session alive and the exit code 0 whatever the room does: answer, refuse, hang or talk garbage.

const (
	scenarioOK      = "ok"
	scenarioRefuse  = "refuse"
	scenarioHang    = "hang"
	scenarioGarbage = "garbage"
)

var adapterScenarios = []string{scenarioOK, scenarioRefuse, scenarioHang, scenarioGarbage}

// contractFeatures are the calls an adapter may claim, in the order they are reported.
var contractFeatures = []string{"session", "activity", "permission", "stop", "finish"}

type kitCall struct {
	method, path string
	body         []byte
}

// kitRoom is the agent listener of a room that can be told to misbehave. It records every call before it answers.
type kitRoom struct {
	mode string
	srv  *httptest.Server
	done chan struct{}

	mu    sync.Mutex
	calls []kitCall
}

func newKitRoom(mode string) *kitRoom {
	r := &kitRoom{mode: mode, done: make(chan struct{})}
	r.srv = httptest.NewServer(http.HandlerFunc(r.serve))
	return r
}

func (r *kitRoom) close() {
	close(r.done)
	r.srv.Close()
}

func (r *kitRoom) seen() []kitCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]kitCall(nil), r.calls...)
}

func (r *kitRoom) serve(w http.ResponseWriter, req *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(req.Body, 1<<20))
	r.mu.Lock()
	r.calls = append(r.calls, kitCall{req.Method, req.URL.Path, body})
	r.mu.Unlock()

	switch {
	case r.mode == scenarioRefuse:
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				_ = conn.Close()
			}
		}
		return
	case r.mode == scenarioGarbage:
		_, _ = w.Write([]byte("<<not json>>"))
		return
	case r.mode == scenarioHang && req.URL.Path != "/permission":
		// A permission call is a long poll by design, so it is the one call a hang does not apply to.
		select {
		case <-req.Context().Done():
		case <-r.done:
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch req.URL.Path {
	case "/contract":
		_, _ = w.Write([]byte(`{"versions":[1]}`))
	case "/gate":
		_, _ = w.Write([]byte(`{"gate":true}`))
	case "/session":
		_, _ = w.Write([]byte(`{"ok":true,"context":"kit context"}`))
	case "/permission":
		_, _ = w.Write([]byte(`{"decision":"block","reason":"kit says no"}`))
	case "/stop":
		_, _ = w.Write([]byte(`{"decision":"block","reason":"kit message"}`))
	case "/finish":
		_, _ = w.Write([]byte(`{"recorded":true,"task_id":"kit-card","status":"done"}`))
	default:
		_, _ = w.Write([]byte(`{"ok":true}`))
	}
}

// adapterStep is one thing the built-in driver does as the runner would.
type adapterStep struct {
	args  []string
	stdin string
	// want checks what the step printed in the ok scenario. Every other scenario must print nothing that blocks.
	want func(out string) string
}

// claudeSteps drives today's claude hooks the way claude runs them.
func claudeSteps(cwd string) []adapterStep {
	payload := func(event, extra string) string {
		return fmt.Sprintf(`{"hook_event_name":%q,"session_id":"kit-session","cwd":%q%s}`, event, filepath.ToSlash(cwd), extra)
	}
	tool := `,"tool_name":"Bash","tool_input":{"command":"rm kit"},"tool_use_id":"kit-1"`
	return []adapterStep{
		{args: []string{"session", "--event", "start"}, stdin: payload("SessionStart", `,"source":"startup"`),
			want: func(out string) string {
				if !strings.Contains(out, "kit context") {
					return "session start did not hand the model the room's context"
				}
				return ""
			}},
		{args: []string{"hook", "--event", "tool-start"}, stdin: payload("PreToolUse", tool)},
		{args: []string{"hook", "--event", "permission"}, stdin: payload("PreToolUse", tool),
			want: func(out string) string {
				if !strings.Contains(out, `"permissionDecision":"deny"`) {
					return "a blocked tool call did not come out as a deny"
				}
				return ""
			}},
		{args: []string{"hook", "--event", "tool-end"}, stdin: payload("PostToolUse", tool)},
		{args: []string{"turn", "--event", "end"}, stdin: payload("Stop", ""),
			want: func(out string) string {
				if !strings.Contains(out, `"decision":"block"`) || !strings.Contains(out, "kit message") {
					return "a queued message at turn end did not come out as a block"
				}
				return ""
			}},
		{args: []string{"session", "--event", "end"}, stdin: payload("SessionEnd", "")},
	}
}

type adapterKit struct {
	// cmd is the shell command that drives the adapter. Empty means the built-in claude driver.
	cmd     string
	exe     string
	timeout time.Duration
	out     io.Writer
}

type scenarioResult struct {
	scenario string
	faults   []string
	features map[string]bool
	took     time.Duration
}

func (k adapterKit) run() []scenarioResult {
	var results []scenarioResult
	for _, sc := range adapterScenarios {
		results = append(results, k.runScenario(sc))
	}
	return results
}

func (k adapterKit) runScenario(sc string) scenarioResult {
	res := scenarioResult{scenario: sc, features: map[string]bool{}}
	room := newKitRoom(sc)
	defer room.close()

	dir, err := os.MkdirTemp("", "atrium-adapter-")
	if err != nil {
		res.faults = append(res.faults, err.Error())
		return res
	}
	defer os.RemoveAll(dir)
	loc := filepath.Join(dir, "location.json")
	b, _ := json.Marshal(map[string]string{"agent": room.srv.URL, "board": room.srv.URL})
	if err := os.WriteFile(loc, b, 0o600); err != nil {
		res.faults = append(res.faults, err.Error())
		return res
	}

	env := adapterEnv(loc, sc)
	ctx, cancel := context.WithTimeout(context.Background(), k.timeout)
	defer cancel()
	start := time.Now()
	if k.cmd != "" {
		if out, err := runDriver(ctx, env, "", dir, shellCommand(k.cmd)); err != nil {
			res.faults = append(res.faults, fmt.Sprintf("the adapter command failed: %v %s", err, strings.TrimSpace(out)))
		}
	} else {
		for _, st := range claudeSteps(dir) {
			out, err := runDriver(ctx, env, st.stdin, dir, append([]string{k.exe}, st.args...))
			name := strings.Join(st.args, " ")
			if err != nil {
				res.faults = append(res.faults, fmt.Sprintf("%s: %v", name, err))
				continue
			}
			if sc == scenarioOK && st.want != nil {
				if f := st.want(out); f != "" {
					res.faults = append(res.faults, name+": "+f)
				}
			} else if sc != scenarioOK && (strings.Contains(out, `"block"`) || strings.Contains(out, `"deny"`)) {
				res.faults = append(res.faults, name+": blocked on an answer the room never gave: "+strings.TrimSpace(out))
			}
		}
	}
	res.took = time.Since(start)
	res.faults = append(res.faults, checkCalls(room.seen(), res.features)...)
	return res
}

// adapterEnv is the environment an adapter runs in: this one's, with the room's address replaced.
func adapterEnv(location, scenario string) []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "ATRIUM_HUB_URL", "ATRIUM_LOCATION", "ATRIUM_TASK_ID", "ATRIUM_AGENT_NAME", "ATRIUM_PERM_GATE", "ATRIUM_RUNNER":
			continue
		}
		env = append(env, kv)
	}
	return append(env, "ATRIUM_LOCATION="+location, "ATRIUM_TASK_ID=kit-card", "ATRIUM_AGENT_NAME=kit-agent",
		"ATRIUM_PERM_GATE=on", "ATRIUM_ADAPTER_SCENARIO="+scenario)
}

func shellCommand(cmd string) []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd", "/C", cmd}
	}
	return []string{"sh", "-c", cmd}
}

// runDriver runs one process to exit under the kit's deadline. A non-zero exit and a deadline are both faults, since
// a hook that exits non-zero or outlives its bound fails the session it rides.
func runDriver(ctx context.Context, env []string, stdin, dir string, argv []string) (string, error) {
	c := exec.CommandContext(ctx, argv[0], argv[1:]...)
	c.Env = env
	c.Dir = dir
	c.Stdin = strings.NewReader(stdin)
	var out bytes.Buffer
	c.Stdout = &out
	c.Stderr = &out
	c.WaitDelay = time.Second
	err := c.Run()
	if ctx.Err() != nil {
		return out.String(), fmt.Errorf("did not finish in the kit's deadline")
	}
	if err != nil {
		return out.String(), err
	}
	return out.String(), nil
}

// checkCalls validates every body the room received against its schema, fills in the features seen, and holds an
// adapter to the features it claimed on session start.
func checkCalls(calls []kitCall, seen map[string]bool) []string {
	schemas := map[string]string{"/session": "session", "/activity": "activity", "/permission": "permission",
		"/stop": "stop", "/finish": "finish"}
	var faults []string
	reported := map[string]bool{}
	var claimed []string
	for _, c := range calls {
		name, ok := schemas[c.path]
		if !ok {
			continue
		}
		if c.method != http.MethodPost {
			faults = append(faults, fmt.Sprintf("%s was sent as %s, not POST", c.path, c.method))
			continue
		}
		seen[name] = true
		for _, f := range validateAgainst(name, c.body) {
			line := c.path + ": " + f
			if !reported[line] {
				reported[line] = true
				faults = append(faults, line)
			}
		}
		if name == "session" {
			var s struct {
				Features []string `json:"features"`
			}
			if json.Unmarshal(c.body, &s) == nil {
				claimed = append(claimed, s.Features...)
			}
		}
	}
	for _, f := range claimed {
		if !seen[f] {
			faults = append(faults, "claimed "+f+" in features and never made the call")
		}
	}
	return faults
}

func newAdapter() *cobra.Command {
	c := &cobra.Command{
		Use:   "adapter",
		Short: "Work with runner adapters.",
	}
	c.AddCommand(newAdapterTest())
	return c
}

func newAdapterTest() *cobra.Command {
	var cmdline, runner string
	var timeout time.Duration
	c := &cobra.Command{
		Use:   "test",
		Short: "Run the runner contract conformance kit against an adapter.",
		Long: "Starts a fake room on a loopback port, points ATRIUM_LOCATION at it and runs the adapter four times: " +
			"the room answering, refusing, hanging and answering garbage. The adapter passes when every run exits 0 " +
			"inside the deadline and every body it sent matches the contract's schema. See docs/runtime/runner-contract.md.\n\n" +
			"--cmd is a shell command that drives one scripted session through the runner: a session start, a tool " +
			"call, a turn end and a session end. It gets ATRIUM_ADAPTER_SCENARIO, ATRIUM_TASK_ID and ATRIUM_AGENT_NAME. " +
			"--runner claude drives atrium's own claude hooks instead.",
		Args: cobra.NoArgs,
		RunE: speaksForItself(func(cmd *cobra.Command, _ []string) error {
			if (cmdline == "") == (runner == "") {
				return fmt.Errorf("give exactly one of --cmd or --runner claude")
			}
			if runner != "" && runner != "claude" {
				return fmt.Errorf("--runner %q has no built-in driver, use --cmd", runner)
			}
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			k := adapterKit{cmd: cmdline, exe: exe, timeout: timeout, out: cmd.OutOrStdout()}
			return reportAdapter(k.out, k.run())
		}),
	}
	c.Flags().StringVar(&cmdline, "cmd", "", "shell command that drives one scripted session through the runner")
	c.Flags().StringVar(&runner, "runner", "", "test a built-in driver instead: claude")
	c.Flags().DurationVar(&timeout, "timeout", 30*time.Second, "deadline for one run of the adapter")
	return c
}

func reportAdapter(w io.Writer, results []scenarioResult) error {
	failed := false
	for _, r := range results {
		if len(r.faults) == 0 {
			fmt.Fprintf(w, "pass  %-8s %s\n", r.scenario, r.took.Round(time.Millisecond))
			continue
		}
		failed = true
		fmt.Fprintf(w, "FAIL  %-8s %s\n", r.scenario, r.took.Round(time.Millisecond))
		for _, f := range r.faults {
			fmt.Fprintf(w, "        %s\n", f)
		}
	}
	for _, r := range results {
		if r.scenario != scenarioOK {
			continue
		}
		var parts []string
		for _, f := range contractFeatures {
			state := "no"
			if r.features[f] {
				state = "yes"
			}
			parts = append(parts, f+" "+state)
		}
		fmt.Fprintf(w, "features: %s\n", strings.Join(parts, ", "))
	}
	if failed {
		return alreadySaid("the adapter does not conform to runner contract v1")
	}
	fmt.Fprintln(w, "the adapter conforms to runner contract v1")
	return nil
}
