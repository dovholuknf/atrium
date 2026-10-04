package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/forge"
	prrender "github.com/dovholuknf/atrium/internal/prreview/render"
	"github.com/dovholuknf/atrium/internal/store"
)

// The pulls-view runner (docs/rnd/pulls-view-design.md section 5, P1).
//
// A review is a row, never a card. The runner fetches the PR into the run
// folder, primes ONE claude session with the whole bundle, and then every
// reviewer, verifier and critic is a one-shot fork of that session, the way
// keep-alive forks a card (keepalive.go). Reviewers return JSON, and step 8 is
// Go: prrender.Render writes the finding files and walk.txt.
//
// Every move of the row goes through store.MovePR with the states it may come
// from. An abort marks the row `aborted` before it calls Abort, so a runner that
// finds the row gone from its state stops and writes nothing more.

const (
	prGHTimeout   = 2 * time.Minute
	prGitTimeout  = 5 * time.Minute
	prForkTimeout = 10 * time.Minute
	prDiffLimit   = 8 << 20
	prJSONLimit   = 4 << 20
	// prFileLimit and prBundleFiles bound the changed files put in bundle.md.
	prFileLimit   = 200 << 10
	prBundleFiles = 600 << 10
	// prVerifyForks is the most verify forks one run starts.
	prVerifyForks = 3
)

// The steps a runner records, in order. Steps 6 and 7 (second, settle) are P3.
var prSteps = []string{"fetch", "prime", "panel", "verify", "critics", "merge", "write"}

// prCmd is one named outbound command.
type prCmd = forge.Cmd

// prRunner is the daemon's PRRunner.
type prRunner struct {
	st      *store.Store
	publish func(id string)
	// fork runs one claude call. run runs the forge's CLI and git. Both are seams for tests.
	fork func(ctx context.Context, spec forkSpec) ([]byte, error)
	run  func(ctx context.Context, c prCmd) ([]byte, error)
	// forgeOf, when set, picks the forge for a host instead of the providers. A seam for tests.
	forgeOf func(host string) (forge.Forge, error)
	now     func() time.Time
	// baseEnv is the environment every child starts from.
	baseEnv func() []string
	// agentsDir is where the panel's agent definitions are.
	agentsDir    func() string
	userSettings func() []byte

	mu      sync.Mutex
	cancels map[string]context.CancelFunc
	wg      sync.WaitGroup
}

func newPRRunner(st *store.Store, publish func(string)) *prRunner {
	r := &prRunner{
		st: st, publish: publish, fork: runForkProcess, now: func() time.Time { return time.Now().UTC() },
		baseEnv:      os.Environ,
		userSettings: func() []byte { return readUserSettings() },
		cancels:      map[string]context.CancelFunc{},
		agentsDir: func() string {
			home, _ := os.UserHomeDir()
			return filepath.Join(home, ".claude", "agents")
		},
	}
	r.run = r.runBounded
	return r
}

// Start moves a queued row to fetching and runs it in the background. A row that
// is not queued is somebody else's run and is left alone.
func (r *prRunner) Start(id string) {
	if _, err := r.st.MovePR(id, []string{store.PRQueued}, store.PRFetching, "fetch", ""); err != nil {
		log.Printf("[atrium] pr %s: not started: %v", id, err)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.mu.Lock()
	r.cancels[id] = cancel
	r.mu.Unlock()
	r.publish(id)
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer func() {
			r.mu.Lock()
			delete(r.cancels, id)
			r.mu.Unlock()
			cancel()
		}()
		r.execute(ctx, id)
	}()
}

// Abort cancels the run, which kills its forks. The API has already marked the
// row aborted and deletes the folder.
func (r *prRunner) Abort(id string) {
	r.mu.Lock()
	cancel := r.cancels[id]
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Wait blocks until every run has ended. For tests.
func (r *prRunner) Wait() { r.wg.Wait() }

// Stop cancels every run, for the daemon's shutdown.
func (r *prRunner) Stop() {
	r.mu.Lock()
	for _, c := range r.cancels {
		c()
	}
	r.mu.Unlock()
}

// runBounded runs a command with a time bound and a read bound, as sources do:
// the output is cut as it is read, not after it has all been held.
func (r *prRunner) runBounded(ctx context.Context, c prCmd) ([]byte, error) {
	if c.Timeout <= 0 {
		c.Timeout = prGHTimeout
	}
	if c.Limit <= 0 {
		c.Limit = prJSONLimit
	}
	runCtx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, c.Name, c.Args...)
	cmd.Dir = c.Dir
	hideWindow(cmd)
	// git must never stop to ask for a credential nobody is there to type.
	cmd.Env = childEnvFrom(r.baseEnv(), nil, map[string]string{"GIT_TERMINAL_PROMPT": "0"})
	var out, errOut bytes.Buffer
	cmd.Stdout = &limitedWriter{w: &out, left: c.Limit + 1}
	cmd.Stderr = &limitedWriter{w: &errOut, left: 8 << 10}
	err := cmd.Run()
	label := c.Name
	if len(c.Args) > 0 {
		label += " " + c.Args[0]
		if c.Name != "git" && len(c.Args) > 1 {
			label += " " + c.Args[1]
		}
	}
	switch {
	case ctx.Err() != nil:
		return nil, ctx.Err()
	case runCtx.Err() == context.DeadlineExceeded:
		return nil, fmt.Errorf("%s took longer than %s and was stopped", label, c.Timeout)
	case out.Len() > c.Limit:
		return nil, fmt.Errorf("%s printed more than %d bytes and was stopped", label, c.Limit)
	case err != nil:
		if errOut.Len() > 0 {
			return nil, fmt.Errorf("%s: %s", label, firstLine(errOut.String()))
		}
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	return out.Bytes(), nil
}

// ---- review.json ----

type prForkRec struct {
	Label      string  `json:"label"`
	Cost       float64 `json:"cost_usd"`
	Input      int64   `json:"input"`
	CacheWrite int64   `json:"cache_write"`
	CacheRead  int64   `json:"cache_read"`
	Output     int64   `json:"output"`
	Turns      int     `json:"turns"`
	Denials    int     `json:"permission_denials"`
	Error      string  `json:"error,omitempty"`
}

type prStepRec struct {
	Started string      `json:"started"`
	Ended   string      `json:"ended"`
	Seconds float64     `json:"seconds"`
	Cost    float64     `json:"cost_usd"`
	Done    bool        `json:"done"`
	Error   string      `json:"error,omitempty"`
	Forks   []prForkRec `json:"forks,omitempty"`
}

// prReviewFile is review.json: what the run was of, what it cost, and the cache
// reads of the forks against the prime's write (review-memory decision 1).
type prReviewFile struct {
	PR           string                `json:"pr"`
	Head         string                `json:"head"`
	Recipe       string                `json:"recipe"`
	Harness      string                `json:"harness"`
	Panel        []string              `json:"panel"`
	Steps        map[string]*prStepRec `json:"steps"`
	Prime        prForkRec             `json:"prime"`
	PrimeSession string                `json:"prime_session"`
	Cache        struct {
		PrimeWrite int64 `json:"prime_write"`
		ForkReads  int64 `json:"fork_reads"`
		ForkWrites int64 `json:"fork_writes"`
	} `json:"cache"`
	Denials      int     `json:"permission_denials"`
	CostUSD      float64 `json:"cost_usd"`
	BudgetUSD    float64 `json:"budget_usd"`
	BudgetRaised bool    `json:"budget_raised"`
	BudgetFailed bool    `json:"budget_failed"`
	Started      string  `json:"started"`
	Ready        string  `json:"ready"`
	Seconds      float64 `json:"seconds"`
	Findings     int     `json:"findings"`
}

// ---- one run ----

type prRun struct {
	r       *prRunner
	ctx     context.Context
	id      string
	row     *store.PRReview
	dir     string
	recipe  *store.PRRecipe
	harness *store.Harness

	mu      sync.Mutex
	review  prReviewFile
	started time.Time
	spent   float64
	cap     float64

	commonMu sync.Mutex
	workDir  string
	srcAbs   string
	common   []string
	env      []string
	files    []prFile
	raw      []prrender.Finding
	verd     []prVerdict
	final    []prrender.Finding
}

type prFile = forge.File

// errStopped means the row left the states a runner may move it from: it was
// aborted, and the run ends without writing anything.
var errStopped = errors.New("the run was stopped")

type stepErr struct {
	step string
	err  error
}

func (e *stepErr) Error() string { return e.err.Error() }
func (e *stepErr) Unwrap() error { return e.err }

type budgetErr struct{ spent, cap float64 }

func (e *budgetErr) Error() string {
	return fmt.Sprintf("spent $%.2f of the $%.2f budget, retry raises it once", e.spent, e.cap)
}

func (r *prRunner) execute(ctx context.Context, id string) {
	row, err := r.st.PRByID(id)
	if err != nil {
		log.Printf("[atrium] pr %s: %v", id, err)
		return
	}
	pr := &prRun{r: r, ctx: ctx, id: id, row: row, started: r.now()}
	if err := pr.begin(); err != nil {
		pr.fail("recipe", err)
		return
	}
	fns := map[string]func() error{"fetch": pr.fetch, "prime": pr.prime, "panel": pr.panel, "verify": pr.verify,
		"critics": pr.critics, "merge": pr.merge, "write": pr.render}
	for _, name := range prSteps {
		err := pr.step(name, fns[name])
		var se *stepErr
		var be *budgetErr
		switch {
		case err == nil:
		case errors.Is(err, errStopped) || ctx.Err() != nil:
			return
		case errors.As(err, &be):
			pr.mu.Lock()
			pr.review.BudgetFailed = true
			pr.mu.Unlock()
			pr.saveReview()
			pr.fail("budget", err)
			return
		case errors.As(err, &se):
			pr.fail(se.step, se.err)
			return
		default:
			pr.fail(name, err)
			return
		}
	}
	pr.finish()
}

// begin loads the recipe and what an earlier run of this folder left. A retry
// keeps the folder, so steps that finished are not run again, and what they cost
// is carried onto the row, which a reset zeroed.
func (pr *prRun) begin() error {
	r := pr.r
	pr.dir = filepath.FromSlash(pr.row.RunDir)
	recipe, err := r.st.RecipeFor(pr.row.OrgRepo)
	if err != nil {
		return fmt.Errorf("no recipe: %w", err)
	}
	h, err := r.st.Harness(recipe.Harness)
	if err != nil {
		return fmt.Errorf("the recipe's harness %q: %w", recipe.Harness, err)
	}
	pr.recipe, pr.harness = recipe, h
	pr.review = prReviewFile{Steps: map[string]*prStepRec{}}
	if b, err := os.ReadFile(filepath.Join(pr.dir, "review.json")); err == nil {
		var old prReviewFile
		if json.Unmarshal(b, &old) == nil && old.Steps != nil {
			pr.review = old
		}
	}
	if pr.review.BudgetFailed && !pr.review.BudgetRaised {
		pr.review.BudgetRaised = true
	}
	pr.review.BudgetFailed = false
	pr.review.PR, pr.review.Recipe, pr.review.Harness = pr.row.URL, recipe.Name, h.ID
	pr.review.BudgetUSD = recipe.BudgetUSD
	pr.cap = recipe.BudgetUSD
	if pr.review.BudgetRaised {
		pr.cap *= 2
	}
	if pr.review.Started == "" {
		pr.review.Started = pr.started.Format(time.RFC3339)
	}
	carried := 0.0
	for _, s := range pr.review.Steps {
		carried += s.Cost
	}
	pr.spent = carried
	if carried > pr.row.CostUSD {
		if _, err := r.st.AddPRCost(pr.id, carried-pr.row.CostUSD); err != nil {
			return err
		}
	}
	return nil
}

// step runs one step: the state move, the budget check, the log lines.
func (pr *prRun) step(name string, fn func() error) error {
	if pr.ctx.Err() != nil {
		return errStopped
	}
	if rec := pr.review.Steps[name]; rec != nil && rec.Done && name != "write" {
		if fn() == nil {
			return nil
		}
		rec.Done = false
	}
	if name != "fetch" {
		pr.mu.Lock()
		over := pr.spent >= pr.cap
		spent, cap := pr.spent, pr.cap
		pr.mu.Unlock()
		if over {
			return &budgetErr{spent, cap}
		}
		if _, err := pr.r.st.MovePR(pr.id, []string{store.PRFetching, store.PRRunning}, store.PRRunning, name, ""); err != nil {
			if errors.Is(err, store.ErrPRState) {
				return errStopped
			}
			return err
		}
		pr.r.publish(pr.id)
	}
	t0 := pr.r.now()
	pr.mu.Lock()
	rec := &prStepRec{Started: t0.Format(time.RFC3339)}
	if old := pr.review.Steps[name]; old != nil {
		// A rerun keeps what the earlier try spent.
		rec.Cost = old.Cost
	}
	pr.review.Steps[name] = rec
	before := rec.Cost
	pr.mu.Unlock()
	pr.logf("%s start", name)
	err := fn()
	t1 := pr.r.now()
	pr.mu.Lock()
	rec.Ended, rec.Seconds = t1.Format(time.RFC3339), t1.Sub(t0).Seconds()
	rec.Done = err == nil
	if err != nil {
		rec.Error = err.Error()
	}
	cost := rec.Cost - before
	pr.mu.Unlock()
	if pr.ctx.Err() != nil {
		return errStopped
	}
	if err != nil {
		pr.logf("%s error %s", name, firstLine(err.Error()))
	} else {
		pr.logf("%s end %.2f", name, cost)
	}
	pr.saveReview()
	pr.r.publish(pr.id)
	return err
}

func (pr *prRun) logf(format string, a ...any) {
	if pr.ctx.Err() != nil {
		return
	}
	line := pr.r.now().Format("15:04:05") + " " + fmt.Sprintf(format, a...) + "\n"
	f, err := os.OpenFile(filepath.Join(pr.dir, "run.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(line)
}

func (pr *prRun) saveReview() {
	if pr.ctx.Err() != nil {
		return
	}
	pr.mu.Lock()
	pr.review.CostUSD = pr.spent
	b, err := json.MarshalIndent(pr.review, "", "  ")
	pr.mu.Unlock()
	if err != nil {
		return
	}
	if _, err := os.Stat(pr.dir); err != nil {
		return
	}
	os.WriteFile(filepath.Join(pr.dir, "review.json"), b, 0o644)
}

// write puts a file in the run folder, unless the run was stopped, so a late
// answer never rebuilds a folder an abort has deleted.
func (pr *prRun) write(rel string, data []byte) error {
	if pr.ctx.Err() != nil {
		return errStopped
	}
	p := filepath.Join(pr.dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

func (pr *prRun) fail(step string, err error) {
	if pr.ctx.Err() != nil {
		return
	}
	pr.logf("%s failed %s", step, firstLine(err.Error()))
	if _, merr := pr.r.st.MovePR(pr.id, []string{store.PRFetching, store.PRRunning}, store.PRFailed, "",
		step+": "+firstLine(err.Error())); merr != nil {
		log.Printf("[atrium] pr %s: could not record the failure of %s: %v", pr.id, step, merr)
		return
	}
	pr.r.publish(pr.id)
}

func (pr *prRun) finish() {
	if pr.ctx.Err() != nil {
		return
	}
	pr.mu.Lock()
	now := pr.r.now()
	pr.review.Ready = now.Format(time.RFC3339)
	if t, err := time.Parse(time.RFC3339, pr.review.Started); err == nil {
		pr.review.Seconds = now.Sub(t).Seconds()
	}
	pr.review.Findings = len(pr.final)
	pr.mu.Unlock()
	pr.saveReview()
	if _, err := pr.r.st.MovePR(pr.id, []string{store.PRRunning}, store.PRReady, "", ""); err != nil {
		return
	}
	pr.logf("ready")
	pr.r.publish(pr.id)
}

// ---- 1 fetch ----

// forge is the forge of the row's host: an enabled provider with that host and its
// forge field, else the built-in default for the host.
func (pr *prRun) forge() (forge.Forge, error) {
	if pr.r.forgeOf != nil {
		return pr.r.forgeOf(pr.row.Host)
	}
	return pr.r.forgeFor(pr.row.Host)
}

func (r *prRunner) forgeFor(host string) (forge.Forge, error) {
	if host == "" {
		host = "github.com"
	}
	var entries []forge.Entry
	if r.st != nil {
		rows, err := r.st.Providers()
		if err != nil {
			return nil, err
		}
		for _, p := range rows {
			if p.Enabled && p.Host != "" {
				entries = append(entries, forge.Entry{Host: p.Host, Forge: p.Forge, Cmd: p.ForgeCmd})
			}
		}
	}
	return forge.For(host, entries, r.run)
}

func (pr *prRun) ref() forge.Ref {
	return forge.Ref{Host: pr.row.Host, Org: pr.row.Org, Repo: pr.row.Repo, Number: pr.row.Number}
}

func (pr *prRun) prURL() string {
	f, err := pr.forge()
	if err != nil {
		return ""
	}
	return f.PRURL(pr.ref())
}

func (pr *prRun) readPRJSON() (*forge.PR, error) {
	b, err := os.ReadFile(filepath.Join(pr.dir, "pr.json"))
	if err != nil {
		return nil, err
	}
	var v forge.PR
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	if v.Head == "" {
		return nil, errors.New("pr.json holds no head")
	}
	pr.files = v.Files
	return &v, nil
}

func (pr *prRun) fetch() error {
	if _, err := os.Stat(filepath.Join(pr.dir, "bundle.md")); err == nil && pr.review.Steps["fetch"] != nil &&
		pr.review.Steps["fetch"].Done {
		if _, err := pr.readPRJSON(); err == nil {
			return nil
		}
	}
	f, err := pr.forge()
	if err != nil {
		return err
	}
	ref := pr.ref()
	v, err := f.View(pr.ctx, ref)
	if err != nil {
		return err
	}
	if !store.ValidPRHead(v.Head) || v.Head == "" {
		return errors.New(f.Kind() + " gave no usable head")
	}
	// The folder is named for the head, which a caller who did not know it left as
	// `pending`. Done first, so every later write lands in the final name.
	if err := pr.rename(v.Head); err != nil {
		return err
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := pr.write("pr.json", out); err != nil {
		return err
	}
	pr.files = v.Files
	diff, err := f.Diff(pr.ctx, ref)
	if err != nil {
		return err
	}
	if err := pr.write("pr.diff", diff); err != nil {
		return err
	}
	if err := pr.fetchSource(f.FetchSpec(ref), v.Head); err != nil {
		return err
	}
	bundle := pr.bundle(v, string(diff))
	if err := pr.write("bundle.md", []byte(bundle)); err != nil {
		return err
	}
	pr.mu.Lock()
	pr.review.Head = strings.ToLower(v.Head)
	pr.mu.Unlock()
	row, err := pr.r.st.SetPRFetched(pr.id, v.Head, v.Title, v.Author, filepath.ToSlash(pr.dir))
	if err != nil {
		return err
	}
	pr.row = row
	return nil
}

// rename moves the run into the folder named for head, when it is not already.
func (pr *prRun) rename(head string) error {
	want := fmt.Sprintf("pr-%d-%s", pr.row.Number, strings.ToLower(head)[:7])
	if filepath.Base(pr.dir) == want {
		return nil
	}
	to := filepath.Join(filepath.Dir(pr.dir), want)
	if _, err := os.Stat(to); err == nil {
		return fmt.Errorf("the folder %s is already there", want)
	}
	if err := os.Rename(pr.dir, to); err != nil {
		return err
	}
	old := pr.dir
	pr.dir = to
	// The row names the folder at once, so an abort deletes the one that exists.
	row, err := pr.r.st.SetPRFetched(pr.id, head, pr.row.Title, pr.row.Author, filepath.ToSlash(to))
	if err != nil {
		if rerr := os.Rename(to, old); rerr != nil {
			// The folder stays where it is, and the row has not moved to it.
			pr.dir = to
			return fmt.Errorf("%w, and the folder could not be moved back: %v", err, rerr)
		}
		pr.dir = old
		return err
	}
	pr.row = row
	// An abort between the rename and the row update deleted the old name, which
	// was already gone. The folder it meant is this one.
	if pr.ctx.Err() != nil {
		os.RemoveAll(to)
		return errStopped
	}
	return nil
}

// fetchSource is a blobless shallow fetch of the PR head into src/.
func (pr *prRun) fetchSource(spec forge.FetchSpec, head string) error {
	src := filepath.Join(pr.dir, "src")
	if err := os.RemoveAll(src); err != nil {
		return err
	}
	if err := os.MkdirAll(src, 0o755); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"remote", "add", "origin", spec.Remote},
		{"fetch", "-q", "--depth", "1", "--filter=blob:none", "origin", spec.Refspec},
		{"checkout", "-q", "--detach", "FETCH_HEAD"},
	} {
		if _, err := pr.r.run(pr.ctx, prCmd{Name: "git", Args: args, Dir: src, Timeout: prGitTimeout,
			Limit: prJSONLimit}); err != nil {
			return err
		}
	}
	out, err := pr.r.run(pr.ctx, prCmd{Name: "git", Args: []string{"rev-parse", "HEAD"}, Dir: src,
		Timeout: prGitTimeout, Limit: 1024})
	if err != nil {
		return err
	}
	if got := strings.TrimSpace(string(out)); !strings.EqualFold(got, head) {
		return fmt.Errorf("the fetched head %s is not the PR's head %s", got, head)
	}
	return nil
}

// bundle is what the prime reads, built here so no model builds it: a summary,
// the diff, and each changed file in full at the head.
func (pr *prRun) bundle(v *forge.PR, diff string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Pull request %s/%s #%d: %s\n\nauthor: %s\nbase: %s\nhead: %s\n\n## Changed files\n\n",
		pr.row.Org, pr.row.Repo, pr.row.Number, v.Title, v.Author, v.BaseRef, v.Head)
	for _, f := range v.Files {
		fmt.Fprintf(&b, "- %s (+%d -%d)\n", f.Path, f.Additions, f.Deletions)
	}
	b.WriteString("\n## Diff\n\n```diff\n" + diff + "\n```\n\n## Changed files at the head\n")
	room := prBundleFiles
	for _, f := range v.Files {
		p, err := safeJoin(filepath.Join(pr.dir, "src"), f.Path)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			// A deleted file has no head.
			continue
		}
		if len(data) > prFileLimit || bytes.IndexByte(data, 0) >= 0 {
			fmt.Fprintf(&b, "\n### %s\n\n(left out: too large or binary)\n", f.Path)
			continue
		}
		if len(data) > room {
			fmt.Fprintf(&b, "\n### %s\n\n(left out: the bundle is full, read it from src/)\n", f.Path)
			continue
		}
		room -= len(data)
		fmt.Fprintf(&b, "\n### %s\n\n```\n%s\n```\n", f.Path, strings.TrimRight(string(data), "\n"))
	}
	return b.String()
}

func safeJoin(root, rel string) (string, error) {
	p := filepath.Join(root, filepath.FromSlash(rel))
	if r, err := filepath.Rel(root, p); err != nil || strings.HasPrefix(r, "..") {
		return "", errors.New("outside")
	}
	return p, nil
}

// ---- forks ----

const prSchema = `{"findings": [{"sev": "high|med|low|nit", "path": "path as the diff names it", "line": 412,
  "code": "the text of that line", "says": "what is wrong, one or two sentences", "fix": "ONE change, or for a finding
  you have not proven, one question that starts 'Could we' and ends in a single '?'", "test_ask": "Add a test to
  <file>: <what to do>, expect <result>", "proven": "code|no", "impact": "one sentence: what breaks for a user",
  "rank": 0, "exposure": {"who": "who hits it", "likely": "how likely", "opt_in": "yes|no"},
  "cause": "introduced|pre-existing", "found": "traced|read, and what was and was not run", "leak": "empty, or
  the size and stack of a leak", "raised_by": "your name"}]}`

const prRules = `Rules: report a defect only on a line the PR adds or changes, and name the line that is wrong. Set
"proven" to "code" only when the code itself settles it, otherwise "no". A finding rated med or higher fills in
all three "exposure" answers. Every leak goes in with its size and stack in "leak". Nothing can be run, so say what
you traced. Return ONLY one JSON object in this schema, no prose and no code fence:
`

// agentBody reads ~/.claude/agents/<name>.md without its frontmatter.
func (r *prRunner) agentBody(name string) (string, error) {
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return "", fmt.Errorf("agent name %q", name)
	}
	b, err := os.ReadFile(filepath.Join(r.agentsDir(), name+".md"))
	if err != nil {
		return "", err
	}
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	if rest, ok := strings.CutPrefix(s, "---\n"); ok {
		if i := strings.Index(rest, "\n---\n"); i >= 0 {
			s = rest[i+5:]
		}
	}
	return strings.TrimSpace(s), nil
}

// forkCommon is the part of the command line the prime and every fork share, so
// they share a cache prefix: the lean flags, and read-only tools.
func (pr *prRun) forkCommon() error {
	pr.commonMu.Lock()
	defer pr.commonMu.Unlock()
	if pr.common != nil {
		return nil
	}
	h := pr.harness
	args := append([]string{}, h.Args...)
	lean, err := leanArgs(args, pr.r.userSettings(), "", nil, leanKit{}, pr.r.st.LeanWorkerGateway(), os.ReadFile)
	if err != nil {
		return err
	}
	// THE PR'S CHECKOUT IS NEVER A CLAUDE PROJECT. A PR can carry a .claude/settings.json
	// whose hooks would run as the daemon's user with its credentials, so the calls run
	// in a folder atrium wrote, with no setting source at all (the empty value: atrium's own hooks come in on --settings, and the operator's
	// settings.json would load a second time and run its other hooks), and reach src/
	// read-only through --add-dir. leanArgs asks for project,local: that is replaced.
	for i := 0; i+1 < len(lean); i++ {
		if lean[i] == "--setting-sources" {
			lean[i+1] = ""
		}
	}
	work := filepath.Join(pr.dir, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	src, err := filepath.Abs(filepath.Join(pr.dir, "src"))
	if err != nil {
		return err
	}
	pr.workDir, pr.srcAbs = work, src
	// --add-dir takes several values, so a flag follows it.
	pr.common = append(lean, "--add-dir", src, "--tools", "Read,Grep,Glob")
	// The permission gate is off: a fork never waits on an answer. The PreToolUse
	// hook still reports, for an agent atrium has never heard of, as keep-alive
	// forks do, and permission_denials in the receipt is how a run shows it never
	// blocked.
	extra := map[string]string{"ATRIUM_PERM_GATE": "off"}
	leanEnv(extra)
	if _, set := h.Env[keepaliveTTLVar]; !set {
		extra[keepaliveTTLVar] = "1h"
	}
	base := make([]string, 0, 64)
	for _, kv := range pr.r.baseEnv() {
		if !strings.HasPrefix(strings.ToUpper(kv), "ATRIUM_PERM_GATE=") {
			base = append(base, kv)
		}
	}
	pr.env = childEnvFrom(base, h.Env, extra)
	return nil
}

func prPrice(rec *forkReceipt) keepalivePrice {
	for m := range rec.ModelUsage {
		if p, ok := usagePriceFor(m); ok {
			return p
		}
	}
	p, _ := usagePriceFor("claude-sonnet-5-5")
	return p
}

// call runs one claude call in src/ with the prompt on stdin, records its cost
// and returns its receipt. dir is the step's folder under steps/, and a prompt.md
// and the answer are kept in it.
func (pr *prRun) call(step, label, dir string, args []string, prompt string) (*forkReceipt, error) {
	if err := pr.forkCommon(); err != nil {
		return nil, err
	}
	rel := "steps/" + dir
	if err := pr.write(rel+"/prompt.md", []byte(prompt)); err != nil {
		return nil, err
	}
	spec := forkSpec{Exe: pr.harness.Exe(), Dir: pr.workDir, Env: pr.env,
		Args: append(append([]string{"-p"}, args...), pr.common...), Stdin: []byte(prompt), Timeout: prForkTimeout}
	out, runErr := pr.r.fork(pr.ctx, spec)
	if pr.ctx.Err() != nil {
		return nil, errStopped
	}
	var rec *forkReceipt
	if t := bytes.TrimSpace(out); len(t) > 0 {
		var x forkReceipt
		if json.Unmarshal(t, &x) == nil {
			rec = &x
		}
		pr.write(rel+"/out.json", t)
	}
	fr := prForkRec{Label: label}
	var cost float64
	if rec != nil && rec.Usage != nil {
		p := prPrice(rec)
		cost = receiptCost(rec, p)
		fr.Cost, fr.Input, fr.CacheWrite, fr.CacheRead, fr.Output = cost, rec.Usage.Input, rec.Usage.CacheWrite,
			rec.Usage.CacheRead, rec.Usage.Output
	}
	if rec != nil {
		fr.Turns, fr.Denials = rec.NumTurns, len(rec.PermissionDenials)
	}
	var err error
	switch {
	case rec == nil && runErr != nil:
		err = runErr
	case rec == nil:
		err = errors.New("no receipt")
	case rec.Subtype != "success" || rec.IsError:
		err = fmt.Errorf("%s after %d turns", orStr(rec.Subtype, "failed"), rec.NumTurns)
	case strings.TrimSpace(rec.Result) == "":
		err = errors.New("an empty answer")
	}
	if err != nil {
		fr.Error = err.Error()
	}
	pr.mu.Lock()
	if s := pr.review.Steps[step]; s != nil {
		s.Cost += cost
		s.Forks = append(s.Forks, fr)
	}
	pr.spent += cost
	pr.review.Denials += fr.Denials
	if step == "prime" {
		pr.review.Prime = fr
		pr.review.Cache.PrimeWrite = fr.CacheWrite
	} else {
		pr.review.Cache.ForkReads += fr.CacheRead
		pr.review.Cache.ForkWrites += fr.CacheWrite
	}
	pr.mu.Unlock()
	if cost > 0 {
		if _, aerr := pr.r.st.AddPRCost(pr.id, cost); aerr != nil {
			log.Printf("[atrium] pr %s: recording cost: %v", pr.id, aerr)
		}
	}
	if err != nil {
		return rec, err
	}
	return rec, nil
}

func orStr(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// fork runs one fork of the prime and returns its answer's text.
func (pr *prRun) fork(step, label, dir, prompt string) (string, error) {
	pr.mu.Lock()
	session := pr.review.PrimeSession
	pr.mu.Unlock()
	if session == "" {
		return "", errors.New("there is no prime session to fork")
	}
	turns := pr.recipe.TurnsCap
	if turns <= 0 {
		turns = 12
	}
	rec, err := pr.call(step, label, dir, []string{"--resume", session, "--fork-session", "--no-session-persistence",
		"--max-turns", fmt.Sprint(turns), "--output-format", "json"}, prompt)
	if err != nil {
		return "", err
	}
	return rec.Result, nil
}

// extractJSON decodes the first JSON object in text, tolerating a code fence or
// a line of prose before it.
func extractJSON(text string, v any) error {
	i := strings.IndexByte(text, '{')
	if i < 0 {
		return errors.New("no JSON in the answer")
	}
	return json.NewDecoder(strings.NewReader(text[i:])).Decode(v)
}

// parallel runs fns at once and returns each one's error, in order.
func parallel(n int, fn func(i int) error) []error {
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = fn(i)
		}(i)
	}
	wg.Wait()
	return errs
}

// ---- 2 prime ----

func (pr *prRun) prime() error {
	pr.mu.Lock()
	have := pr.review.PrimeSession
	pr.mu.Unlock()
	if have != "" && pr.review.Steps["prime"] != nil && pr.review.Steps["prime"].Done {
		return nil
	}
	bundle, err := os.ReadFile(filepath.Join(pr.dir, "bundle.md"))
	if err != nil {
		return err
	}
	if err := pr.forkCommon(); err != nil {
		return err
	}
	prompt := "You are about to review a pull request with others. The bundle below is the whole pull request: " +
		"a summary, the diff, and each changed file in full at the head. The repository at the head is at " +
		pr.srcAbs + ", read only: name files by paths under it. Read the bundle, do not use any tool, and answer with the single word ok.\n\n" +
		string(bundle)
	rec, err := pr.call("prime", "prime", "prime", []string{"--max-turns", "1", "--output-format", "json"}, prompt)
	if err != nil {
		return err
	}
	if rec.SessionID == "" {
		return errors.New("the prime gave no session id")
	}
	pr.mu.Lock()
	pr.review.PrimeSession = rec.SessionID
	pr.mu.Unlock()
	return nil
}

// ---- 3 panel ----

type prFindings struct {
	Findings []prrender.Finding `json:"findings"`
}

func (pr *prRun) panel() error {
	if pr.review.Steps["panel"] != nil && pr.review.Steps["panel"].Done {
		var got prFindings
		if err := pr.readStep("panel/out.json", &got); err == nil {
			pr.raw = got.Findings
			return nil
		}
	}
	if _, err := pr.readPRJSON(); err != nil {
		return err
	}
	paths := make([]string, len(pr.files))
	for i, f := range pr.files {
		paths[i] = f.Path
	}
	entries, err := pr.recipe.ReviewersFor(paths)
	if err != nil {
		return fmt.Errorf("the recipe's panel: %w", err)
	}
	type job struct{ agent, prompt string }
	var jobs []job
	seen := map[string]bool{}
	for _, e := range entries {
		if seen[e.Agent] {
			continue
		}
		seen[e.Agent] = true
		body, err := pr.r.agentBody(e.Agent)
		if err != nil {
			pr.logf("panel skips %s: %v", e.Agent, err)
			continue
		}
		jobs = append(jobs, job{e.Agent, body + "\n\n---\n\nYou are " + e.Agent + ". Review this pull request.\n" +
			prRules + prSchema})
	}
	if len(jobs) == 0 {
		return errors.New("no reviewer in the panel applies, or none of their agent files was found")
	}
	pr.mu.Lock()
	pr.review.Panel = nil
	for _, j := range jobs {
		pr.review.Panel = append(pr.review.Panel, j.agent)
	}
	pr.mu.Unlock()
	got := make([][]prrender.Finding, len(jobs))
	errs := parallel(len(jobs), func(i int) error {
		text, err := pr.fork("panel", jobs[i].agent, "panel/"+jobs[i].agent, jobs[i].prompt)
		if err != nil {
			return err
		}
		var out prFindings
		if err := extractJSON(text, &out); err != nil {
			return err
		}
		for k := range out.Findings {
			if out.Findings[k].RaisedBy == "" {
				out.Findings[k].RaisedBy = jobs[i].agent
			}
		}
		got[i] = out.Findings
		return nil
	})
	failed := 0
	for i, e := range errs {
		if e != nil {
			if errors.Is(e, errStopped) {
				return e
			}
			failed++
			pr.logf("panel %s failed %s", jobs[i].agent, firstLine(e.Error()))
		}
	}
	if failed == len(jobs) {
		return fmt.Errorf("every reviewer failed: %v", firstErr(errs))
	}
	var all []prrender.Finding
	n := 0
	for _, fs := range got {
		for _, f := range fs {
			n++
			f.ID = fmt.Sprintf("p%d", n)
			all = append(all, f)
		}
	}
	pr.raw = all
	return pr.writeJSON("steps/panel/out.json", prFindings{Findings: all})
}

func firstErr(errs []error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

func (pr *prRun) readStep(rel string, v any) error {
	b, err := os.ReadFile(filepath.Join(pr.dir, "steps", filepath.FromSlash(rel)))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func (pr *prRun) writeJSON(rel string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return pr.write(rel, b)
}

// ---- 4 verify, critics ----

type prVerdict struct {
	ID      string `json:"id"`
	Verdict string `json:"verdict"` // holds, does_not_hold, holds_at
	Sev     string `json:"sev"`
	Because string `json:"because"`
	Proven  string `json:"proven"`
}

func sevRank(s string) int {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "high", "blocking", "critical":
		return 0
	case "med", "medium":
		return 1
	case "low":
		return 2
	}
	return 3
}

func (pr *prRun) verify() error {
	if pr.review.Steps["verify"] != nil && pr.review.Steps["verify"].Done {
		var got struct {
			Verdicts []prVerdict `json:"verdicts"`
		}
		if err := pr.readStep("verify/out.json", &got); err == nil {
			pr.verd = got.Verdicts
			return nil
		}
	}
	at := map[string]int{"high": 0, "med": 1, "low": 2}
	limit, on := at[pr.recipe.VerifyAt]
	var eligible []prrender.Finding
	if on {
		for _, f := range pr.raw {
			if sevRank(f.Sev) <= limit {
				eligible = append(eligible, f)
			}
		}
	}
	pr.verd = nil
	if len(eligible) > 0 {
		forks := prVerifyForks
		if len(eligible) < forks {
			forks = len(eligible)
		}
		per := (len(eligible) + forks - 1) / forks
		var mu sync.Mutex
		errs := parallel(forks, func(i int) error {
			lo, hi := i*per, (i+1)*per
			if hi > len(eligible) {
				hi = len(eligible)
			}
			if lo >= hi {
				return nil
			}
			list, _ := json.MarshalIndent(eligible[lo:hi], "", " ")
			prompt := "Verify these findings of a review of this pull request, each one on its own. Read the code " +
				"in src/ and the diff, and answer per finding with: verdict \"holds\", \"does_not_hold\" (say " +
				"because), or \"holds_at\" (the severity it holds at, in sev). Set proven to \"code\" only if the " +
				"code itself settles it, else \"no\". Say what you read in because. Return ONLY one JSON object: " +
				`{"verdicts": [{"id": "p1", "verdict": "holds", "sev": "med", "because": "...", "proven": "no"}]}` +
				"\n\nThe findings:\n" + string(list)
			text, err := pr.fork("verify", fmt.Sprintf("verify-%d", i+1), fmt.Sprintf("verify/%d", i+1), prompt)
			if err != nil {
				return err
			}
			var out struct {
				Verdicts []prVerdict `json:"verdicts"`
			}
			if err := extractJSON(text, &out); err != nil {
				return err
			}
			mu.Lock()
			pr.verd = append(pr.verd, out.Verdicts...)
			mu.Unlock()
			return nil
		})
		for _, e := range errs {
			if e != nil && errors.Is(e, errStopped) {
				return e
			}
		}
		if failed := firstErr(errs); failed != nil {
			// A verify that failed leaves its findings unverified, which the merge
			// step is told. It does not fail the run.
			pr.logf("verify: a fork failed %s", firstLine(failed.Error()))
		}
	}
	return pr.writeJSON("steps/verify/out.json", map[string]any{"verdicts": pr.verd})
}

var prCriticAsk = map[string]string{
	"coverage": "You are the coverage critic. Read the findings below, then read the diff and src/ for what they " +
		"MISSED: changed code with no finding, error paths, resource handling, missing tests. Report only NEW " +
		"findings, never a repeat of one below.",
	"consumers": "You are the consumers critic. For each changed public function, type or behaviour, find who calls " +
		"or relies on it, in src/ and in the known consumers below, and report what breaks for them as NEW findings. " +
		"Never repeat one below.",
}

func (pr *prRun) critics() error {
	if pr.review.Steps["critics"] != nil && pr.review.Steps["critics"].Done {
		var got prFindings
		if err := pr.readStep("critics/out.json", &got); err == nil {
			pr.raw = append(pr.raw, got.Findings...)
			return nil
		}
	}
	var names []string
	if err := json.Unmarshal([]byte(orStr(pr.recipe.Critics, "[]")), &names); err != nil {
		return fmt.Errorf("the recipe's critics: %w", err)
	}
	if len(names) == 0 {
		return pr.writeJSON("steps/critics/out.json", prFindings{Findings: []prrender.Finding{}})
	}
	consumers := pr.knownConsumers()
	list, _ := json.MarshalIndent(pr.raw, "", " ")
	got := make([][]prrender.Finding, len(names))
	errs := parallel(len(names), func(i int) error {
		ask, ok := prCriticAsk[names[i]]
		if !ok {
			return fmt.Errorf("no critic called %q", names[i])
		}
		prompt := ask + "\n" + prRules + prSchema + "\n\nThe findings so far:\n" + string(list)
		if names[i] == "consumers" && consumers != "" {
			prompt += "\n\n## Known consumers\n" + consumers
		}
		text, err := pr.fork("critics", names[i], "critics/"+names[i], prompt)
		if err != nil {
			return err
		}
		var out prFindings
		if err := extractJSON(text, &out); err != nil {
			return err
		}
		for k := range out.Findings {
			out.Findings[k].RaisedBy = "critic:" + names[i]
		}
		got[i] = out.Findings
		return nil
	})
	for i, e := range errs {
		if e != nil {
			if errors.Is(e, errStopped) {
				return e
			}
			pr.logf("critics %s failed %s", names[i], firstLine(e.Error()))
		}
	}
	var all []prrender.Finding
	n := 0
	for _, fs := range got {
		for _, f := range fs {
			n++
			f.ID = fmt.Sprintf("c%d", n)
			all = append(all, f)
		}
	}
	pr.raw = append(pr.raw, all...)
	return pr.writeJSON("steps/critics/out.json", prFindings{Findings: all})
}

// knownConsumers is the `## Known consumers` section of the panel's agent files
// (rule 27's list), for the consumers critic.
func (pr *prRun) knownConsumers() string {
	var out []string
	for _, a := range pr.review.Panel {
		body, err := pr.r.agentBody(a)
		if err != nil {
			continue
		}
		if i := strings.Index(body, "## Known consumers"); i >= 0 {
			sec := body[i+len("## Known consumers"):]
			if j := strings.Index(sec, "\n## "); j >= 0 {
				sec = sec[:j]
			}
			out = append(out, strings.TrimSpace(sec))
		}
	}
	return strings.Join(out, "\n")
}

// ---- 5 merge ----

const prMergeAsk = "You are the merge step. Below are every reviewer's and critic's findings, each with an id, and " +
	"the verifiers' verdicts. Dedupe findings about the same defect (keep the id of the one you build on), drop " +
	"what a verdict says does not hold, apply a verdict's severity, and NEVER drop a finding that carries a " +
	"\"leak\". Give every finding an \"impact\" (one sentence: what breaks for a user of this code and how " +
	"likely) and a \"rank\" across the WHOLE list, 1 being worst, unique, so that a worse finding has a smaller " +
	"number. Leaks rank by their impact, not last. Keep \"proven\" as the verdicts say. Return the final list " +
	"ONLY as one JSON object in this schema:\n"

func (pr *prRun) merge() error {
	if pr.review.Steps["merge"] != nil && pr.review.Steps["merge"].Done {
		var got prFindings
		if err := pr.readStep("merge/findings.json", &got); err == nil {
			pr.final = got.Findings
			return nil
		}
	}
	raw, _ := json.MarshalIndent(pr.raw, "", " ")
	verd, _ := json.MarshalIndent(pr.verd, "", " ")
	prompt := prMergeAsk + prSchema + "\n\nFindings:\n" + string(raw) + "\n\nVerdicts:\n" + string(verd)
	if len(pr.verd) == 0 {
		prompt += "\n(No verifier ran or answered. Keep every proven as \"no\".)"
	}
	text, err := pr.fork("merge", "merge", "merge", prompt)
	if err != nil {
		return err
	}
	var out prFindings
	if err := extractJSON(text, &out); err != nil {
		return fmt.Errorf("the merge answer: %w", err)
	}
	pr.final = pr.enforceProven(out.Findings)
	// The step's own out.json is the fork's receipt, written by call. The merged
	// list is its own file.
	return pr.writeJSON("steps/merge/findings.json", prFindings{Findings: pr.final})
}

// enforceProven is spec 5.6: a reviewer sets proven and verify must agree, so a
// `code` that no verdict confirms becomes `no`.
func (pr *prRun) enforceProven(fs []prrender.Finding) []prrender.Finding {
	ok := map[string]bool{}
	for _, v := range pr.verd {
		if (v.Verdict == "holds" || v.Verdict == "holds_at") && v.Proven == "code" {
			ok[v.ID] = true
		}
	}
	out := make([]prrender.Finding, len(fs))
	copy(out, fs)
	for i := range out {
		if out[i].Proven != "code" || !ok[out[i].ID] {
			out[i].Proven = "no"
		}
	}
	return out
}

// ---- 8 write ----

// resendPrompt asks the merge fork once to fix what the renderer refused.
func resendPrompt(final []prrender.Finding, rs []prrender.Resend) string {
	list, _ := json.MarshalIndent(final, "", " ")
	var b strings.Builder
	b.WriteString("The renderer refused some of your merged findings. Fix each one as asked, change nothing else, " +
		"and return the WHOLE corrected list as one JSON object in this schema:\n" + prSchema + "\n\n")
	for _, r := range rs {
		b.WriteString(r.Prompt + "\n")
	}
	b.WriteString("\nThe list you gave:\n" + string(list))
	return b.String()
}

// render is step 8: Go, not a model. A Resend is answered once by asking the
// merge fork again with the rule quoted, and a refusal after that fails `merge`.
func (pr *prRun) render() error {
	diff, err := os.ReadFile(filepath.Join(pr.dir, "pr.diff"))
	if err != nil {
		return err
	}
	in := prrender.Input{PRURL: pr.prURL(), Diff: string(diff), RunDir: pr.dir, Findings: pr.final, Raw: pr.raw}
	res, resends, err := prrender.Render(in)
	if err == nil && len(resends) > 0 {
		pr.logf("write: the renderer sent %d finding(s) back to merge", len(resends))
		text, ferr := pr.fork("merge", "merge-resend", "merge/resend", resendPrompt(pr.final, resends))
		if ferr != nil {
			return &stepErr{"merge", ferr}
		}
		var out prFindings
		if jerr := extractJSON(text, &out); jerr != nil {
			return &stepErr{"merge", fmt.Errorf("the resend answer: %w", jerr)}
		}
		pr.final = pr.enforceProven(out.Findings)
		// A retry replays the list the renderer accepted, not the one it refused.
		if werr := pr.writeJSON("steps/merge/findings.json", prFindings{Findings: pr.final}); werr != nil {
			return werr
		}
		in.Findings, in.Resent = pr.final, true
		res, resends, err = prrender.Render(in)
		if err == nil && len(resends) > 0 {
			err = &prrender.FailMergeError{Checks: resends[0].Checks}
		}
	}
	var fm *prrender.FailMergeError
	if errors.As(err, &fm) {
		return &stepErr{"merge", errors.New(strings.TrimPrefix(fm.Error(), "merge: "))}
	}
	if err != nil {
		return err
	}
	if pr.ctx.Err() != nil {
		return errStopped
	}
	if err := prrender.Write(pr.dir, res); err != nil {
		return err
	}
	return nil
}
