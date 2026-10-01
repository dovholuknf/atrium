package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// The pulls index: one row per PR review run, and the recipe a run follows.
//
// A row is an INDEX over a run folder and never holds a finding. The folder is
// the source of truth for findings and the walk, because three parties write
// them and a copy here would drift the first time a walker renamed a file while
// the daemon was down. What the folder cannot hold is the run's own progress and
// what GitHub says now, and that is this table's part.
//
// Everything on a row but `why` is OBSERVED: the runner and the fetch step write
// it and a person never types it. See docs/rnd/pulls-view-design.md and
// docs/rnd/pulls-api.md.

// SettingReviewsRoot is where run folders are made. Empty means the default
// under the daemon's data directory.
const SettingReviewsRoot = "reviews_root"

// The states of a row, in the order a run goes through them.
const (
	PRQueued   = "queued"
	PRFetching = "fetching"
	PRRunning  = "running"
	PRReady    = "ready"
	PRFailed   = "failed"
	PRAborted  = "aborted"
)

// PRStates lists every state, for a caller that has to zero a count for each.
var PRStates = []string{PRQueued, PRFetching, PRRunning, PRReady, PRFailed, PRAborted}

// MaxPRWhy bounds the reason a caller gives for a review.
const MaxPRWhy = 2000

// PRSecond is the second opinion's state on a row.
type PRSecond struct {
	State   string `json:"state"`
	Summary string `json:"summary"`
	Error   string `json:"error"`
}

// PRReview is one row of the pulls index.
type PRReview struct {
	ID        string   `json:"id"`
	URL       string   `json:"url"`
	Host      string   `json:"host"`
	Org       string   `json:"org"`
	Repo      string   `json:"repo"`
	OrgRepo   string   `json:"org_repo"`
	Number    int      `json:"number"`
	Title     string   `json:"title"`
	Why       string   `json:"why"`
	Head      string   `json:"head"`
	Head7     string   `json:"head7"`
	State     string   `json:"state"`
	RunState  string   `json:"run_state"`
	RunError  string   `json:"run_error"`
	CostUSD   float64  `json:"cost_usd"`
	StartedAt string   `json:"started_at"`
	ReadyAt   string   `json:"ready_at"`
	CreatedAt string   `json:"created_at"`
	Archived  string   `json:"archived_at"`
	RunDir    string   `json:"run_dir"`
	Second    PRSecond `json:"second"`
	Author    string   `json:"author"`
	// WalkerTask is the card walking it. An override: only a person or the walker
	// launch ever writes it.
	WalkerTask string `json:"walker_task"`
}

const prColumns = `id, run_dir, url, why, host, org, repo, number, reviewed_head, observed_title,
	observed_author, walker_task, archived_at, state, run_state, run_error, cost_usd,
	started_at, ready_at, second_state, second_summary, second_error, created_at`

func scanPR(sc interface{ Scan(...any) error }) (*PRReview, error) {
	var p PRReview
	if err := sc.Scan(&p.ID, &p.RunDir, &p.URL, &p.Why, &p.Host, &p.Org, &p.Repo, &p.Number,
		&p.Head, &p.Title, &p.Author, &p.WalkerTask, &p.Archived, &p.State, &p.RunState,
		&p.RunError, &p.CostUSD, &p.StartedAt, &p.ReadyAt, &p.Second.State,
		&p.Second.Summary, &p.Second.Error, &p.CreatedAt); err != nil {
		return nil, err
	}
	p.OrgRepo = p.Org + "/" + p.Repo
	if len(p.Head) >= 7 {
		p.Head7 = p.Head[:7]
	}
	return &p, nil
}

// NewPR is what a caller knows when a review is asked for.
type NewPR struct {
	URL    string
	Why    string
	Host   string
	Org    string
	Repo   string
	Number int
	// Head is the full SHA when the caller knows it, else empty and the fetch
	// step fills it in.
	Head   string
	RunDir string
}

var hexHead = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)

// ValidPRHead reports whether s can name a head: empty, or 7 to 40 hex digits.
func ValidPRHead(s string) bool { return s == "" || hexHead.MatchString(s) }

// CreatePR makes a row, or returns the one already there for that run folder.
//
// The run folder is the identity: the same PR at the same head is the same
// review, so a second ask finds the first. created says which happened.
func (st *Store) CreatePR(in NewPR) (*PRReview, bool, error) {
	if strings.TrimSpace(in.Org) == "" || strings.TrimSpace(in.Repo) == "" || in.Number <= 0 {
		return nil, false, errors.New("a review needs an org, a repo and a number")
	}
	if strings.TrimSpace(in.RunDir) == "" {
		return nil, false, errors.New("a review needs a run folder")
	}
	if len(in.Why) > MaxPRWhy {
		return nil, false, fmt.Errorf("why is over %d characters", MaxPRWhy)
	}
	if !ValidPRHead(in.Head) {
		return nil, false, errors.New("head is not a commit hash")
	}
	var out *PRReview
	created := false
	err := st.guard(func() error {
		row := st.db.QueryRow(`SELECT `+prColumns+` FROM pr_review WHERE run_dir = ?`, in.RunDir)
		if got, err := scanPR(row); err == nil {
			out = got
			return nil
		} else if err != sql.ErrNoRows {
			return err
		}
		id := newID()
		_, err := st.db.Exec(`INSERT INTO pr_review
			(id, run_dir, url, why, host, org, repo, number, reviewed_head, created_at)
			VALUES (?,?,?,?,?,?,?,?,?,?)`,
			id, in.RunDir, strings.TrimSpace(in.URL), in.Why, in.Host, in.Org, in.Repo,
			in.Number, strings.ToLower(in.Head), ts(now()))
		if err != nil {
			return err
		}
		got, err := scanPR(st.db.QueryRow(`SELECT `+prColumns+` FROM pr_review WHERE id = ?`, id))
		if err != nil {
			return err
		}
		out, created = got, true
		return nil
	})
	return out, created, err
}

// PRByID returns one row. A missing row is sql.ErrNoRows.
func (st *Store) PRByID(id string) (*PRReview, error) {
	var out *PRReview
	err := st.guard(func() error {
		got, err := scanPR(st.db.QueryRow(`SELECT `+prColumns+` FROM pr_review WHERE id = ?`, id))
		if err != nil {
			return err
		}
		out = got
		return nil
	})
	return out, err
}

// PRFilter narrows the index. The zero value is every row that is not archived.
type PRFilter struct {
	States   []string
	OrgRepo  string
	Archived bool
}

// PRs lists rows newest first.
func (st *Store) PRs(f PRFilter) ([]*PRReview, error) {
	var out []*PRReview
	err := st.guard(func() error {
		out = nil
		q := `SELECT ` + prColumns + ` FROM pr_review WHERE 1 = 1`
		var args []any
		if !f.Archived {
			q += ` AND archived_at = ''`
		}
		if len(f.States) > 0 {
			q += ` AND state IN (?` + strings.Repeat(`,?`, len(f.States)-1) + `)`
			for _, s := range f.States {
				args = append(args, s)
			}
		}
		if f.OrgRepo != "" {
			q += ` AND org || '/' || repo = ?`
			args = append(args, f.OrgRepo)
		}
		q += ` ORDER BY created_at DESC, id DESC`
		rows, err := st.db.Query(q, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			p, err := scanPR(rows)
			if err != nil {
				return err
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, err
}

// PRCounts counts the rows that are not archived, one key per state.
func (st *Store) PRCounts() (map[string]int, error) {
	out := map[string]int{}
	for _, s := range PRStates {
		out[s] = 0
	}
	err := st.guard(func() error {
		rows, err := st.db.Query(`SELECT state, COUNT(*) FROM pr_review
			WHERE archived_at = '' GROUP BY state`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var s string
			var n int
			if err := rows.Scan(&s, &n); err != nil {
				return err
			}
			out[s] = n
		}
		return rows.Err()
	})
	return out, err
}

func validPRState(s string) bool {
	for _, v := range PRStates {
		if v == s {
			return true
		}
	}
	return false
}

// SetPRState moves a row. started_at is stamped the first time a run begins and
// ready_at when it becomes ready. runErr belongs to failed and is cleared by
// every other state.
func (st *Store) SetPRState(id, state, runState, runErr string) (*PRReview, error) {
	if !validPRState(state) {
		return nil, fmt.Errorf("%q is not a review state", state)
	}
	if state != PRFailed {
		runErr = ""
	}
	n := ts(now())
	err := st.guard(func() error {
		res, err := st.db.Exec(`UPDATE pr_review SET state = ?, run_state = ?, run_error = ?,
			started_at = CASE WHEN started_at = '' AND ? IN ('fetching', 'running') THEN ? ELSE started_at END,
			ready_at   = CASE WHEN ? = 'ready' THEN ? ELSE '' END
			WHERE id = ?`,
			state, runState, runErr, state, n, state, n, id)
		if err != nil {
			return err
		}
		if k, _ := res.RowsAffected(); k == 0 {
			return sql.ErrNoRows
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return st.PRByID(id)
}

// AddPRCost adds one step's cost to the run's total.
func (st *Store) AddPRCost(id string, usd float64) (*PRReview, error) {
	err := st.guard(func() error {
		res, err := st.db.Exec(`UPDATE pr_review SET cost_usd = cost_usd + ? WHERE id = ?`, usd, id)
		if err != nil {
			return err
		}
		if k, _ := res.RowsAffected(); k == 0 {
			return sql.ErrNoRows
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return st.PRByID(id)
}

// SetPRFetched records what the fetch step learned: the head the review is of,
// and what GitHub says the PR is called and who wrote it. runDir is the folder
// named for that head, which the caller has already moved the run into.
func (st *Store) SetPRFetched(id, head, title, author, runDir string) (*PRReview, error) {
	if !ValidPRHead(head) {
		return nil, errors.New("head is not a commit hash")
	}
	err := st.guard(func() error {
		res, err := st.db.Exec(`UPDATE pr_review SET reviewed_head = ?, observed_head = ?,
			observed_title = ?, observed_author = ?, checked_at = ?,
			run_dir = CASE WHEN ? = '' THEN run_dir ELSE ? END WHERE id = ?`,
			strings.ToLower(head), strings.ToLower(head), title, author, ts(now()),
			runDir, runDir, id)
		if err != nil {
			return err
		}
		if k, _ := res.RowsAffected(); k == 0 {
			return sql.ErrNoRows
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return st.PRByID(id)
}

// ResetPR puts a row back to the start for a rerun in runDir: queued, nothing
// observed about the old run left on it. The head, title and author stay, since
// they say what the review is of.
func (st *Store) ResetPR(id, runDir string) (*PRReview, error) {
	err := st.guard(func() error {
		res, err := st.db.Exec(`UPDATE pr_review SET state = 'queued', run_state = '',
			run_error = '', cost_usd = 0, started_at = '', ready_at = '',
			second_state = 'none', second_summary = '', second_error = '',
			run_dir = CASE WHEN ? = '' THEN run_dir ELSE ? END WHERE id = ?`,
			runDir, runDir, id)
		if err != nil {
			return err
		}
		if k, _ := res.RowsAffected(); k == 0 {
			return sql.ErrNoRows
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return st.PRByID(id)
}

// SetPRSecond records the second opinion's progress.
func (st *Store) SetPRSecond(id, state, summary, errText string) (*PRReview, error) {
	switch state {
	case "none", "pending", "done", "failed":
	default:
		return nil, fmt.Errorf("%q is not a second opinion state", state)
	}
	err := st.guard(func() error {
		res, err := st.db.Exec(`UPDATE pr_review SET second_state = ?, second_summary = ?,
			second_error = ? WHERE id = ?`, state, summary, errText, id)
		if err != nil {
			return err
		}
		if k, _ := res.RowsAffected(); k == 0 {
			return sql.ErrNoRows
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return st.PRByID(id)
}

// SetPRWalker records the card walking a review, or clears it with "".
func (st *Store) SetPRWalker(id, task string) (*PRReview, error) {
	err := st.guard(func() error {
		res, err := st.db.Exec(`UPDATE pr_review SET walker_task = ? WHERE id = ?`, task, id)
		if err != nil {
			return err
		}
		if k, _ := res.RowsAffected(); k == 0 {
			return sql.ErrNoRows
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return st.PRByID(id)
}

// ---- the reviews root and the run folder ----

// ReviewsRoot is where run folders are made: the setting, else the default under
// the daemon's data directory.
func (st *Store) ReviewsRoot() string {
	if v, err := st.Setting(SettingReviewsRoot); err == nil && strings.TrimSpace(v) != "" {
		return filepath.ToSlash(strings.TrimSpace(v))
	}
	return filepath.ToSlash(st.DefaultReviewsRoot)
}

// RunFolder makes the run folder for a GitHub PR at a head, and returns its
// path with forward slashes. Idempotent.
func (st *Store) RunFolder(org, repo string, n int, head string) (string, error) {
	return st.RunFolderOn("github.com", org, repo, n, head)
}

// RunFolderOn is RunFolder for any host. The folder is
//
//	<root>/<host's first label>-<org>-<repo>/pr-<n>-<head7>/
//
// with steps/ and findings/ made inside it. src/ is made by the fetch step,
// since a checkout wants to create its own directory. An unknown head is named
// `pending`, and the fetch step moves the run into the right name.
func (st *Store) RunFolderOn(host, org, repo string, n int, head string) (string, error) {
	dir, err := RunFolderPath(st.ReviewsRoot(), host, org, repo, n, head)
	if err != nil {
		return "", err
	}
	for _, sub := range []string{"steps", "findings"} {
		if err := os.MkdirAll(filepath.Join(filepath.FromSlash(dir), sub), 0o755); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// RunFolderPath names a run folder without making it. Pure, so the names can be
// tested and a caller can ask where a run would live.
func RunFolderPath(root, host, org, repo string, n int, head string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", errors.New("no reviews root")
	}
	if n <= 0 {
		return "", errors.New("a run folder needs a pull request number")
	}
	if !ValidPRHead(head) {
		return "", errors.New("head is not a commit hash")
	}
	short := "pending"
	if head != "" {
		short = strings.ToLower(head)
		if len(short) > 7 {
			short = short[:7]
		}
	}
	h := host
	if i := strings.IndexByte(h, '.'); i > 0 {
		h = h[:i]
	}
	top := folderSeg(h) + "-" + folderSeg(org) + "-" + folderSeg(repo)
	return path.Join(filepath.ToSlash(root), top, fmt.Sprintf("pr-%d-%s", n, short)), nil
}

// folderSeg makes one part of a folder name safe: letters, digits, dot, dash and
// underscore, and never a name made of dots.
func folderSeg(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.TrimSpace(b.String())
	if strings.Trim(out, ".") == "" {
		return "_"
	}
	return out
}

// ---- the recipe ----

// DefaultPRPanel is the panel the review-panel skill picks today, as the recipe's
// JSON. Spliced into the seed migration.
const DefaultPRPanel = `[{"agent":"c-systems-reviewer","when":["*.c","*.h"]},` +
	`{"agent":"go-security-reviewer","when":["*.go"]},` +
	`{"agent":"functional-tester","when":["*"]},` +
	`{"agent":"nonfunctional-tester","when":["*"]}]`

// DefaultWalkerBrief is the seeded walker brief. No quote marks, because it is
// spliced into a SQL literal. The operator edits it in settings.
const DefaultWalkerBrief = `Walk this pull request review with clint, one finding at a time, in the order of ` +
	`walk.txt. Show the severity, file and line header, the code line with its link, and the bullets. ` +
	`Do not show a file name or an item N of M. Write done, skipped and deferred to walk.txt as clint says. ` +
	`Suggested fix appears only on a finding whose proven field is code. Check the PR head first with ` +
	`gh pr view <n> --json headRefOid.`

// PRRecipe is the stored recipe a run follows.
type PRRecipe struct {
	Name        string  `json:"name"`
	Match       string  `json:"match"`
	Harness     string  `json:"harness"`
	Panel       string  `json:"panel"`
	VerifyAt    string  `json:"verify_at"`
	Critics     string  `json:"critics"`
	Second      string  `json:"second"`
	WalkerBrief string  `json:"walker_brief"`
	BudgetUSD   float64 `json:"budget_usd"`
	TurnsCap    int     `json:"turns_cap"`
	UpdatedAt   string  `json:"updated_at"`
}

// PanelEntry is one reviewer in a recipe's panel.
type PanelEntry struct {
	Agent string   `json:"agent"`
	When  []string `json:"when"`
}

// Reviewers parses the panel.
func (r *PRRecipe) Reviewers() ([]PanelEntry, error) {
	var out []PanelEntry
	err := json.Unmarshal([]byte(r.Panel), &out)
	return out, err
}

// ReviewersFor picks the panel entries that apply to a set of changed files: an
// entry applies when any of its globs matches any file's base name.
func (r *PRRecipe) ReviewersFor(files []string) ([]PanelEntry, error) {
	all, err := r.Reviewers()
	if err != nil {
		return nil, err
	}
	var out []PanelEntry
	for _, e := range all {
		if panelApplies(e, files) {
			out = append(out, e)
		}
	}
	return out, nil
}

func panelApplies(e PanelEntry, files []string) bool {
	for _, g := range e.When {
		if g == "*" {
			return true
		}
		for _, f := range files {
			if ok, _ := path.Match(g, path.Base(filepath.ToSlash(f))); ok {
				return true
			}
		}
	}
	return false
}

const prRecipeColumns = `name, match, harness, panel, verify_at, critics, second, walker_brief,
	budget_usd, turns_cap, updated_at`

func scanPRRecipe(sc interface{ Scan(...any) error }) (*PRRecipe, error) {
	var r PRRecipe
	err := sc.Scan(&r.Name, &r.Match, &r.Harness, &r.Panel, &r.VerifyAt, &r.Critics,
		&r.Second, &r.WalkerBrief, &r.BudgetUSD, &r.TurnsCap, &r.UpdatedAt)
	return &r, err
}

// PRRecipes lists every recipe, by name.
func (st *Store) PRRecipes() ([]*PRRecipe, error) {
	var out []*PRRecipe
	err := st.guard(func() error {
		out = nil
		rows, err := st.db.Query(`SELECT ` + prRecipeColumns + ` FROM pr_recipe ORDER BY name ASC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanPRRecipe(rows)
			if err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

// PRRecipeByName returns one recipe. A missing one is sql.ErrNoRows.
func (st *Store) PRRecipeByName(name string) (*PRRecipe, error) {
	var out *PRRecipe
	err := st.guard(func() error {
		got, err := scanPRRecipe(st.db.QueryRow(
			`SELECT `+prRecipeColumns+` FROM pr_recipe WHERE name = ?`, name))
		if err != nil {
			return err
		}
		out = got
		return nil
	})
	return out, err
}

// SavePRRecipe creates or replaces a recipe. Refused, with a sentence, when the
// panel or critics are not the JSON a run reads or the match is not a glob:
// discovering that at run time would put the error in front of nobody.
func (st *Store) SavePRRecipe(r PRRecipe) (*PRRecipe, error) {
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" {
		return nil, errors.New("a recipe needs a name")
	}
	if strings.TrimSpace(r.Harness) == "" {
		return nil, errors.New("a recipe needs a harness")
	}
	if strings.TrimSpace(r.WalkerBrief) == "" {
		return nil, errors.New("a recipe needs a walker brief")
	}
	if _, err := path.Match(r.Match, "x/y"); err != nil {
		return nil, errors.New("match is not a valid glob: " + err.Error())
	}
	panel, err := r.Reviewers()
	if err != nil {
		return nil, errors.New("panel is not a json list of {agent, when}: " + err.Error())
	}
	if len(panel) == 0 {
		return nil, errors.New("a recipe needs at least one reviewer")
	}
	for _, e := range panel {
		if strings.TrimSpace(e.Agent) == "" {
			return nil, errors.New("every reviewer in the panel needs an agent")
		}
		for _, g := range e.When {
			if _, err := path.Match(g, "x"); err != nil {
				return nil, fmt.Errorf("reviewer %s: %q is not a valid glob", e.Agent, g)
			}
		}
	}
	if strings.TrimSpace(r.Critics) == "" {
		r.Critics = "[]"
	}
	var critics []string
	if err := json.Unmarshal([]byte(r.Critics), &critics); err != nil {
		return nil, errors.New("critics is not a json list of names: " + err.Error())
	}
	if r.VerifyAt == "" {
		r.VerifyAt = "med"
	}
	switch r.VerifyAt {
	case "high", "med", "low", "none":
	default:
		return nil, errors.New("verify_at is high, med, low or none")
	}
	if r.BudgetUSD <= 0 {
		return nil, errors.New("the budget has to be above zero")
	}
	if r.TurnsCap < 1 {
		return nil, errors.New("turns_cap has to be at least 1")
	}
	err = st.guard(func() error {
		_, err := st.db.Exec(`INSERT INTO pr_recipe (`+prRecipeColumns+`)
			VALUES (?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(name) DO UPDATE SET match = excluded.match, harness = excluded.harness,
				panel = excluded.panel, verify_at = excluded.verify_at,
				critics = excluded.critics, second = excluded.second,
				walker_brief = excluded.walker_brief, budget_usd = excluded.budget_usd,
				turns_cap = excluded.turns_cap, updated_at = excluded.updated_at`,
			r.Name, r.Match, r.Harness, r.Panel, r.VerifyAt, r.Critics, r.Second,
			r.WalkerBrief, r.BudgetUSD, r.TurnsCap, ts(now()))
		return err
	})
	if err != nil {
		return nil, err
	}
	return st.PRRecipeByName(r.Name)
}

// DeletePRRecipe removes a recipe. Deleting the last one means a run has
// nothing to follow, which RecipeFor reports.
func (st *Store) DeletePRRecipe(name string) error {
	return st.guard(func() error {
		_, err := st.db.Exec(`DELETE FROM pr_recipe WHERE name = ?`, name)
		return err
	})
}

// ErrNoPRRecipe is what RecipeFor answers when no recipe covers a repo.
var ErrNoPRRecipe = errors.New("no review recipe covers this repo")

// RecipeFor picks the recipe for an org/repo. The most specific match wins,
// the rule standing rules use: a recipe with a glob beats one with none, and
// the glob with more literal characters beats the vaguer one. An empty match is
// every repo, so `default` is the fallback.
func (st *Store) RecipeFor(orgRepo string) (*PRRecipe, error) {
	all, err := st.PRRecipes()
	if err != nil {
		return nil, err
	}
	var best *PRRecipe
	bestScore := -1
	for _, r := range all {
		score := 0
		if r.Match != "" {
			ok, err := path.Match(r.Match, orgRepo)
			if err != nil || !ok {
				continue
			}
			score = 1 + literalChars(r.Match)
		}
		if score > bestScore || (score == bestScore && best != nil && r.Name < best.Name) {
			best, bestScore = r, score
		}
	}
	if best == nil {
		return nil, ErrNoPRRecipe
	}
	return best, nil
}

func literalChars(glob string) int {
	n := 0
	for _, r := range glob {
		if r != '*' && r != '?' && r != '[' && r != ']' {
			n++
		}
	}
	return n
}
