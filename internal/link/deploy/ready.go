package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/base/edge"
	"github.com/dovholuknf/atrium/internal/base/nowindow"
	"github.com/dovholuknf/atrium/internal/deployready"
	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/link/wire"
)

// The hub's "deploy ready" state, and the one-click deploy behind it. See
// docs/rnd/factory-shape.md "(a) The hub deploys itself", step one, and
// internal/deployready for what ready means.
//
//	GET  /_hub/deploy-ready          ready, or the commits that block it, and where a deploy stands
//	POST /_hub/deploy-ready/deploy   {"tip": "<sha>"}. Loopback only. Starts the deploy and answers at once
//
// THERE IS NO TIMER THAT DEPLOYS. A person presses the button, or nothing happens. The only clock here is the
// hub's existing minute tick, and all it does is re-read git and tell a watching board when the answer moved.
//
// THE CLICK NAMES THE TIP IT WAS SHOWN. A board that rendered "ready: abc1234" and a branch that has moved since
// must not deploy def5678 on the strength of the old line, so the POST carries the tip and a mismatch is a 409.
//
// THE DEPLOY IS A SCRIPT, started detached. It stops this very process, so it cannot be a goroutine, and it is
// scripts/live/deploy-ready.ps1 so it can be run by hand with -WhatIf. The hub passes it a validated tip and
// nothing else. Whatever the script does about the restart gate, the build, the swap and the revert is the
// deploy that exists today.

const (
	// SettingDeployBinary names the installed binary, whose commit is "what is deployed". Unset means this hub's own
	// executable, which is the installed binary on every hub that has not been moved.
	SettingDeployBinary = "deploy_binary"
	// SettingDeployScript names the script a click runs. Unset means scripts/live/deploy-ready.ps1 in the
	// integration checkout.
	SettingDeployScript = "deploy_script"

	// ReadyEvent is the stream event a board re-fetches on. A delta, like `deps` and `audit`.
	ReadyEvent = "deploy-ready"
	// deployReadyFresh is how long one answer is reused, so a burst of boards costs one pass over git.
	deployReadyFresh = 10 * time.Second
	deployVersionRun = 10 * time.Second
)

// deployReadyBound is how long one pass may spend on git and on asking the binary its version. A board is waiting on
// it, and git is killed at the bound. A field on the state (`bound`) so a test need not wait five seconds.
const deployReadyBound = 5 * time.Second

var fullSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

// deployRun is where the deploy this hub started stands. In memory: the deploy ends this process, and the log the
// script writes is the record that survives.
type deployRun struct {
	State   string    `json:"state"` // idle, running, finished
	Tip     string    `json:"tip,omitempty"`
	Started time.Time `json:"started,omitempty"`
	Ended   time.Time `json:"ended,omitempty"`
	Exit    *int      `json:"exit,omitempty"`
	Error   string    `json:"error,omitempty"`
}

type ReadyState struct {
	Mu sync.Mutex

	// Seams for a test. In production both are the real thing.
	Installed func(ctx context.Context, p *Service) (string, error)
	// Bound is how long one pass may run, deployReadyBound unless a test shortens it.
	Bound time.Duration
	Spawn func(script, tip string) (*DeployProc, error)
	// clearing is the new contexts under way. Nil means ask the attached rooms.
	Clearing func(ctx context.Context) []NewContextRun

	// running is the commit this hub was built from, for the deploy queue. See deployqueue.go.
	running string

	Checker *deployready.Checker
	last    deployready.Report
	lastAt  time.Time
	HaveRep bool
	// inflight is true while one pass reads git. The lock is not held for it.
	Inflight bool
	// kicking is true while a background pass started by a GET is on its way.
	kicking bool
	sig     string
	run     deployRun

	// version is the installed binary's answer, kept while the file is the same file.
	vPath string
	vMod  time.Time
	vSize int64
	vSHA  string
}

// DeployProc is a started script. Wait blocks until it exits and returns the exit code.
type DeployProc struct {
	Pid  int
	Wait func() (int, error)
}

// Settings is the part of the hub's store the deploy reads and writes.
type Settings interface {
	HubSetting(name string) (string, error)
	SetHubSetting(name, value string) error
}

// Calls is every call out of the deploy into the rest of the hub.
type Calls struct {
	// Settings is the hub's own settings. Nil on a hub with no store.
	Settings    func() Settings
	RepoFor     func(name string) (gitsync.Repo, error)
	Runner      func() *gitsync.Runner
	Watchers    func() int
	Broadcast   func(ev wire.Event)
	Rooms       func() []wire.Attached
	Attached    func() []wire.Attached
	RoomGet     func(ctx context.Context, room, path string, out any) bool
	RecordAudit func(room, kind, detail string)
}

// Service is the deploy readiness, the deploy queue, the new contexts under way and the deploy owner. See mcp.go for
// the tool.
type Service struct {
	// mu guards ready, which SetDeployReady turns on after the Service is built.
	mu    sync.Mutex
	ready *ReadyState
	Calls
}

// New is a Service with deploy-ready off.
func New(c Calls) *Service { return &Service{Calls: c} }

// SetDeployReady turns the state on. A hub that never calls it answers /_hub/deploy-ready with 404.
func (p *Service) SetDeployReady() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ready = &ReadyState{Bound: deployReadyBound, Installed: installedCommit, Spawn: spawnDeployScript, run: deployRun{State: "idle"}}
}

func (p *Service) DeployReady() *ReadyState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ready
}

func (p *Service) hubSetting(name string) string {
	st := p.Settings()
	if st == nil {
		return ""
	}
	v, _ := st.HubSetting(name)
	return strings.TrimSpace(v)
}

// installedCommit is the commit of the installed binary, asked of the file and never of the running hub: the file
// is what the next restart starts, and it can be newer than the hub when only the room was deployed.
func installedCommit(ctx context.Context, p *Service) (string, error) {
	st := p.DeployReady()
	bin := p.hubSetting(SettingDeployBinary)
	if bin == "" {
		exe, err := os.Executable()
		if err != nil {
			return "", fmt.Errorf("could not find the installed binary: %v", err)
		}
		bin = exe
	}
	fi, err := os.Stat(bin)
	if err != nil {
		return "", fmt.Errorf("could not read the installed binary: %v", err)
	}
	st.Mu.Lock()
	if st.vPath == bin && st.vMod.Equal(fi.ModTime()) && st.vSize == fi.Size() && st.vSHA != "" {
		sha := st.vSHA
		st.Mu.Unlock()
		return sha, nil
	}
	st.Mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, deployVersionRun)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "version")
	nowindow.Hide(cmd)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("the installed binary did not answer `version`: %v", err)
	}
	sha := ParseVersionCommit(string(out))
	if sha == "" {
		return "", errors.New("the installed binary reports no commit, so it cannot be compared with the branch")
	}
	st.Mu.Lock()
	st.vPath, st.vMod, st.vSize, st.vSHA = bin, fi.ModTime(), fi.Size(), sha
	st.Mu.Unlock()
	return sha, nil
}

var versionCommitRe = regexp.MustCompile(`(?m)^commit\s+([0-9a-f]{7,40})\b`)

func ParseVersionCommit(out string) string {
	if m := versionCommitRe.FindStringSubmatch(out); m != nil {
		return m[1]
	}
	return ""
}

// ReadyView is what GET answers: the report, flat, and where a deploy stands.
type ReadyView struct {
	deployready.Report
	Deploy deployRun `json:"deploy"`
	// Building is true while a pass is reading git, or none has finished yet. Computed and AgeSeconds say how old
	// the answer is, and are absent until the first pass has finished. See readyCached.
	Building   bool   `json:"building"`
	Computed   string `json:"computed,omitempty"`
	AgeSeconds int    `json:"age_seconds"`
	// Script is what a click would run, so the board can say so. Empty when there is no way to run one.
	Script string `json:"script,omitempty"`
}

// readyReport is the current answer. `fresh` skips the reuse window, for the pass a click makes.
//
// ONE PASS IN FLIGHT, AND NO LOCK HELD ACROSS IT. The caller that starts a pass waits for it, up to the bound. Any
// caller that arrives meanwhile gets the last answer marked stale, or unknown if there is none, at once. A pass that
// hits the bound answers unknown and keeps what git gave it, so the next pass starts further along.
func (p *Service) readyReport(ctx context.Context, fresh bool) deployready.Report {
	st := p.DeployReady()
	st.Mu.Lock()
	if !fresh && st.HaveRep && time.Since(st.lastAt) < deployReadyFresh {
		rep := st.last
		st.Mu.Unlock()
		return rep
	}
	if st.Inflight {
		rep, have := st.last, st.HaveRep
		st.Mu.Unlock()
		if !have {
			return readyUnknown("still reading git for the first time, ask again in a moment")
		}
		rep.Stale = true
		rep.Ready = false
		rep.Notes = append(append([]string{}, rep.Notes...), "a newer read is still running, so this is the last answer")
		return rep
	}
	st.Inflight = true
	st.Mu.Unlock()

	bctx, cancel := context.WithTimeout(ctx, st.Bound)
	rep := p.computeReady(bctx, st)
	timedOut := bctx.Err() != nil
	cancel()
	if timedOut {
		rep = readyUnknown("timed out after %s reading git, the next ask continues from what was read", st.Bound)
	}

	st.Mu.Lock()
	st.Inflight = false
	st.last, st.HaveRep = rep, true
	st.lastAt = time.Now()
	if timedOut {
		st.lastAt = time.Time{} // the next ask reads again at once rather than reusing a give-up
	}
	// Whoever ran the pass tells the watching boards, so a pill that sat on unknown does not wait for the minute tick.
	changed := st.sig != rep.Signature()
	st.sig = rep.Signature()
	st.Mu.Unlock()
	if changed {
		p.deployReadyChanged()
	}
	return rep
}

func readyUnknown(format string, a ...any) deployready.Report {
	msg := fmt.Sprintf(format, a...)
	return deployready.Report{State: deployready.StateUnknown, Error: msg, CheckedAt: time.Now().UTC(),
		Line: "deploy readiness unknown: " + msg}
}

func (p *Service) computeReady(ctx context.Context, st *ReadyState) deployready.Report {
	unknown := readyUnknown
	repo, err := p.RepoFor("")
	if err != nil {
		return unknown("%v", err)
	}
	installed, err := st.Installed(ctx, p)
	if err != nil {
		rep := unknown("%v", err)
		rep.Branch = repo.Branch
		return rep
	}
	st.Mu.Lock()
	c := p.checkerFor(st, repo)
	st.Mu.Unlock()
	return c.Check(ctx, installed)
}

// checkerFor keeps one Checker per repository so its per-commit cache survives from one pass to the next.
func (p *Service) checkerFor(st *ReadyState, repo gitsync.Repo) *deployready.Checker {
	if st.Checker != nil && st.Checker.Dir == repo.Checkout && st.Checker.Branch == repo.Branch {
		return st.Checker
	}
	st.Checker = &deployready.Checker{Git: p.Runner(), Dir: repo.Checkout, Branch: repo.Branch}
	return st.Checker
}

// scriptFor is the script a click runs, or an explanation of why there is none.
func (p *Service) scriptFor() (string, error) {
	if s := p.hubSetting(SettingDeployScript); s != "" {
		if _, err := os.Stat(s); err != nil {
			return "", fmt.Errorf("the %s setting names %s, which cannot be read", SettingDeployScript, s)
		}
		return s, nil
	}
	repo, err := p.RepoFor("")
	if err != nil {
		return "", err
	}
	s := filepath.Join(repo.Checkout, "scripts", "live", "deploy-ready.ps1")
	if _, err := os.Stat(s); err != nil {
		return "", fmt.Errorf("there is no %s, and the %s setting is empty", s, SettingDeployScript)
	}
	return s, nil
}

// DeployReadyTick re-reads the answer once a minute and tells watching boards when it moved. It deploys nothing.
// Skipped while nobody is watching, because the next GET pays for the same pass.
func (p *Service) DeployReadyTick(ctx context.Context) {
	st := p.DeployReady()
	if st == nil || p.Watchers() == 0 {
		return
	}
	p.readyReport(ctx, true) // announces the answer itself when it moved
}

func (p *Service) deployReadyChanged() {
	payload, err := json.Marshal(map[string]string{"what": "changed"})
	if err != nil {
		return
	}
	p.Broadcast(wire.Event{Kind: ReadyEvent, Data: payload})
}

// ServeDeployReady answers /_hub/deploy-ready and /_hub/deploy-ready/deploy.
func (p *Service) ServeDeployReady(w http.ResponseWriter, r *http.Request, sub string) {
	st := p.DeployReady()
	if st == nil {
		http.NotFound(w, r)
		return
	}
	fail := func(code int, msg string, extra map[string]any) {
		w.WriteHeader(code)
		body := map[string]any{"error": msg}
		for k, v := range extra {
			body[k] = v
		}
		_ = json.NewEncoder(w).Encode(body)
	}
	switch sub {
	case "deploy-ready":
		if r.Method != http.MethodGet {
			fail(http.StatusMethodNotAllowed, "that has to be a GET", nil)
			return
		}
		_ = json.NewEncoder(w).Encode(p.deployReadyView(r.URL.Query().Get("fresh") == "1"))
	case "deploy-ready/deploy":
		if r.Method != http.MethodPost {
			fail(http.StatusMethodNotAllowed, "that has to be a POST", nil)
			return
		}
		p.startDeploy(w, r, st, fail)
	default:
		http.NotFound(w, r)
	}
}

// ReadyView answers a GET from the last pass and runs no git: see readyCached. `fresh` asks for a new pass to
// start, and does not wait for it.
func (p *Service) deployReadyView(fresh bool) ReadyView {
	rep, at, building := p.readyCached(fresh)
	st := p.DeployReady()
	st.Mu.Lock()
	v := ReadyView{Report: rep, Deploy: st.run, Building: building}
	st.Mu.Unlock()
	if !at.IsZero() {
		v.Computed = at.UTC().Format(time.RFC3339)
		v.AgeSeconds = int(time.Since(at).Seconds())
	}
	if s, err := p.scriptFor(); err == nil {
		v.Script = s
	}
	return v
}

// readyCached is the last answer at once, with when it was read and whether a newer pass is under way. THE GET NEVER
// WAITS ON GIT: a pass reads the repository and asks the installed binary its version, which took seconds. Before the
// first pass has finished the answer is unknown with `building` set. A pass is started in the background when the
// answer is older than deployReadyFresh, when there is none, or when `fresh` is set.
func (p *Service) readyCached(fresh bool) (deployready.Report, time.Time, bool) {
	st := p.DeployReady()
	st.Mu.Lock()
	rep, have, at := st.last, st.HaveRep, st.lastAt
	if have {
		rep.Notes = append([]string(nil), rep.Notes...)
	}
	building := st.Inflight || st.kicking
	st.Mu.Unlock()
	if have && at.IsZero() {
		// The last pass gave up at its bound. What it kept is not an answer to act on, and it is read again once it is
		// as old as an answer may be, not on every ask.
		at = rep.CheckedAt
	}
	if !have || fresh || time.Since(at) >= deployReadyFresh {
		building = p.kickReady() || building
	}
	if !have {
		return readyUnknown("still reading git for the first time, ask again in a moment"), time.Time{}, true
	}
	return rep, at, building
}

// kickReady starts a pass in the background unless one is running. It says whether one is running now.
func (p *Service) kickReady() bool {
	st := p.DeployReady()
	st.Mu.Lock()
	if st.Inflight || st.kicking {
		st.Mu.Unlock()
		return true
	}
	st.kicking = true
	st.Mu.Unlock()
	go func() {
		p.readyReport(context.Background(), true)
		st.Mu.Lock()
		st.kicking = false
		st.Mu.Unlock()
	}()
	return true
}

func (p *Service) startDeploy(w http.ResponseWriter, r *http.Request, st *ReadyState,
	fail func(int, string, map[string]any)) {

	// LOOPBACK ONLY, like git sync and the launch caps. This swaps the binary every hook on the machine runs, and an
	// overlay is not an auth layer.
	if !edge.LocalOperator(r) {
		fail(http.StatusForbidden, "a deploy is started only from the machine the hub runs on"+edge.ProxyNote(r), nil)
		return
	}
	var in struct {
		Tip string `json:"tip"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&in); err != nil {
		fail(http.StatusBadRequest, "could not read that: "+err.Error(), nil)
		return
	}
	tip := strings.ToLower(strings.TrimSpace(in.Tip))
	if !fullSHA.MatchString(tip) {
		fail(http.StatusBadRequest, "say the full tip the board showed", nil)
		return
	}
	st.Mu.Lock()
	if st.run.State == "running" {
		run := st.run
		st.Mu.Unlock()
		fail(http.StatusConflict, "a deploy is already running", map[string]any{"deploy": run})
		return
	}
	st.Mu.Unlock()

	rep := p.readyReport(r.Context(), true)
	if rep.Stale {
		fail(http.StatusConflict, "a read of git is still running, try again in a moment", map[string]any{"report": rep})
		return
	}
	if !rep.Ready {
		fail(http.StatusConflict, "not ready to deploy: "+rep.Line, map[string]any{"report": rep})
		return
	}
	if rep.Tip != tip {
		fail(http.StatusConflict, fmt.Sprintf("%s has moved since that was shown: it is %s now, not %s",
			rep.Branch, rep.Tip[:8], tip[:8]), map[string]any{"report": rep})
		return
	}
	script, err := p.scriptFor()
	if err != nil {
		fail(http.StatusConflict, err.Error(), nil)
		return
	}
	// A DEPLOY RESTARTS THE ROOM, which cuts a clear off between its steps. The click is refused and can be
	// pressed again once the card has its wake.
	clearing := st.Clearing
	if clearing == nil {
		clearing = p.NewContextsUnderWay
	}
	if runs := clearing(r.Context()); len(runs) > 0 {
		fail(http.StatusConflict, "a new context is under way on "+newContextNames(runs)+", so the deploy waits for it",
			map[string]any{"new_contexts": runs})
		return
	}

	st.Mu.Lock()
	if st.run.State == "running" { // two clicks that both passed the check above
		run := st.run
		st.Mu.Unlock()
		fail(http.StatusConflict, "a deploy is already running", map[string]any{"deploy": run})
		return
	}
	proc, err := st.Spawn(script, tip)
	if err != nil {
		st.run = deployRun{State: "finished", Tip: tip, Started: time.Now().UTC(), Ended: time.Now().UTC(),
			Error: err.Error()}
		run := st.run
		st.Mu.Unlock()
		fail(http.StatusInternalServerError, "could not start the deploy: "+err.Error(), map[string]any{"deploy": run})
		return
	}
	st.run = deployRun{State: "running", Tip: tip, Started: time.Now().UTC()}
	run := st.run
	st.Mu.Unlock()

	p.RecordAudit("", "deploy-ready-started", fmt.Sprintf("tip %s, %d commits, script %s", tip[:8], rep.Commits, script))
	p.deployReadyChanged()
	go func() {
		code, err := proc.Wait()
		st.Mu.Lock()
		st.run.State, st.run.Ended = "finished", time.Now().UTC()
		st.run.Exit = &code
		if err != nil {
			st.run.Error = err.Error()
		}
		st.Mu.Unlock()
		p.deployReadyChanged()
	}()
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"started": true, "pid": proc.Pid, "deploy": run, "script": script})
}

// DeployEnv drops every ATRIUM_* variable from env. The hub runs inside a session's environment more often than not, and
// ATRIUM_NEW_BUILD or ATRIUM_LOCATION there would point the deploy at a binary or a hub nobody checked. The script reads
// what it needs (ATRIUM_HOSTS) from the User environment itself.
func DeployEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if len(kv) >= 7 && strings.EqualFold(kv[:7], "ATRIUM_") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// spawnDeployScript starts the script detached from this process, which the script is about to stop.
func spawnDeployScript(script, tip string) (*DeployProc, error) {
	shell, err := exec.LookPath("pwsh")
	if err != nil {
		if shell, err = exec.LookPath("powershell"); err != nil {
			return nil, errors.New("no pwsh or powershell on this machine's PATH")
		}
	}
	cmd := exec.Command(shell, "-NoProfile", "-NonInteractive", "-File", script, "-Tip", tip)
	cmd.Env = DeployEnv(gitsync.CleanEnv())
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &DeployProc{Pid: cmd.Process.Pid, Wait: func() (int, error) {
		err := cmd.Wait()
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode(), nil
		}
		if err != nil {
			return -1, err
		}
		return 0, nil
	}}, nil
}
