package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// POST /v1/preflight: what THIS room process can see and run.
//
// Every toolchain fact that mattered about a room was a fact about the room
// process, and ssh sees a different PATH. So the room answers for itself, from
// inside itself, with its own environment.
//
// THE CALLER NAMES KEYS, NEVER COMMANDS. That is the whole design. This route is
// on the human listener, which anyone who can reach the board can reach,
// overlays included. A verb that ran what its body named would be remote
// command execution on every room. So the body holds keys, a table compiled
// into the binary maps each key to a fixed argv, and a key the table does not
// know gets a PATH lookup and NO exec. Nothing a body says can reach exec.
//
// `env_present` answers a boolean per name and never a value.
//
// Not on the agent listener, which every session can reach.
// See docs/fabric/room-requirements-design.md, section 4.

const (
	// preflightEach bounds one command, preflightTotal the whole request.
	preflightEach  = 10 * time.Second
	preflightTotal = 60 * time.Second

	// preflightOutput caps what one command may say, kept WHILE reading as a
	// source's output is. A status command prints a few lines.
	preflightOutput = 4 << 10

	// preflightMaxKeys bounds each list in the body. Unknown keys cost only a
	// PATH lookup, but a list is still not the place for a megabyte.
	preflightMaxKeys = 64
)

// preflightTools is the fixed command behind each tool key.
var preflightTools = map[string][]string{
	"go":    {"go", "version"},
	"node":  {"node", "--version"},
	"git":   {"git", "--version"},
	"pwsh":  {"pwsh", "-v"},
	"npm":   {"npm", "--version"},
	"cmake": {"cmake", "--version"},
	"make":  {"make", "--version"},
	"gh":    {"gh", "--version"},
	"cargo": {"cargo", "--version"},
}

// preflightRunners is the fixed sign-in check behind each runner key. The
// binary is resolved the way the room resolves a harness row, so the check runs
// the one the room would start. Only the arguments live here.
var preflightRunners = map[string][]string{
	"claude": {"auth", "status"},
	"codex":  {"login", "status"},
}

// The seams. A test replaces these to count what would have been executed and
// to stand in for a slow command.
var (
	preflightLook = exec.LookPath
	preflightRun  = runPreflightCommand
	// The timing is a var only so a test need not wait a minute.
	preflightEachFor  = preflightEach
	preflightTotalFor = preflightTotal
)

type preflightRequest struct {
	RunnerAuth []string `json:"runner_auth"`
	Tools      []string `json:"tools"`
	EnvPresent []string `json:"env_present"`
	// Forges names the forge CLIs to ask for their login status, on a room with no hub. A room with a hub answers that
	// the hub keeps them.
	Forges []preflightForge `json:"forges"`
}

type preflightForge struct {
	Tool   string   `json:"tool"`
	Host   string   `json:"host"`
	Scopes []string `json:"scopes"`
}

type preflightItem struct {
	OK     bool   `json:"ok"`
	Path   string `json:"path"`
	Output string `json:"output"`
	Error  string `json:"error"`
}

type preflightAnswer struct {
	Tools      map[string]preflightItem `json:"tools"`
	RunnerAuth map[string]preflightItem `json:"runner_auth"`
	EnvPresent map[string]bool          `json:"env_present"`
	// Forges is keyed tool@host. A failure also raises the board alert.
	Forges    map[string]forgeStatus `json:"forges"`
	PID       int                    `json:"pid"`
	StartedBy string                 `json:"started_by"`
}

// runPreflightCommand runs one resolved command, from this process's own
// environment, with no shell, and bounded while reading.
func runPreflightCommand(ctx context.Context, exe string, args []string, out io.Writer) error {
	exe, args = viaShellIfScript(exe, args)
	cmd := exec.CommandContext(ctx, exe, args...)
	hideWindow(cmd)
	cmd.Stdout = out
	cmd.Stderr = out
	// A grandchild holding the pipe open must not outlive the timeout.
	cmd.WaitDelay = 2 * time.Second
	return cmd.Run()
}

func (d *Daemon) handlePreflight(w http.ResponseWriter, r *http.Request) {
	var req preflightRequest
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, "body must be JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.RunnerAuth) > preflightMaxKeys || len(req.Tools) > preflightMaxKeys ||
		len(req.EnvPresent) > preflightMaxKeys || len(req.Forges) > preflightMaxKeys {
		http.Error(w, "too many keys in one request", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), preflightTotalFor)
	defer cancel()

	ans := preflightAnswer{
		Tools:      map[string]preflightItem{},
		RunnerAuth: map[string]preflightItem{},
		EnvPresent: map[string]bool{},
		Forges:     map[string]forgeStatus{},
		PID:        os.Getpid(),
		StartedBy:  d.opts.StartedBy,
	}
	for _, k := range req.Tools {
		if _, done := ans.Tools[k]; !done {
			ans.Tools[k] = d.preflightOne(ctx, k, preflightTools[k], k)
		}
	}
	for _, k := range req.RunnerAuth {
		if _, done := ans.RunnerAuth[k]; done {
			continue
		}
		args, known := preflightRunners[k]
		if !known {
			ans.RunnerAuth[k] = d.preflightOne(ctx, k, nil, k)
			continue
		}
		// The binary the room would start: a harness row's explicit path first,
		// else its command, else the key itself.
		exe := k
		if h, err := d.st.Harness(k); err == nil && h != nil {
			exe = h.Exe()
		}
		ans.RunnerAuth[k] = d.preflightOne(ctx, k, append([]string{exe}, args...), exe)
	}
	for _, fg := range req.Forges {
		if d.HubForge() != nil {
			// A room with a hub never runs a forge CLI. The logins are the hub's, and so is their check.
			ans.Forges[fg.Tool+"@"+fg.Host] = forgeStatus{Tool: fg.Tool, Host: fg.Host, State: forgeUnknown,
				Message: "forge logins are the hub's, nothing was run here. check them on the hub with POST /_hub/forge/check"}
			continue
		}
		if fg.Host != "" && !store.ValidForgeHost(fg.Host) {
			ans.Forges[fg.Tool+"@"+fg.Host] = forgeStatus{Tool: fg.Tool, Host: fg.Host, State: forgeUnknown, Message: "not a host name, nothing was run"}
			continue
		}
		st := d.checkForge(ctx, fg.Tool, fg.Host, fg.Scopes)
		d.applyForge(st)
		ans.Forges[st.Tool+"@"+st.Host] = st
	}
	for _, n := range req.EnvPresent {
		// Presence only. The value is never read into anything that leaves.
		_, ok := os.LookupEnv(n)
		ans.EnvPresent[n] = ok
	}
	writeJSONBody(w, ans)
}

// preflightOne answers one key. argv is nil for a key the table does not know,
// which is resolved and NEVER executed.
func (d *Daemon) preflightOne(ctx context.Context, key string, argv []string, lookup string) preflightItem {
	path, err := preflightLook(lookup)
	if err != nil {
		return preflightItem{Error: "not found"}
	}
	if argv == nil {
		return preflightItem{Path: path, Error: "unknown key, resolved at " + path}
	}
	if ctx.Err() != nil {
		return preflightItem{Path: path, Error: "timeout"}
	}
	cctx, cancel := context.WithTimeout(ctx, preflightEachFor)
	defer cancel()
	var buf bytes.Buffer
	err = preflightRun(cctx, path, argv[1:], &limitedWriter{w: &buf, left: preflightOutput})
	item := preflightItem{Path: path, Output: strings.TrimSpace(buf.String())}
	switch {
	case cctx.Err() != nil:
		item.Error = "timeout"
	case err != nil:
		item.Error = err.Error()
	default:
		item.OK = true
	}
	return item
}

func writeJSONBody(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
