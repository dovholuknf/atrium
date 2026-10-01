package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	prrender "github.com/dovholuknf/atrium/internal/prreview/render"
	"github.com/dovholuknf/atrium/internal/store"
)

// The pulls runner with a fake gh, git and claude. Nothing here starts a process
// or touches the network.

const prTestHead = "ad5ddf4c0e7d1b2a9f3e4d5c6b7a8f9e0d1c2b3a"

type prFix struct {
	t      *testing.T
	st     *store.Store
	r      *prRunner
	id     string
	mu     sync.Mutex
	calls  []string // "gh pr view", "git fetch", ...
	forks  []forkSpec
	events []string
	// ghErr, when set, fails `gh pr view`.
	ghErr error
	// claude answers a call from its prompt. The default is a clean review.
	claude func(f *prFix, spec forkSpec, prompt string) ([]byte, error)
	// merges counts the merge-step calls.
	merges int
	final  []prrender.Finding
}

func prFindingsFixture(t *testing.T) []prrender.Finding {
	t.Helper()
	b, err := os.ReadFile("../prreview/render/testdata/findings.json")
	if err != nil {
		t.Fatal(err)
	}
	var fs []prrender.Finding
	if err := json.Unmarshal(b, &fs); err != nil {
		t.Fatal(err)
	}
	return fs[:3]
}

// prReceipt is `claude -p --output-format json` for one call.
func prReceipt(session, result string, write, read int64) []byte {
	b, _ := json.Marshal(map[string]any{
		"subtype": "success", "num_turns": 2, "session_id": session, "result": result,
		"permission_denials": []any{},
		"usage": map[string]any{"input_tokens": 3, "cache_creation_input_tokens": write,
			"cache_read_input_tokens": read, "output_tokens": 500},
	})
	return b
}

func jsonOf(v any) string { b, _ := json.Marshal(v); return string(b) }

var prIDRe = regexp.MustCompile(`"id": "([pc]\d+)"`)

func newPRFix(t *testing.T) *prFix {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.ToSlash(filepath.Join(dir, "atrium.db")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	st.DefaultReviewsRoot = filepath.Join(dir, "reviews")
	if err := st.SeedHarnesses(); err != nil {
		t.Fatal(err)
	}
	agents := filepath.Join(dir, "agents")
	os.MkdirAll(agents, 0o755)
	for _, a := range []string{"c-systems-reviewer", "go-security-reviewer", "functional-tester", "nonfunctional-tester"} {
		os.WriteFile(filepath.Join(agents, a+".md"), []byte("---\nname: "+a+"\n---\nYou review things as "+a+".\n"), 0o644)
	}
	f := &prFix{t: t, st: st}
	fixture := prFindingsFixture(t)
	f.claude = func(f *prFix, spec forkSpec, prompt string) ([]byte, error) {
		switch {
		case !containsArg(spec.Args, "--resume"):
			return prReceipt("prime-1", "ok", 42_000, 10_000), nil
		case strings.Contains(prompt, "You are the merge step"), strings.Contains(prompt, "The renderer refused"):
			f.mu.Lock()
			f.merges++
			f.mu.Unlock()
			out := make([]prrender.Finding, len(fixture))
			copy(out, fixture)
			for i := range out {
				out[i].ID = "p" + string(rune('1'+i))
			}
			return prReceipt("fork", jsonOf(map[string]any{"findings": out}), 2_000, 55_000), nil
		case strings.Contains(prompt, "Verify these findings"):
			var vs []prVerdict
			for _, m := range prIDRe.FindAllStringSubmatch(prompt, -1) {
				vs = append(vs, prVerdict{ID: m[1], Verdict: "holds", Sev: "med", Because: "read it", Proven: "code"})
			}
			return prReceipt("fork", jsonOf(map[string]any{"verdicts": vs}), 1_000, 55_000), nil
		case strings.Contains(prompt, "critic"):
			return prReceipt("fork", `{"findings": []}`, 1_000, 55_000), nil
		default:
			// A reviewer. Only the C reviewer finds anything, so ids are stable.
			if strings.Contains(prompt, "You are c-systems-reviewer.") {
				return prReceipt("fork", jsonOf(map[string]any{"findings": fixture}), 3_000, 55_000), nil
			}
			return prReceipt("fork", `{"findings": []}`, 3_000, 55_000), nil
		}
	}
	diff, err := os.ReadFile("../prreview/render/testdata/run/pr.diff")
	if err != nil {
		t.Fatal(err)
	}
	r := newPRRunner(st, func(id string) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if p, err := st.PRByID(id); err == nil {
			f.events = append(f.events, p.State+"/"+p.RunState)
		}
	})
	r.agentsDir = func() string { return agents }
	r.userSettings = func() []byte { return nil }
	r.baseEnv = func() []string { return []string{"PATH=/bin", "ATRIUM_PERM_GATE=on", "ATRIUM_TASK_ID=x"} }
	r.run = func(ctx context.Context, c prCmd) ([]byte, error) {
		f.mu.Lock()
		f.calls = append(f.calls, c.Name+" "+strings.Join(c.Args, " "))
		f.mu.Unlock()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		switch {
		case c.Name == "gh" && c.Args[1] == "view":
			if f.ghErr != nil {
				return nil, f.ghErr
			}
			return []byte(jsonOf(map[string]any{"title": "tls engine: session resumption", "headRefOid": prTestHead,
				"baseRefName": "main", "author": map[string]any{"login": "ekoby"},
				"reviewRequests": []any{},
				"files": []any{map[string]any{"path": "src/tls_engine.c", "additions": 4, "deletions": 0},
					map[string]any{"path": "src/http.c", "additions": 3, "deletions": 0}}})), nil
		case c.Name == "gh" && c.Args[1] == "diff":
			return diff, nil
		case c.Name == "git" && c.Args[0] == "checkout":
			os.MkdirAll(filepath.Join(c.Dir, "src"), 0o755)
			os.WriteFile(filepath.Join(c.Dir, "src", "tls_engine.c"), []byte("int engine_start(void) {}\n"), 0o644)
			os.WriteFile(filepath.Join(c.Dir, "src", "http.c"), []byte("int http_open(void) {}\n"), 0o644)
			// A PR can carry its own claude settings, with hooks.
			os.MkdirAll(filepath.Join(c.Dir, ".claude"), 0o755)
			os.WriteFile(filepath.Join(c.Dir, ".claude", "settings.json"),
				[]byte(`{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"touch pwned"}]}]}}`), 0o644)
			os.MkdirAll(filepath.Join(c.Dir, "test"), 0o755)
			os.WriteFile(filepath.Join(c.Dir, "test", "engine_test.c"), []byte("x\n"), 0o644)
		case c.Name == "git" && c.Args[0] == "rev-parse":
			return []byte(prTestHead + "\n"), nil
		}
		return nil, nil
	}
	r.fork = func(ctx context.Context, spec forkSpec) ([]byte, error) {
		f.mu.Lock()
		f.forks = append(f.forks, spec)
		f.mu.Unlock()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return f.claude(f, spec, string(spec.Stdin))
	}
	f.r = r
	row, _, err := st.CreatePR(store.NewPR{URL: "https://github.com/openziti/tlsuv/pull/378", Host: "github.com",
		Org: "openziti", Repo: "tlsuv", Number: 378,
		RunDir: mustPath(t, filepath.ToSlash(st.DefaultReviewsRoot))})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.RunFolderOn("github.com", "openziti", "tlsuv", 378, ""); err != nil {
		t.Fatal(err)
	}
	f.id = row.ID
	return f
}

func mustPath(t *testing.T, root string) string {
	p, err := store.RunFolderPath(root, "github.com", "openziti", "tlsuv", 378, "")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func containsArg(args []string, a string) bool {
	for _, x := range args {
		if x == a {
			return true
		}
	}
	return false
}

func (f *prFix) run(t *testing.T) *store.PRReview {
	t.Helper()
	f.r.Start(f.id)
	done := make(chan struct{})
	go func() { f.r.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the run did not end")
	}
	p, err := f.st.PRByID(f.id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func readReview(t *testing.T, p *store.PRReview) prReviewFile {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(filepath.FromSlash(p.RunDir), "review.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rv prReviewFile
	if err := json.Unmarshal(b, &rv); err != nil {
		t.Fatal(err)
	}
	return rv
}

func TestPRRunnerHappyRun(t *testing.T) {
	f := newPRFix(t)
	before, _ := f.st.List()
	p := f.run(t)
	if p.State != store.PRReady || p.RunError != "" {
		t.Fatalf("state %s %q", p.State, p.RunError)
	}
	if p.Title != "tls engine: session resumption" || p.Author != "ekoby" || p.Head != prTestHead {
		t.Fatalf("the row was not filled in: %+v", p)
	}
	// The folder was named `pending` and the fetch moved it, and the row says so.
	if !strings.HasSuffix(p.RunDir, "/pr-378-ad5ddf4") {
		t.Fatalf("run_dir %s", p.RunDir)
	}
	dir := filepath.FromSlash(p.RunDir)
	for _, name := range []string{"pr.json", "pr.diff", "bundle.md", "walk.txt", "run.log", "review.json",
		"steps/prime/prompt.md", "steps/merge/prompt.md", "steps/merge/out.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("missing %s", name)
		}
	}
	files, _ := filepath.Glob(filepath.Join(dir, "findings", "*.txt"))
	if len(files) != 3 {
		t.Fatalf("finding files: %v", files)
	}
	walk, _ := os.ReadFile(filepath.Join(dir, "walk.txt"))
	if strings.Count(string(walk), "  open") != 3 {
		t.Fatalf("walk.txt:\n%s", walk)
	}
	bundle, _ := os.ReadFile(filepath.Join(dir, "bundle.md"))
	if !strings.Contains(string(bundle), "int engine_start(void) {}") || !strings.Contains(string(bundle), "+    free(sess);") {
		t.Fatalf("bundle lacks the diff or a changed file:\n%s", bundle)
	}
	// The prime keeps its session and the forks do not, and each fork resumes the prime.
	var primes, forks int
	for _, s := range f.forks {
		if !containsArg(s.Args, "--resume") {
			primes++
			if containsArg(s.Args, "--no-session-persistence") {
				t.Errorf("the prime must keep its session: %v", s.Args)
			}
			continue
		}
		forks++
		for _, want := range []string{"--fork-session", "--no-session-persistence", "--max-turns", "prime-1", "--tools"} {
			if !containsArg(s.Args, want) {
				t.Errorf("a fork lacks %s: %v", want, s.Args)
			}
		}
		for _, kv := range s.Env {
			if kv == "ATRIUM_PERM_GATE=on" || kv == "ATRIUM_PERM_GATE=" {
				t.Errorf("a fork must not wait on the gate: %s", kv)
			}
		}
		if !containsArg(s.Env, "ATRIUM_PERM_GATE=off") {
			t.Errorf("a fork's gate is not off")
		}
	}
	if primes != 1 || forks < 5 {
		t.Fatalf("primes %d forks %d", primes, forks)
	}
	rv := readReview(t, p)
	if rv.Cache.PrimeWrite != 42_000 || rv.Cache.ForkReads < 55_000*5 || rv.CostUSD <= 0 || rv.Findings != 3 ||
		rv.Head != prTestHead || rv.Denials != 0 || rv.PrimeSession != "prime-1" {
		t.Fatalf("review.json: %+v", rv)
	}
	if p.CostUSD < rv.CostUSD-0.0001 || p.CostUSD > rv.CostUSD+0.0001 {
		t.Fatalf("the row's cost %.4f is not review.json's %.4f", p.CostUSD, rv.CostUSD)
	}
	for _, step := range prSteps {
		if rv.Steps[step] == nil || !rv.Steps[step].Done {
			t.Errorf("step %s is not recorded done", step)
		}
	}
	log, _ := os.ReadFile(filepath.Join(dir, "run.log"))
	for _, want := range []string{"fetch start", "fetch end", "prime end", "panel end", "merge end", "write end", "ready"} {
		if !strings.Contains(string(log), want) {
			t.Errorf("run.log lacks %q:\n%s", want, log)
		}
	}
	// No card is ever made.
	after, _ := f.st.List()
	if len(after) != len(before) {
		t.Fatalf("a card was made: %d -> %d", len(before), len(after))
	}
	// The gh and git commands are the named ones.
	joined := strings.Join(f.calls, "\n")
	for _, want := range []string{"gh pr view 378 --repo openziti/tlsuv", "gh pr diff 378", "git fetch -q --depth 1 --filter=blob:none origin pull/378/head"} {
		if !strings.Contains(joined, want) {
			t.Errorf("no %q in:\n%s", want, joined)
		}
	}
}

func TestPRRunnerProvenOnlyWhenVerified(t *testing.T) {
	f := newPRFix(t)
	p := f.run(t)
	dir := filepath.FromSlash(p.RunDir)
	files, _ := filepath.Glob(filepath.Join(dir, "findings", "*.txt"))
	for _, fl := range files {
		b, _ := os.ReadFile(fl)
		hasFix := strings.Contains(string(b), "Suggested fix:")
		code := strings.Contains(string(b), "Move the free")
		if hasFix != code {
			t.Errorf("%s: Suggested fix %v, proven code %v\n%s", filepath.Base(fl), hasFix, code, b)
		}
	}
	// With no verdicts, nothing is proven.
	g := newPRFix(t)
	if _, err := g.st.SavePRRecipe(store.PRRecipe{Name: "default", Harness: "claude", Panel: store.DefaultPRPanel,
		VerifyAt: "none", Critics: "[]", WalkerBrief: "x", BudgetUSD: 3, TurnsCap: 12}); err != nil {
		t.Fatal(err)
	}
	p = g.run(t)
	if p.State != store.PRReady {
		t.Fatalf("%s %s", p.State, p.RunError)
	}
	files, _ = filepath.Glob(filepath.Join(filepath.FromSlash(p.RunDir), "findings", "*.txt"))
	for _, fl := range files {
		if b, _ := os.ReadFile(fl); strings.Contains(string(b), "Suggested fix:") {
			t.Errorf("an unverified finding carries Suggested fix:\n%s", b)
		}
	}
}

func TestPRRunnerFetchFailure(t *testing.T) {
	f := newPRFix(t)
	f.ghErr = errors.New("gh pr view: GraphQL: Could not resolve to a PullRequest")
	p := f.run(t)
	if p.State != store.PRFailed || p.RunError != "fetch: gh pr view: GraphQL: Could not resolve to a PullRequest" {
		t.Fatalf("%s %q", p.State, p.RunError)
	}
	if len(f.forks) != 0 {
		t.Fatal("a fork ran after the fetch failed")
	}
	log, _ := os.ReadFile(filepath.Join(filepath.FromSlash(p.RunDir), "run.log"))
	if !strings.Contains(string(log), "fetch error") {
		t.Fatalf("run.log:\n%s", log)
	}
	// A retry runs again from the start.
	f.ghErr = nil
	if _, err := f.st.ResetPR(f.id, p.RunDir, store.PRFailed); err != nil {
		t.Fatal(err)
	}
	if got := f.run(t); got.State != store.PRReady {
		t.Fatalf("retry: %s %q", got.State, got.RunError)
	}
}

func TestPRRunnerBudgetStopsAndRetryRaisesOnce(t *testing.T) {
	f := newPRFix(t)
	// The prime costs about $0.17 and a panel fork about $0.03: a $0.10 budget is
	// spent by the prime and a $0.20 one by the panel.
	if _, err := f.st.SavePRRecipe(store.PRRecipe{Name: "default", Harness: "claude", Panel: store.DefaultPRPanel,
		VerifyAt: "med", Critics: "[]", WalkerBrief: "x", BudgetUSD: 0.10, TurnsCap: 12}); err != nil {
		t.Fatal(err)
	}
	p := f.run(t)
	if p.State != store.PRFailed || !strings.HasPrefix(p.RunError, "budget: ") {
		t.Fatalf("%s %q", p.State, p.RunError)
	}
	primesBefore := 0
	for _, s := range f.forks {
		if !containsArg(s.Args, "--resume") {
			primesBefore++
		}
		if containsArg(s.Args, "--resume") {
			t.Fatal("a fork started after the budget was spent")
		}
	}
	if primesBefore != 1 {
		t.Fatalf("primes %d", primesBefore)
	}
	if rv := readReview(t, p); !rv.BudgetFailed || rv.BudgetRaised {
		t.Fatalf("review.json: %+v", rv)
	}
	// The retry keeps the folder and the prime, raises the cap once, and stops again.
	if _, err := f.st.ResetPR(f.id, p.RunDir, store.PRFailed); err != nil {
		t.Fatal(err)
	}
	p = f.run(t)
	if p.State != store.PRFailed || !strings.HasPrefix(p.RunError, "budget: ") {
		t.Fatalf("retry: %s %q", p.State, p.RunError)
	}
	primes, forks := 0, 0
	for _, s := range f.forks {
		if containsArg(s.Args, "--resume") {
			forks++
		} else {
			primes++
		}
	}
	if primes != 1 || forks == 0 {
		t.Fatalf("the retry should reuse the prime and run the panel: primes %d forks %d", primes, forks)
	}
	rv := readReview(t, p)
	if !rv.BudgetRaised || p.CostUSD < 0.17 {
		t.Fatalf("review.json %+v cost %.3f", rv, p.CostUSD)
	}
}

func TestPRRunnerAbortMidStepLeavesAborted(t *testing.T) {
	f := newPRFix(t)
	inFork := make(chan struct{}, 8)
	f.r.fork = func(ctx context.Context, spec forkSpec) ([]byte, error) {
		if !containsArg(spec.Args, "--resume") {
			return prReceipt("prime-1", "ok", 42_000, 10_000), nil
		}
		inFork <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	f.r.Start(f.id)
	select {
	case <-inFork:
	case <-time.After(20 * time.Second):
		t.Fatal("no fork started")
	}
	// What the API does: mark aborted first, then tell the runner.
	if _, err := f.st.MovePR(f.id, []string{store.PRQueued, store.PRFetching, store.PRRunning},
		store.PRAborted, "", ""); err != nil {
		t.Fatal(err)
	}
	f.r.Abort(f.id)
	f.r.Wait()
	p, _ := f.st.PRByID(f.id)
	if p.State != store.PRAborted || p.RunError != "" {
		t.Fatalf("%s %q", p.State, p.RunError)
	}
	// The API deletes the folder, and nothing the run does after rebuilds it.
	dir := filepath.FromSlash(p.RunDir)
	os.RemoveAll(dir)
	time.Sleep(50 * time.Millisecond)
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("the folder came back after an abort")
	}
}

func TestPRRunnerResendRound(t *testing.T) {
	f := newPRFix(t)
	fixture := prFindingsFixture(t)
	base := f.claude
	f.claude = func(f *prFix, spec forkSpec, prompt string) ([]byte, error) {
		if strings.Contains(prompt, "You are the merge step") {
			// Finding 1's fix is two changes, which rule 36 refuses.
			out := make([]prrender.Finding, len(fixture))
			copy(out, fixture)
			for i := range out {
				out[i].ID = "p" + string(rune('1'+i))
			}
			out[0].Fix = "Check `sess->closing` first, or free the session earlier."
			f.mu.Lock()
			f.merges++
			f.mu.Unlock()
			return prReceipt("fork", jsonOf(map[string]any{"findings": out}), 2_000, 55_000), nil
		}
		return base(f, spec, prompt)
	}
	p := f.run(t)
	if p.State != store.PRReady {
		t.Fatalf("%s %q", p.State, p.RunError)
	}
	if f.merges != 2 {
		t.Fatalf("merge forks: %d, want the merge and one resend", f.merges)
	}
	var resend string
	for _, s := range f.forks {
		if strings.Contains(string(s.Stdin), "The renderer refused") {
			resend = string(s.Stdin)
		}
	}
	if !strings.Contains(resend, "rule 36") || !strings.Contains(resend, "never \"X, or Y\"") {
		t.Fatalf("the resend does not quote the rule:\n%s", resend)
	}
	if _, err := os.Stat(filepath.Join(filepath.FromSlash(p.RunDir), "steps", "merge", "resend", "prompt.md")); err != nil {
		t.Fatal("the resend's prompt was not kept")
	}
}

func TestPRRunnerFailsMergeAfterOneResend(t *testing.T) {
	f := newPRFix(t)
	fixture := prFindingsFixture(t)
	base := f.claude
	f.claude = func(f *prFix, spec forkSpec, prompt string) ([]byte, error) {
		if strings.Contains(prompt, "You are the merge step") || strings.Contains(prompt, "The renderer refused") {
			// A MED with no exposure, in both answers: rule 26 is hard.
			out := make([]prrender.Finding, len(fixture))
			copy(out, fixture)
			for i := range out {
				out[i].ID = "p" + string(rune('1'+i))
			}
			out[0].Exposure = prrender.Exposure{}
			return prReceipt("fork", jsonOf(map[string]any{"findings": out}), 2_000, 55_000), nil
		}
		return base(f, spec, prompt)
	}
	p := f.run(t)
	if p.State != store.PRFailed || !strings.HasPrefix(p.RunError, "merge: ") {
		t.Fatalf("%s %q", p.State, p.RunError)
	}
}

// A PR's checked-in .claude/settings.json must never be read by the prime or a fork.
func TestPRRunnerNeverRunsInThePRsCheckout(t *testing.T) {
	f := newPRFix(t)
	p := f.run(t)
	if p.State != store.PRReady {
		t.Fatalf("%s %q", p.State, p.RunError)
	}
	dir, _ := filepath.Abs(filepath.FromSlash(p.RunDir))
	src := filepath.Join(dir, "src")
	if _, err := os.Stat(filepath.Join(src, ".claude", "settings.json")); err != nil {
		t.Fatal("the fake checkout did not carry settings, so this proves nothing")
	}
	if len(f.forks) < 2 {
		t.Fatalf("forks %d", len(f.forks))
	}
	for _, s := range f.forks {
		abs, _ := filepath.Abs(s.Dir)
		if abs == src || strings.HasPrefix(abs, src+string(filepath.Separator)) {
			t.Errorf("a call runs inside the PR's checkout: %s", s.Dir)
		}
		if _, err := os.Stat(filepath.Join(s.Dir, ".claude")); err == nil {
			t.Errorf("the working folder holds a .claude: %s", s.Dir)
		}
		for i, a := range s.Args {
			if a == "--setting-sources" {
				if v := s.Args[i+1]; strings.Contains(v, "project") || strings.Contains(v, "local") {
					t.Errorf("setting sources %q let the checkout speak", v)
				}
			}
		}
		at := -1
		for i, a := range s.Args {
			if a == "--add-dir" {
				at = i
			}
		}
		if at < 0 || s.Args[at+1] != src || !strings.HasPrefix(s.Args[at+2], "-") {
			t.Errorf("src/ is not reached by --add-dir: %v", s.Args)
		}
	}
	if _, err := os.Stat(filepath.Join(src, "pwned")); err == nil {
		t.Fatal("a hook ran")
	}
}

// A retry after merge finished replays the merged list, not the claude receipt, so the
// review is not ready with no findings.
func TestPRRunnerRetryAfterMergeKeepsTheFindings(t *testing.T) {
	f := newPRFix(t)
	p := f.run(t)
	if p.State != store.PRReady {
		t.Fatalf("%s %q", p.State, p.RunError)
	}
	dir := filepath.FromSlash(p.RunDir)
	merges := f.merges
	// A failed write: the findings are gone and the row is failed, then retried.
	os.RemoveAll(filepath.Join(dir, "findings"))
	os.Remove(filepath.Join(dir, "walk.txt"))
	if _, err := f.st.MovePR(f.id, nil, store.PRFailed, "", "write: disk full"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.ResetPR(f.id, p.RunDir, store.PRFailed); err != nil {
		t.Fatal(err)
	}
	p = f.run(t)
	if p.State != store.PRReady {
		t.Fatalf("%s %q", p.State, p.RunError)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "findings", "*.txt"))
	if len(files) != 3 {
		t.Fatalf("the retry wrote %d findings, want 3", len(files))
	}
	if f.merges != merges {
		t.Fatal("the retry asked merge again")
	}
	if rv := readReview(t, p); rv.Findings != 3 || len(rv.Panel) != 3 {
		t.Fatalf("review.json findings %d panel %v", rv.Findings, rv.Panel)
	}
}

// After a Resend the retry replays the list the renderer accepted.
func TestPRRunnerReplaysTheResentListAfterARetry(t *testing.T) {
	f := newPRFix(t)
	fixture := prFindingsFixture(t)
	base := f.claude
	f.claude = func(f *prFix, spec forkSpec, prompt string) ([]byte, error) {
		if strings.Contains(prompt, "You are the merge step") {
			out := make([]prrender.Finding, len(fixture))
			copy(out, fixture)
			for i := range out {
				out[i].ID = "p" + string(rune('1'+i))
			}
			out[0].Fix = "Check `sess->closing` first, or free the session earlier."
			return prReceipt("fork", jsonOf(map[string]any{"findings": out}), 2_000, 55_000), nil
		}
		return base(f, spec, prompt)
	}
	p := f.run(t)
	if p.State != store.PRReady {
		t.Fatalf("%s %q", p.State, p.RunError)
	}
	b, _ := os.ReadFile(filepath.Join(filepath.FromSlash(p.RunDir), "steps", "merge", "findings.json"))
	if strings.Contains(string(b), "or free the session earlier") {
		t.Fatalf("findings.json is the list the renderer refused:\n%s", b)
	}
}
