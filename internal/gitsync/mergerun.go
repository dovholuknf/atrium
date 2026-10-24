package gitsync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// The room's half of the merge queue: test one candidate the hub merged, and say how it went. See
// docs/rnd/merge-queue.md.
//
//	POST /v1/git/merge/run     {n, repo, candidate, base}, sent by the hub over the link. Answers 202 at once.
//	GET  /v1/git/merge/status  ?n=, the last verdict of a run, from memory, for a hub that restarted mid-run
//	POST /v1/git/merge/prune   {repo, branch, sha}, delete a landed local branch when it is safe to
//
// THE WORKTREE IS NOT A CARD. One per repository under Dir, detached at the candidate and cleaned before each run,
// with ignored files (node_modules, build caches) kept. No session runs in it and nothing else writes it.
//
// ONE RUN PER REPOSITORY. A newer run of the same repository ends the older one, since the hub only asks for a
// second when it has given up on the first.

// MergeJob is one run the hub asks for.
type MergeJob struct {
	N         int64  `json:"n"`
	Repo      string `json:"repo"`
	Candidate string `json:"candidate"`
	Base      string `json:"base"`
	// Token is the run's capability, handed back with the verdict so only this run's verdict is heard.
	Token string `json:"token"`
}

// The verdicts of a run.
const (
	VerdictGreen = "green"
	VerdictRed   = "red"
	VerdictError = "error"
	// VerdictCannot is the room failing to run the tests: no clone, no bash, the hub's git out of reach.
	VerdictCannot = "cannot"
	// VerdictRunning is no verdict: the status of a run still going.
	VerdictRunning = "running"
)

// MergeReport is what a run came to, posted to the hub's /_hub/merge/report.
type MergeReport struct {
	N         int64    `json:"n"`
	Repo      string   `json:"repo"`
	Candidate string   `json:"candidate"`
	Verdict   string   `json:"verdict"`
	Failing   []string `json:"failing,omitempty"`
	Output    string   `json:"output,omitempty"`
	Reason    string   `json:"reason,omitempty"`
	// Note is how a green came about when the script alone did not say so: a rerun, or only known failures.
	Note  string `json:"note,omitempty"`
	Token string `json:"token,omitempty"`
}

// MergeScript is the test the queue runs, from the worktree's root.
const MergeScript = "scripts/test-touched.sh"

// MergeTestBound is how long one run may take before it is killed and called an error.
const MergeTestBound = 45 * time.Minute

const mergeOutputKeep = 1 << 20

// MergeRunner runs the tests. Root is where the room's clones live, Dir where the merge worktrees go, Hub the link's
// transport to the hub's store, Report how a verdict reaches the hub.
type MergeRunner struct {
	Root   func() string
	Dir    string
	Hub    func() (http.RoundTripper, error)
	Report func(ctx context.Context, r MergeReport) error
	Runner *Runner
	// Bash runs the script. Empty finds it: beside git on Windows, else bash on the PATH.
	Bash string
	// Bound replaces MergeTestBound, for a test.
	Bound time.Duration

	mu      sync.Mutex
	running map[string]*mergeRun
	last    map[int64]MergeReport
	runs    atomic.Int64
}

type mergeRun struct {
	job    MergeJob
	cancel context.CancelFunc
	done   chan struct{}
}

// ErrReportRefused is a verdict the hub answered with a 4xx. Sending it again changes nothing.
var ErrReportRefused = errors.New("the hub refused the verdict")

func (m *MergeRunner) runner() *Runner {
	if m.Runner != nil {
		return m.Runner
	}
	return Default
}

func (m *MergeRunner) mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/git/merge/run", m.serveRun)
	mux.HandleFunc("GET /v1/git/merge/status", m.serveStatus)
	mux.HandleFunc("POST /v1/git/merge/prune", m.servePrune)
}

func (m *MergeRunner) serveRun(w http.ResponseWriter, r *http.Request) {
	var j MergeJob
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&j); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read that: " + err.Error()})
		return
	}
	if _, err := ParseName(j.Repo); err != nil || j.N <= 0 || !ValidSHA(j.Candidate) || !ValidSHA(j.Base) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "a run is {n, repo, candidate, base}, with full shas"})
		return
	}
	m.mu.Lock()
	if m.running == nil {
		m.running = map[string]*mergeRun{}
	}
	key := strings.ToLower(j.Repo)
	old := m.running[key]
	if old != nil {
		old.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	run := &mergeRun{job: j, cancel: cancel, done: make(chan struct{})}
	m.running[key] = run
	m.mu.Unlock()
	go func() {
		defer close(run.done)
		defer cancel()
		// The old run's worktree is this run's, so its process tree is gone before this one starts.
		if old != nil {
			<-old.done
		}
		rep := m.Run(ctx, j)
		m.mu.Lock()
		if m.running[key] != run {
			m.mu.Unlock()
			return
		}
		delete(m.running, key)
		if m.last == nil {
			m.last = map[int64]MergeReport{}
		}
		m.last[j.N] = rep
		if len(m.last) > 50 {
			low := j.N
			for n := range m.last {
				if n < low {
					low = n
				}
			}
			delete(m.last, low)
		}
		m.mu.Unlock()
		m.report(rep)
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "n": j.N})
}

func (m *MergeRunner) serveStatus(w http.ResponseWriter, r *http.Request) {
	var n int64
	if _, err := fmt.Sscan(r.URL.Query().Get("n"), &n); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "say which run with ?n="})
		return
	}
	m.mu.Lock()
	rep, ok := m.last[n]
	for _, run := range m.running {
		if run.job.N == n {
			j := run.job
			rep, ok = MergeReport{N: j.N, Repo: j.Repo, Candidate: j.Candidate, Verdict: VerdictRunning, Token: j.Token}, true
		}
	}
	m.mu.Unlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "this room has no verdict for that run"})
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

// report posts a verdict, trying for ten minutes, since the hub may be restarting. The verdict stays in memory for
// the status route whether or not it got through.
func (m *MergeRunner) report(rep MergeReport) {
	if m.Report == nil {
		return
	}
	wait := 2 * time.Second
	for deadline := time.Now().Add(10 * time.Minute); ; {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		err := m.Report(ctx, rep)
		cancel()
		if err == nil || errors.Is(err, ErrReportRefused) || time.Now().After(deadline) {
			if err != nil {
				log.Printf("[git] merge %d: the hub did not take the verdict: %v", rep.N, err)
			}
			return
		}
		time.Sleep(wait)
		if wait < time.Minute {
			wait *= 2
		}
	}
}

// Run tests one candidate and answers the verdict. It never panics a caller and never answers without a verdict.
// What is wrong with this room is VerdictCannot and never VerdictError, so a broken room holds the queue and refuses
// nobody.
func (m *MergeRunner) Run(ctx context.Context, j MergeJob) MergeReport {
	m.runs.Add(1)
	rep := MergeReport{N: j.N, Repo: j.Repo, Candidate: j.Candidate, Token: j.Token}
	say := func(verdict, format string, a ...any) MergeReport {
		rep.Verdict, rep.Reason = verdict, mergeCutReason(fmt.Sprintf(format, a...))
		return rep
	}
	fail := func(format string, a ...any) MergeReport { return say(VerdictError, format, a...) }
	cannot := func(format string, a ...any) MergeReport { return say(VerdictCannot, format, a...) }
	root := ""
	if m.Root != nil {
		root = m.Root()
	}
	if root == "" || m.Dir == "" {
		return cannot("this room has no git root or no merge directory")
	}
	clone := filepath.Join(root, filepath.FromSlash(j.Repo))
	if st, err := os.Stat(filepath.Join(clone, ".git")); err != nil || !st.IsDir() {
		return cannot("this room has no clone of %s at %s. sync it from the hub first", j.Repo, clone)
	}
	if m.Hub == nil {
		return cannot("this room is not linked to a hub's git")
	}
	rt, err := m.Hub()
	if err != nil {
		return cannot("this room cannot reach the hub's git: %v", err)
	}
	url, stop, err := HubLoopback(rt, j.Repo)
	if err != nil {
		return cannot("%v", err)
	}
	defer stop()
	g := m.runner()
	ref := MergeRef(j.N)
	if _, err := g.Git(ctx, clone, "fetch", "--no-tags", "--quiet", url, "+"+ref+":"+ref); err != nil {
		return cannot("could not fetch the candidate: %v", gitWhy(err))
	}
	defer func() { _, _ = g.Git(context.Background(), clone, "update-ref", "-d", ref) }()
	if got, err := g.Git(ctx, clone, "rev-parse", "--verify", "-q", ref+"^{commit}"); err != nil || strings.TrimSpace(got) != j.Candidate {
		return cannot("the hub's candidate for run %d is not %s", j.N, short(j.Candidate))
	}
	wt := filepath.Join(m.Dir, filepath.FromSlash(j.Repo))
	if err := m.worktree(ctx, clone, wt, j.Candidate); err != nil {
		return cannot("could not set up the merge worktree %s: %v", wt, gitWhy(err))
	}
	out, code, err := m.test(ctx, wt, j.Base)
	if err == nil && code != 0 && RedRerun(out) {
		out, code, err = m.test(ctx, wt, j.Base)
		rep.Note = "rerun once after a unit budget miss"
	}
	rep.Output = MergeCut(out)
	var cr *cannotRun
	known, _ := os.ReadFile(filepath.Join(wt, filepath.FromSlash(KnownFailingFile)))
	switch {
	case errors.As(err, &cr):
		return cannot("%v", err)
	case err != nil:
		return fail("%v", err)
	case code == 0:
		rep.Verdict = VerdictGreen
	case RedKnown(out, string(known)):
		rep.Verdict = VerdictGreen
		rep.Note = strings.TrimPrefix(rep.Note+", every failing test is in "+KnownFailingFile, ", ")
	default:
		rep.Verdict = VerdictRed
		rep.Failing = MergeFailing(out)
		rep.Reason = fmt.Sprintf("%s exited %d", MergeScript, code)
	}
	return rep
}

func gitWhy(err error) string {
	var ge *Error
	if errors.As(err, &ge) {
		if f := ge.First(); f != "" {
			return f
		}
	}
	return err.Error()
}

// worktree makes or resets the merge worktree at wt, detached at sha, with untracked files gone and ignored ones kept.
func (m *MergeRunner) worktree(ctx context.Context, clone, wt, sha string) error {
	g := m.runner()
	if st, err := os.Stat(filepath.Join(wt, ".git")); err == nil && !st.IsDir() {
		if _, err := g.Git(ctx, wt, "checkout", "--quiet", "--detach", "--force", sha); err != nil {
			return err
		}
		_, err := g.Git(ctx, wt, "clean", "-fdq")
		return err
	}
	_, _ = g.Git(ctx, clone, "worktree", "prune")
	if err := os.MkdirAll(filepath.Dir(wt), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(wt); err == nil {
		if err := os.RemoveAll(wt); err != nil {
			return err
		}
	}
	_, err := g.Git(ctx, clone, "worktree", "add", "--quiet", "--detach", wt, sha)
	return err
}

func (m *MergeRunner) bash() string {
	if m.Bash != "" {
		return m.Bash
	}
	if runtime.GOOS == "windows" {
		if git, err := exec.LookPath("git"); err == nil {
			for _, rel := range []string{"../bin/bash.exe", "../../bin/bash.exe", "../usr/bin/bash.exe"} {
				p := filepath.Clean(filepath.Join(filepath.Dir(git), rel))
				if _, err := os.Stat(p); err == nil {
					return p
				}
			}
		}
	}
	return "bash"
}

// test runs the script in wt against base, and answers its output, kept to the last MiB, and its exit code. An error
// is a script that could not be run or was killed, which is not a verdict on the code.
func (m *MergeRunner) test(ctx context.Context, wt, base string) (string, int, error) {
	if _, err := os.Stat(filepath.Join(wt, filepath.FromSlash(MergeScript))); err != nil {
		return "", 0, fmt.Errorf("the candidate has no %s", MergeScript)
	}
	bound := m.Bound
	if bound <= 0 {
		bound = MergeTestBound
	}
	ctx, cancel := context.WithTimeout(ctx, bound)
	defer cancel()
	cmd := exec.CommandContext(ctx, m.bash(), MergeScript, "--base", base)
	cmd.Dir = wt
	cmd.Env = CleanEnv()
	prepareTree(cmd)
	var buf tailBuffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	cmd.WaitDelay = 10 * time.Second
	var tree atomic.Pointer[procTree]
	cmd.Cancel = func() error {
		if t := tree.Load(); t != nil {
			return t.kill()
		}
		return cmd.Process.Kill()
	}
	err := cmd.Start()
	if err == nil {
		t := startTree(cmd)
		tree.Store(t)
		err = cmd.Wait()
		if t != nil {
			t.close()
		}
	}
	out := buf.String()
	var ee *exec.ExitError
	switch {
	case ctx.Err() != nil:
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return out, 0, fmt.Errorf("%s did not finish in %s and was killed", MergeScript, bound)
		}
		return out, 0, &cannotRun{"the run was stopped: a newer run of this repository took its place"}
	case errors.As(err, &ee):
		return out, ee.ExitCode(), nil
	case err != nil:
		return out, 0, &cannotRun{fmt.Sprintf("could not run %s: %v", MergeScript, err)}
	}
	return out, 0, nil
}

// cannotRun is the room failing to run the script, which says nothing of the candidate.
type cannotRun struct{ msg string }

func (e *cannotRun) Error() string { return e.msg }

// tailBuffer keeps the first 64 KiB and the last of what is written, up to mergeOutputKeep in all.
type tailBuffer struct {
	mu   sync.Mutex
	head bytes.Buffer
	tail []byte
	cut  int
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if room := 64<<10 - b.head.Len(); room > 0 {
		k := min(room, len(p))
		b.head.Write(p[:k])
		p = p[k:]
	}
	b.tail = append(b.tail, p...)
	if over := len(b.tail) - (mergeOutputKeep - 64<<10); over > 0 {
		b.tail = append(b.tail[:0], b.tail[over:]...)
		b.cut += over
	}
	return n, nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cut == 0 {
		return b.head.String() + string(b.tail)
	}
	return b.head.String() + fmt.Sprintf("\n[... %d bytes cut ...]\n", b.cut) + string(b.tail)
}

var (
	goFailRe   = regexp.MustCompile(`(?m)^\s*--- FAIL: (\S+)`)
	stepFailRe = regexp.MustCompile(`(?m)^FAILED (.+?)\s*$`)
	pkgFailRe  = regexp.MustCompile(`(?m)^FAIL\s+(\S+)\s`)
)

// MergeFailing names what failed in a test-touched output: each Go test, each package, then each step, once, in
// order, at most MergeFailingMax.
func MergeFailing(out string) []string {
	out = ansiRe.ReplaceAllString(out, "")
	seen := map[string]bool{}
	var got []string
	for _, re := range []*regexp.Regexp{goFailRe, pkgFailRe, stepFailRe} {
		for _, m := range re.FindAllStringSubmatch(out, -1) {
			name := MergeClean(m[1])
			if len(name) > MergeNameMax {
				name = strings.ToValidUTF8(name[:MergeNameMax], "")
			}
			if name == "" || seen[name] || len(got) >= MergeFailingMax {
				continue
			}
			seen[name] = true
			got = append(got, name)
		}
	}
	return got
}

// MergePrune is a request to delete a landed branch from this room's clone.
type MergePrune struct {
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
	SHA    string `json:"sha"`
}

func (m *MergeRunner) servePrune(w http.ResponseWriter, r *http.Request) {
	var p MergePrune
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<14)).Decode(&p); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read that: " + err.Error()})
		return
	}
	deleted, why, err := m.Prune(r.Context(), p)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted, "kept": why})
}

// Prune deletes a landed branch from the room's clone when it is at sha and no worktree has it out. It answers why it
// kept one, and an error only for a request it will not consider.
func (m *MergeRunner) Prune(ctx context.Context, p MergePrune) (bool, string, error) {
	if _, err := ParseName(p.Repo); err != nil {
		return false, "", err
	}
	if why := CheckPushBranch(p.Branch); why != "" || !strings.HasPrefix(p.Branch, "claude/") || p.Branch == IntegrationBranch {
		return false, "", errors.New("only a landed claude/* branch is pruned")
	}
	if !ValidSHA(p.SHA) {
		return false, "", errors.New("a sha is a full sha")
	}
	root := ""
	if m.Root != nil {
		root = m.Root()
	}
	clone := filepath.Join(root, filepath.FromSlash(p.Repo))
	if root == "" {
		return false, "this room has no git root", nil
	}
	if st, err := os.Stat(filepath.Join(clone, ".git")); err != nil || !st.IsDir() {
		return false, "this room has no clone of " + p.Repo, nil
	}
	g := m.runner()
	ref := headsPrefix + p.Branch
	tip, err := g.Git(ctx, clone, "rev-parse", "--verify", "-q", ref)
	if err != nil {
		return false, "no such branch here", nil
	}
	if strings.TrimSpace(tip) != p.SHA {
		return false, "the branch here is at " + short(strings.TrimSpace(tip)) + ", not the landed " + short(p.SHA), nil
	}
	list, err := g.Git(ctx, clone, "worktree", "list", "--porcelain")
	if err != nil {
		return false, "", err
	}
	path := ""
	for _, l := range strings.Split(list, "\n") {
		if strings.HasPrefix(l, "worktree ") {
			path = strings.TrimPrefix(l, "worktree ")
		}
		if strings.TrimSpace(l) == "branch "+ref {
			return false, "checked out at " + path, nil
		}
	}
	if _, err := g.Git(ctx, clone, "update-ref", "-d", ref, p.SHA); err != nil {
		return false, "", err
	}
	return true, "", nil
}
