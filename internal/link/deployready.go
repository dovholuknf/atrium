package link

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

	"github.com/dovholuknf/atrium/internal/deployready"
	"github.com/dovholuknf/atrium/internal/edge"
	"github.com/dovholuknf/atrium/internal/gitsync"
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

	// deployReadyEvent is the stream event a board re-fetches on. A delta, like `deps` and `audit`.
	deployReadyEvent = "deploy-ready"
	// deployReadyFresh is how long one answer is reused, so a burst of boards costs one pass over git.
	deployReadyFresh = 10 * time.Second
	// deployReadyBound is how long one pass may spend on git and on asking the binary its version.
	deployReadyBound = 60 * time.Second
	deployVersionRun = 10 * time.Second
)

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

type deployReadyState struct {
	mu sync.Mutex

	// Seams for a test. In production both are the real thing.
	installed func(ctx context.Context, p *Proxy) (string, error)
	spawn     func(script, tip string) (*deployProc, error)

	checker *deployready.Checker
	last    deployready.Report
	lastAt  time.Time
	haveRep bool
	sig     string
	run     deployRun

	// version is the installed binary's answer, kept while the file is the same file.
	vPath string
	vMod  time.Time
	vSize int64
	vSHA  string
}

// deployProc is a started script. Wait blocks until it exits and returns the exit code.
type deployProc struct {
	Pid  int
	Wait func() (int, error)
}

// SetDeployReady turns the state on. A hub that never calls it answers /_hub/deploy-ready with 404.
func (p *Proxy) SetDeployReady() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ready = &deployReadyState{installed: installedCommit, spawn: spawnDeployScript, run: deployRun{State: "idle"}}
}

func (p *Proxy) deployReady() *deployReadyState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ready
}

func (p *Proxy) hubSetting(name string) string {
	p.mu.Lock()
	st := p.capStore
	p.mu.Unlock()
	if st == nil {
		return ""
	}
	v, _ := st.HubSetting(name)
	return strings.TrimSpace(v)
}

// installedCommit is the commit of the installed binary, asked of the file and never of the running hub: the file
// is what the next restart starts, and it can be newer than the hub when only the room was deployed.
func installedCommit(ctx context.Context, p *Proxy) (string, error) {
	st := p.deployReady()
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
	st.mu.Lock()
	if st.vPath == bin && st.vMod.Equal(fi.ModTime()) && st.vSize == fi.Size() && st.vSHA != "" {
		sha := st.vSHA
		st.mu.Unlock()
		return sha, nil
	}
	st.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, deployVersionRun)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "version")
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("the installed binary did not answer `version`: %v", err)
	}
	sha := parseVersionCommit(string(out))
	if sha == "" {
		return "", errors.New("the installed binary reports no commit, so it cannot be compared with the branch")
	}
	st.mu.Lock()
	st.vPath, st.vMod, st.vSize, st.vSHA = bin, fi.ModTime(), fi.Size(), sha
	st.mu.Unlock()
	return sha, nil
}

var versionCommitRe = regexp.MustCompile(`(?m)^commit\s+([0-9a-f]{7,40})\b`)

func parseVersionCommit(out string) string {
	if m := versionCommitRe.FindStringSubmatch(out); m != nil {
		return m[1]
	}
	return ""
}

// deployReadyView is what GET answers: the report, flat, and where a deploy stands.
type deployReadyView struct {
	deployready.Report
	Deploy deployRun `json:"deploy"`
	// Script is what a click would run, so the board can say so. Empty when there is no way to run one.
	Script string `json:"script,omitempty"`
}

// readyReport is the current answer. `fresh` skips the reuse window, for the pass a click makes.
func (p *Proxy) readyReport(ctx context.Context, fresh bool) deployready.Report {
	st := p.deployReady()
	st.mu.Lock()
	defer st.mu.Unlock()
	if !fresh && st.haveRep && time.Since(st.lastAt) < deployReadyFresh {
		return st.last
	}
	ctx, cancel := context.WithTimeout(ctx, deployReadyBound)
	defer cancel()
	rep := p.computeReady(ctx, st)
	st.last, st.lastAt, st.haveRep = rep, time.Now(), true
	return rep
}

func (p *Proxy) computeReady(ctx context.Context, st *deployReadyState) deployready.Report {
	unknown := func(format string, a ...any) deployready.Report {
		msg := fmt.Sprintf(format, a...)
		return deployready.Report{State: deployready.StateUnknown, Error: msg, CheckedAt: time.Now().UTC(),
			Line: "deploy readiness unknown: " + msg}
	}
	repo, err := p.repoFor("")
	if err != nil {
		return unknown("%v", err)
	}
	installed, err := st.installed(ctx, p)
	if err != nil {
		rep := unknown("%v", err)
		rep.Branch = repo.Branch
		return rep
	}
	c := p.checkerFor(st, repo)
	return c.Check(ctx, installed)
}

// checkerFor keeps one Checker per repository so its per-commit cache survives from one pass to the next.
func (p *Proxy) checkerFor(st *deployReadyState, repo gitsync.Repo) *deployready.Checker {
	if st.checker != nil && st.checker.Dir == repo.Checkout && st.checker.Branch == repo.Branch {
		return st.checker
	}
	st.checker = &deployready.Checker{Git: runnerOf(p.git()), Dir: repo.Checkout, Branch: repo.Branch}
	return st.checker
}

// scriptFor is the script a click runs, or an explanation of why there is none.
func (p *Proxy) scriptFor() (string, error) {
	if s := p.hubSetting(SettingDeployScript); s != "" {
		if _, err := os.Stat(s); err != nil {
			return "", fmt.Errorf("the %s setting names %s, which cannot be read", SettingDeployScript, s)
		}
		return s, nil
	}
	repo, err := p.repoFor("")
	if err != nil {
		return "", err
	}
	s := filepath.Join(repo.Checkout, "scripts", "live", "deploy-ready.ps1")
	if _, err := os.Stat(s); err != nil {
		return "", fmt.Errorf("there is no %s, and the %s setting is empty", s, SettingDeployScript)
	}
	return s, nil
}

// deployReadyTick re-reads the answer once a minute and tells watching boards when it moved. It deploys nothing.
// Skipped while nobody is watching, because the next GET pays for the same pass.
func (p *Proxy) deployReadyTick(ctx context.Context) {
	st := p.deployReady()
	if st == nil || p.feeds.watchers() == 0 {
		return
	}
	rep := p.readyReport(ctx, true)
	st.mu.Lock()
	changed := st.sig != rep.Signature()
	st.sig = rep.Signature()
	st.mu.Unlock()
	if changed {
		p.deployReadyChanged()
	}
}

func (p *Proxy) deployReadyChanged() {
	payload, err := json.Marshal(map[string]string{"what": "changed"})
	if err != nil {
		return
	}
	p.feeds.broadcast(Event{Kind: deployReadyEvent, Data: payload})
}

// serveDeployReady answers /_hub/deploy-ready and /_hub/deploy-ready/deploy.
func (p *Proxy) serveDeployReady(w http.ResponseWriter, r *http.Request, sub string) {
	st := p.deployReady()
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
		_ = json.NewEncoder(w).Encode(p.deployReadyView(r.Context(), r.URL.Query().Get("fresh") == "1"))
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

func (p *Proxy) deployReadyView(ctx context.Context, fresh bool) deployReadyView {
	rep := p.readyReport(ctx, fresh)
	st := p.deployReady()
	st.mu.Lock()
	v := deployReadyView{Report: rep, Deploy: st.run}
	st.mu.Unlock()
	if s, err := p.scriptFor(); err == nil {
		v.Script = s
	}
	return v
}

func (p *Proxy) startDeploy(w http.ResponseWriter, r *http.Request, st *deployReadyState,
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
	st.mu.Lock()
	if st.run.State == "running" {
		run := st.run
		st.mu.Unlock()
		fail(http.StatusConflict, "a deploy is already running", map[string]any{"deploy": run})
		return
	}
	st.mu.Unlock()

	rep := p.readyReport(r.Context(), true)
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

	st.mu.Lock()
	if st.run.State == "running" { // two clicks that both passed the check above
		run := st.run
		st.mu.Unlock()
		fail(http.StatusConflict, "a deploy is already running", map[string]any{"deploy": run})
		return
	}
	proc, err := st.spawn(script, tip)
	if err != nil {
		st.run = deployRun{State: "finished", Tip: tip, Started: time.Now().UTC(), Ended: time.Now().UTC(),
			Error: err.Error()}
		run := st.run
		st.mu.Unlock()
		fail(http.StatusInternalServerError, "could not start the deploy: "+err.Error(), map[string]any{"deploy": run})
		return
	}
	st.run = deployRun{State: "running", Tip: tip, Started: time.Now().UTC()}
	run := st.run
	st.mu.Unlock()

	p.RecordAudit("", "deploy-ready-started", fmt.Sprintf("tip %s, %d commits, script %s", tip[:8], rep.Commits, script))
	p.deployReadyChanged()
	go func() {
		code, err := proc.Wait()
		st.mu.Lock()
		st.run.State, st.run.Ended = "finished", time.Now().UTC()
		st.run.Exit = &code
		if err != nil {
			st.run.Error = err.Error()
		}
		st.mu.Unlock()
		p.deployReadyChanged()
	}()
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"started": true, "pid": proc.Pid, "deploy": run, "script": script})
}

// deployEnv drops every ATRIUM_* variable from env. The hub runs inside a session's environment more often than not, and
// ATRIUM_NEW_BUILD or ATRIUM_LOCATION there would point the deploy at a binary or a hub nobody checked. The script reads
// what it needs (ATRIUM_HOSTS) from the User environment itself.
func deployEnv(env []string) []string {
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
func spawnDeployScript(script, tip string) (*deployProc, error) {
	shell, err := exec.LookPath("pwsh")
	if err != nil {
		if shell, err = exec.LookPath("powershell"); err != nil {
			return nil, errors.New("no pwsh or powershell on this machine's PATH")
		}
	}
	cmd := exec.Command(shell, "-NoProfile", "-NonInteractive", "-File", script, "-Tip", tip)
	cmd.Env = deployEnv(gitsync.CleanEnv())
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &deployProc{Pid: cmd.Process.Pid, Wait: func() (int, error) {
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
