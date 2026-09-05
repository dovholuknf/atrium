package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/dovholuknf/atrium/internal/daemon"
)

// Turning atrium off and on again, from inside a session atrium is running.
//
// The chicken and egg: a runner supervised by the daemon cannot restart the
// daemon, because the daemon owns that runner's pseudo terminal and closing it
// takes the runner with it. Anything that does the restart has to outlive both.
//
// This is that third party, as an MCP server. Held here rather than in the
// daemon for the reason the whole problem exists: THE DAEMON IS THE THING
// GOING AWAY. It is a stdio server like `atrium agent` and `atrium serve`, so
// it can be a subprocess of a claude session, a backend of mcp-gateway, or
// both, and none of those arrangements change what it does.
//
// **The restart is scheduled, never immediate, and that is the load-bearing
// decision.** A tool call that kills its own caller never returns: the session
// is resumed later with a tool call that has no result, which is a state
// nothing has tested and the model has no way to reason about. So the tool
// spawns a detached process, answers "scheduled", and the turn ends normally
// before the floor goes away.

// restartDelay is how long the detached restarter waits before touching
// anything.
//
// Long enough for the tool result to reach the model, the turn to end, and the
// session hook to fire. Short enough that nobody wonders whether it worked.
const restartDelay = 4 * time.Second

// stopGrace is how long to wait for the old daemon to actually be gone.
//
// `atrium stop` returns as soon as the daemon accepts the request, and the
// wind-down after that gives supervised runners ten seconds. Starting the new
// one before the port is free produces a second daemon that fails to listen
// and exits, which looks exactly like the restart having done nothing.
const stopGrace = 20 * time.Second

func newControl() *cobra.Command {
	var restartNow bool
	var db, delay string

	c := &cobra.Command{
		Use:   "control",
		Short: "MCP server for restarting atrium and asking how it is.",
		Long: "A stdio MCP server with two tools, `atrium_status` and `restart_atrium`.\n\n" +
			"Exists because a session supervised by the daemon cannot restart the daemon: " +
			"closing its pseudo terminal takes that session with it. This runs outside both, " +
			"as a subprocess of a claude session or as an mcp-gateway backend.\n\n" +
			"The restart is always scheduled a few seconds out, so the tool call returns and " +
			"the turn ends before the session is taken down.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if restartNow {
				// The detached half. Not a separate binary, so there is one
				// thing to install and it can never be a version behind.
				return runRestart(db, delay)
			}
			return controlServer().Run(cmd.Context(), &mcp.StdioTransport{})
		},
	}
	// Hidden: this is how the tool re-invokes itself detached, not something to
	// run by hand. Running it by hand stops your daemon on a timer with no
	// obvious way to call it off.
	c.Flags().BoolVar(&restartNow, "restart-now", false, "internal: perform the restart")
	c.Flags().StringVar(&db, "db", "", "internal: database the restarted daemon should open")
	c.Flags().StringVar(&delay, "delay", "", "internal: how long to wait first")
	_ = c.Flags().MarkHidden("restart-now")
	_ = c.Flags().MarkHidden("db")
	_ = c.Flags().MarkHidden("delay")

	return c
}

// ── the server ──────────────────────────────────────────────────────────────

// controlServer builds the MCP server with both tools attached.
func controlServer() *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "atrium-control", Version: "v0.0.0-dev"}, nil)

	mcp.AddTool(s, &mcp.Tool{
		Name: "atrium_status",
		Description: "Is the atrium daemon running, which database is it on, and what is on the " +
			"board. Answers without a daemon too, which is the case worth being able to see: it " +
			"reports `running:false` and where the last one was rather than failing.",
	}, statusHandler)

	mcp.AddTool(s, &mcp.Tool{
		Name: "restart_atrium",
		Description: "Wind the daemon down and bring it straight back on the same database.\n\n" +
			"SCHEDULED, not immediate. Returns at once and the restart happens a few seconds " +
			"later, because it will take down the session that asked for it: the daemon owns " +
			"that session's terminal. Say what you are doing before you call this, and expect " +
			"to be resumed rather than answered.\n\n" +
			"Supervised sessions come back from their resume ids if they are fixtures. Anything " +
			"else does not come back at all.\n\n" +
			"OTHER AGENTS ARE PARKED FIRST. A restart closes every terminal atrium owns and " +
			"kills the process in each, so any supervised session that is working is told what " +
			"is coming and given time to stop. If some are still working when that runs out, " +
			"NOTHING IS SCHEDULED and they are returned in `busy`: wait and call again, or pass " +
			"`force`. Sessions atrium does not own are unaffected by a restart and are ignored.",
	}, restartHandler)

	return s
}

// ── status ──────────────────────────────────────────────────────────────────

type StatusInput struct{}

type StatusOutput struct {
	Running bool   `json:"running"`
	Board   string `json:"board,omitempty"`
	DB      string `json:"db,omitempty"`
	PID     int    `json:"pid,omitempty"`
	// Halted is the store having failed. The daemon is up and the agent
	// listener is closed, which is a state worth reporting as its own thing
	// rather than as "running".
	Halted bool   `json:"halted,omitempty"`
	Cause  string `json:"cause,omitempty"`
	Cards  int    `json:"cards,omitempty"`
	// Waiting is how many want you right now, which is the only number worth
	// reading without opening the board.
	Waiting int `json:"waiting,omitempty"`
	Note    string `json:"note,omitempty"`
}

func statusHandler(ctx context.Context, _ *mcp.CallToolRequest, _ StatusInput) (
	*mcp.CallToolResult, StatusOutput, error) {

	out := StatusOutput{}
	loc, err := readLocation()
	if err != nil || strings.TrimSpace(loc.Board) == "" {
		out.Note = "no daemon is running. nothing has recorded an address."
		if db, ok := lastDatabase(); ok {
			out.DB = db
			out.Note = "no daemon is running. the last one was on " + db + "."
		}
		return nil, out, nil
	}
	out.Board, out.DB, out.PID = loc.Board, loc.DB, loc.PID

	// A recorded address is not a running daemon: the file survives a kill.
	// The only way to know is to ask.
	client := &http.Client{Timeout: 3 * time.Second}
	res, err := client.Get(loc.Board + "/v1/health")
	if err != nil {
		out.Note = "an address is recorded but nothing answers there. " +
			"the daemon was probably killed rather than stopped."
		return nil, out, nil
	}
	defer res.Body.Close()
	var health struct {
		Halted bool   `json:"halted"`
		Cause  string `json:"cause"`
	}
	_ = json.NewDecoder(res.Body).Decode(&health)
	out.Running = true
	out.Halted, out.Cause = health.Halted, health.Cause

	if tasks, err := client.Get(loc.Board + "/v1/tasks"); err == nil {
		defer tasks.Body.Close()
		var body struct {
			Tasks []struct {
				Status string `json:"status"`
			} `json:"tasks"`
		}
		if json.NewDecoder(tasks.Body).Decode(&body) == nil {
			out.Cards = len(body.Tasks)
			for _, t := range body.Tasks {
				if t.Status == "needs-input" || t.Status == "needs-permission" {
					out.Waiting++
				}
			}
		}
	}
	return nil, out, nil
}

// ── restart ─────────────────────────────────────────────────────────────────

type RestartInput struct {
	// Why is recorded in the log the restarter writes, so a board that came
	// back has an account of who asked and what for. It is also what the other
	// agents are told, so it is worth writing as a sentence.
	Why string `json:"why,omitempty" jsonschema:"what this restart is for"`
	// Force skips waiting for other agents to reach a stopping point.
	//
	// Off by default and it should stay off. The whole reason this exists is
	// that a restart closes every terminal atrium owns, and closing one kills
	// the process in it. Forcing is for a board that is already broken.
	Force bool `json:"force,omitempty" jsonschema:"restart even if other agents are still working"`
	// WaitSeconds bounds how long to wait for them. Zero uses the default.
	WaitSeconds int `json:"wait_seconds,omitempty" jsonschema:"how long to wait for other agents, in seconds"`
}

type RestartOutput struct {
	Scheduled bool   `json:"scheduled"`
	InSeconds int    `json:"in_seconds"`
	DB        string `json:"db,omitempty"`
	Note      string `json:"note"`
	// Parked is who was asked to stop and did. Named rather than counted,
	// because the next question is always which ones.
	Parked []string `json:"parked,omitempty"`
	// Busy is who was still working when the wait ran out. Non-empty means
	// nothing was scheduled, unless force was set.
	Busy []busyCard `json:"busy,omitempty"`
}

func restartHandler(_ context.Context, _ *mcp.CallToolRequest, in RestartInput) (
	*mcp.CallToolResult, RestartOutput, error) {

	// The database is captured NOW, while the daemon is still up, because
	// `atrium stop` deletes the address file on its way out. Reading it
	// afterwards would find nothing and the new daemon would pick a database
	// from whatever environment the detached process happened to inherit,
	// which is the exact confusion `--db` exists to end.
	db := ""
	if loc, err := readLocation(); err == nil {
		db = strings.TrimSpace(loc.DB)
	}
	if db == "" {
		if prev, ok := lastDatabase(); ok {
			db = prev
		}
	}
	if db == "" {
		return nil, RestartOutput{}, fmt.Errorf(
			"cannot tell which database to reopen. start the daemon once with --db and try again")
	}

	// NOBODY ELSE GETS THE RUG PULLED.
	//
	// The restart closes every terminal atrium owns and takes the process in
	// each one with it. The caller signed up for that; nothing else did. So
	// anything supervised and working is told what is coming and given a
	// chance to stop, and the restart does not happen until they have.
	//
	// Skipped entirely when the board is not answering: there is nothing to
	// ask and nothing to interrupt.
	var parked []string
	if loc, err := readLocation(); err == nil && strings.TrimSpace(loc.Board) != "" {
		wait := parkWait
		if in.WaitSeconds > 0 {
			wait = time.Duration(in.WaitSeconds) * time.Second
		}
		busy, told, perr := parkAgents(loc.Board, in.Why, wait)
		parked = told
		if perr == nil && len(busy) > 0 && !in.Force {
			return nil, RestartOutput{
				Busy:   busy,
				Parked: told,
				Note: "NOT restarted. these sessions are still working and the restart would " +
					"close their terminals: " + describe(busy) + ". they have been asked to " +
					"stop. wait and try again, or pass force if you accept interrupting them.",
			}, nil
		}
	}

	exe, err := os.Executable()
	if err != nil {
		return nil, RestartOutput{}, fmt.Errorf("cannot find my own binary: %w", err)
	}

	args := []string{"control", "--restart-now", "--db", db,
		"--delay", restartDelay.String()}
	if err := spawnDetached(exe, args); err != nil {
		return nil, RestartOutput{}, fmt.Errorf("could not schedule the restart: %w", err)
	}

	note := "the daemon goes down in a few seconds and comes back on the same database. " +
		"if you are a supervised session you will be taken down with it. say nothing " +
		"further this turn."
	if len(parked) > 0 {
		note = "parked " + strings.Join(parked, ", ") + " first. " + note
	}
	return nil, RestartOutput{
		Scheduled: true,
		InSeconds: int(restartDelay.Seconds()),
		DB:        db,
		Parked:    parked,
		Note:      note,
	}, nil
}

// runRestart is the detached half: wait, stop, wait for the port, start.
//
// Everything here is logged to stderr, which goes nowhere useful, and that is
// accepted. The thing that reports whether this worked is `atrium_status`
// afterwards, because by definition whoever asked for it is gone.
func runRestart(db, delay string) error {
	wait := restartDelay
	if d, err := time.ParseDuration(delay); err == nil && d > 0 {
		wait = d
	}
	time.Sleep(wait)

	board := "http://localhost:7778"
	if loc, err := readLocation(); err == nil && strings.TrimSpace(loc.Board) != "" {
		board = loc.Board
	}
	// Best effort. A daemon that is already gone is not a failure: the point is
	// to end with one running, not to have stopped one.
	_ = stopDaemon(board, "")

	// Wait for it to actually be gone rather than sleeping a guessed amount.
	// The wind-down gives supervised runners ten seconds, and starting during
	// that produces a second daemon that cannot bind and exits.
	deadline := time.Now().Add(stopGrace)
	client := &http.Client{Timeout: time.Second}
	for time.Now().Before(deadline) {
		res, err := client.Get(board + "/v1/health")
		if err != nil {
			break
		}
		res.Body.Close()
		time.Sleep(500 * time.Millisecond)
	}

	exe, err := daemonExe()
	if err != nil {
		return err
	}
	// The daemon is DOWN right now, which is the only moment its binary can be
	// replaced. Anything staged beside it goes in here, the control server
	// included.
	swapAllStaged(exe)
	return spawnDetached(exe, []string{"daemon", "--db", db})
}

// daemonExe is which binary to start as the daemon.
//
// A sibling `atrium.exe` when there is one, and this process otherwise.
//
// The two are separated because they cannot be the same file. This process is
// the MCP server, held open by whatever claude session registered it, for as
// long as that session lives. A binary cannot be overwritten while it is
// running, so if the daemon and the control server were one file there would
// be no moment at which a rebuild could be installed: exactly the chicken and
// egg this command exists to break, one level down.
func daemonExe() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	sibling := filepath.Join(filepath.Dir(self), "atrium"+exeSuffix())
	if sibling != self {
		if _, err := os.Stat(sibling); err == nil {
			return sibling, nil
		}
	}
	return self, nil
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// swapStaged installs a newer binary left beside the daemon's.
//
// A rebuild cannot overwrite a running daemon, so it writes `atrium.next` and
// leaves it. This is the one instant that file can be moved into place: after
// the stop and before the start.
//
// A failure here is logged and stepped over rather than returned. The staged
// binary not landing means the daemon comes back on the old one, which is a
// disappointment. Not coming back at all is an outage.
// swapAllStaged installs whatever has been staged beside the running binaries.
//
// TWO BINARIES, and forgetting the second one is a trap worth naming. The
// daemon is `atrium`, and the MCP server that schedules this restart is
// `atrium-control`, split apart precisely because neither can overwrite a file
// the other is running. Swapping only the daemon means a change to the control
// server sits staged forever: the daemon comes back new, spawns claude, claude
// spawns the OLD control binary, and nothing about the tool has changed.
//
// The control binary is swapped by a copy of ITSELF, which is fine for the
// same reason the daemon swap is: Windows permits renaming an open file, and
// the running image keeps its handle to the file under its new name.
func swapAllStaged(daemonExe string) {
	if err := swapStaged(daemonExe); err != nil {
		log.Printf("[atrium] could not install the staged daemon: %v", err)
	}
	self, err := os.Executable()
	if err != nil {
		return
	}
	// Only when they are siblings. A control server installed somewhere else
	// entirely is not something this should be reaching into.
	if filepath.Dir(self) != filepath.Dir(daemonExe) {
		return
	}
	if err := swapStaged(self); err != nil {
		log.Printf("[atrium] could not install the staged control server: %v", err)
	}
}

// swapStaged installs `<name>.next` over `<name>`, keeping the outgoing one as
// `<name>.old`.
func swapStaged(exe string) error {
	dir := filepath.Dir(exe)
	base := strings.TrimSuffix(filepath.Base(exe), exeSuffix())
	staged := filepath.Join(dir, base+".next"+exeSuffix())
	if _, err := os.Stat(staged); err != nil {
		return nil
	}
	aside := filepath.Join(dir, base+".old"+exeSuffix())

	// THE OUTGOING BINARY IS MOVED ASIDE, NOT DELETED, and on Windows that
	// distinction is the whole reason this works.
	//
	// Windows refuses to delete a file that is open, and an executable is open
	// for as long as anything is running it. It permits a RENAME within the
	// same directory, which is how every self-updater on this platform does
	// it: the running image keeps its handle, the name is freed, and the new
	// file takes it.
	//
	// Without that, this had to be done by hand from a shell outside atrium,
	// with the daemon stopped, which is exactly the chore `restart_atrium`
	// exists to remove.
	_ = os.Remove(aside) // last time's, now that nothing is running it
	if err := os.Rename(exe, aside); err != nil && !os.IsNotExist(err) {
		return err
	}
	// A rename rather than a copy, so there is no instant where the daemon's
	// binary is half written.
	if err := os.Rename(staged, exe); err != nil {
		// Put it back. A failure here with the old one moved aside would leave
		// no binary at that name at all, which is an outage rather than a
		// disappointment.
		_ = os.Rename(aside, exe)
		return err
	}
	log.Printf("[atrium] installed the staged binary at %s", exe)
	return nil
}

// ── plumbing ────────────────────────────────────────────────────────────────

// readLocation reads the running daemon's recorded address.
func readLocation() (daemon.Location, error) {
	var loc daemon.Location
	path, err := daemon.LocationPath()
	if err != nil {
		return loc, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return loc, err
	}
	err = json.Unmarshal(raw, &loc)
	return loc, err
}

// lastDatabase reads which database the last daemon opened, for when none is
// running.
//
// A different file from the address, on purpose: the address is deleted on a
// clean wind-down and this is not, which is precisely the case a restart is in
// the middle of.
func lastDatabase() (string, bool) {
	path, err := daemon.LocationPath()
	if err != nil {
		return "", false
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(path), "lastdb.json"))
	if err != nil {
		return "", false
	}
	var prev struct {
		DB string `json:"db"`
	}
	if json.Unmarshal(raw, &prev) != nil {
		return "", false
	}
	prev.DB = strings.TrimSpace(prev.DB)
	return prev.DB, prev.DB != ""
}

// spawnDetached starts a process that outlives this one.
//
// The whole point: this process is about to be killed, or is a short-lived
// tool call, and the thing it starts has to survive that. Standard streams are
// left nil so the child holds no handle on a console that is going away.
func spawnDetached(exe string, args []string) error {
	cmd := exec.Command(exe, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	// Released rather than waited on. Waiting would tie the child's lifetime
	// to this process, which is the opposite of what detached means.
	return cmd.Process.Release()
}
