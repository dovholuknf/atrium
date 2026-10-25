package render

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Resend asks the merge fork to fix one finding once, with the rules it broke quoted. The runner owns the fork.
type Resend struct {
	Finding string // path:line
	Index   int    // into Input.Findings, or -1 when the finding is a leak that went missing
	Checks  []Check
	Prompt  string
}

// FailMergeError is the run failing at `merge`, naming every finding that could not be placed.
type FailMergeError struct {
	Checks []Check
}

func (e *FailMergeError) Error() string {
	parts := make([]string, len(e.Checks))
	for i, c := range e.Checks {
		parts[i] = c.String()
	}
	return "merge: " + strings.Join(parts, "; ")
}

// Render is step 8. It returns the files, or the findings to send back to the merge fork once, or a FailMergeError.
// The runner calls it, resends what it is told to, sets Input.Resent and calls it again.
func Render(in Input) (Result, []Resend, error) {
	checks := Checks(in)
	if len(checks) > 0 && !in.Resent {
		return Result{}, resends(checks), nil
	}
	var fatal []Check
	failed := map[int]map[int]bool{}
	for _, c := range checks {
		if hard[c.Rule] {
			fatal = append(fatal, c)
			continue
		}
		if failed[c.Index] == nil {
			failed[c.Index] = map[int]bool{}
		}
		failed[c.Index][c.Rule] = true
	}
	if len(fatal) > 0 {
		return Result{}, nil, &FailMergeError{Checks: fatal}
	}

	d := parseDiff(in.Diff)
	tr := newTree(in.RunDir)
	// The diff's name for a file is the only one GitHub's anchor hashes, so the finding is rewritten to it here.
	// Checks has already refused a path that names no file or more than one.
	fs := make([]Finding, len(in.Findings))
	copy(fs, in.Findings)
	for i := range fs {
		fs[i].Path = resolve(d, fs[i].Path)
	}
	order := make([]int, len(fs))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return before(fs[order[a]], fs[order[b]], d) })

	width := len(fmt.Sprint(len(order)))
	if width < 2 {
		width = 2
	}
	var res Result
	for n, i := range order {
		f := fs[i]
		sev := word(f.Sev)
		name := fmt.Sprintf("%0*d-%s-%s-L%d.txt", width, n+1, sev, path.Base(filepath.ToSlash(f.Path)), f.Line)
		var gone []string
		if failed[i][RulePaths] {
			gone = missingPaths(f, tr)
		}
		res.Files = append(res.Files, File{Name: name, Text: text(in.PRURL, f, sev, failed[i], gone)})
	}
	res.Walk = walk(res.Files)
	return res, nil, nil
}

func resends(checks []Check) []Resend {
	var out []Resend
	at := map[string]int{}
	for _, c := range checks {
		k := fmt.Sprintf("%d|%s", c.Index, c.Finding)
		i, ok := at[k]
		if !ok {
			i = len(out)
			at[k] = i
			out = append(out, Resend{Finding: c.Finding, Index: c.Index})
		}
		out[i].Checks = append(out[i].Checks, c)
	}
	for i := range out {
		var b strings.Builder
		fmt.Fprintf(&b, "Fix the finding at %s. It broke:\n", out[i].Finding)
		for _, c := range out[i].Checks {
			fmt.Fprintf(&b, "- rule %d: %s\n  The rule: %s\n", c.Rule, c.Detail, c.Quote)
		}
		out[i].Prompt = b.String()
	}
	return out
}

// before is the walk order of 5.6: disputes left for clint, then the severity band, then rank, then the order the
// PR reads in. File name is never a key.
func before(a, b Finding, d diffInfo) bool {
	if a.LeftForClint != b.LeftForClint {
		return a.LeftForClint
	}
	sa, sb := band(a.Sev), band(b.Sev)
	if sa != sb {
		return sa < sb
	}
	if ra, rb := rankKey(a.Rank), rankKey(b.Rank); ra != rb {
		return ra < rb
	}
	pa, pb := d.position(a.Path), d.position(b.Path)
	if pa != pb {
		return pa < pb
	}
	return a.Line < b.Line
}

// rankKey puts an unset rank last in its band. Checks refuses one, so this only holds for a caller that sorts alone.
func rankKey(r int) int {
	if r <= 0 {
		return int(^uint(0) >> 1)
	}
	return r
}

// word is the severity as the label and the file name spell it. `blocking` keeps its own word (rule 10 lists it
// beside HIGH), and sorts with HIGH.
func word(s string) string {
	if strings.EqualFold(strings.TrimSpace(s), "blocking") {
		return "blocking"
	}
	sev, _ := severity(s)
	return sev
}

func band(s string) int {
	sev, _ := severity(s)
	return map[string]int{"high": 0, "med": 1, "low": 2, "nit": 3}[sev]
}

func idOf(f Finding) string {
	if f.ID != "" {
		return f.ID
	}
	sum := sha256.Sum256([]byte(f.Path + "\n" + f.Code + "\n" + f.Says))
	return hex.EncodeToString(sum[:])[:8]
}

// link is the GitHub anchor for a line on the right-hand side of the diff: the SHA-256 of the path, then R and the
// line.
func link(prURL string, f Finding) string {
	sum := sha256.Sum256([]byte(f.Path))
	return fmt.Sprintf("%s/files#diff-%sR%d", strings.TrimRight(prURL, "/"), hex.EncodeToString(sum[:]), f.Line)
}

func stripLeadIn(s string) string { return strings.TrimSpace(leadInRe.ReplaceAllString(s, "")) }

func text(prURL string, f Finding, sev string, failed map[int]bool, gone []string) string {
	var b strings.Builder
	b.WriteString(prURL + "\n")
	label := fmt.Sprintf("%s %s line %d:", strings.ToUpper(sev), f.Path, f.Line)
	if c := strings.TrimSpace(f.Code); c != "" {
		label += " " + c
	}
	b.WriteString(label + "\n")
	b.WriteString(link(prURL, f) + "\n\n")

	var notes []string
	if s := stripLeadIn(f.Says); s != "" {
		b.WriteString("* LLM review says " + s + "\n")
	}
	if fix := stripLeadIn(f.Fix); fix != "" {
		switch {
		case failed[RuleOneFix]:
			notes = append(notes, "Fix dropped, it offered more than one change (rule 36): "+fix)
		case failed[RuleProven]:
			notes = append(notes, `Fix dropped, it is not a "Could we ...?" question and was not proven (rule 43): `+fix)
		case f.Proven == "code":
			b.WriteString("* Suggested fix: " + fix + "\n")
		default:
			b.WriteString("* " + fix + "\n")
		}
	}
	if t := stripLeadIn(f.TestAsk); t != "" {
		b.WriteString("* " + t + "\n")
	}

	b.WriteString("\nEvidence\n")
	b.WriteString("Id: " + idOf(f) + "\n")
	if f.Cause != "" {
		b.WriteString("Cause: " + f.Cause + "\n")
	}
	if f.Found != "" {
		b.WriteString("Found: " + f.Found + "\n")
	}
	if f.RaisedBy != "" {
		b.WriteString("Raised by: " + f.RaisedBy + "\n")
	}
	if e := f.Exposure; e.Who != "" || e.Likely != "" || e.OptIn != "" {
		fmt.Fprintf(&b, "Exposure: who: %s. Likely: %s. Opt-in: %s.\n", e.Who, e.Likely, e.OptIn)
	}
	if f.Impact != "" {
		b.WriteString("Impact: " + f.Impact + "\n")
	}
	if f.Leak != "" {
		b.WriteString("Leak: " + f.Leak + "\n")
	}
	if f.Dispute != "" {
		b.WriteString("Second opinion: disputes. " + f.Dispute + "\n")
	}
	switch {
	case f.LeftForClint:
		b.WriteString("Settled: left for clint\n")
	case f.Settled != "":
		b.WriteString("Settled: " + f.Settled + "\n")
	}
	for _, p := range gone {
		b.WriteString("Path not in src/: " + p + "\n")
	}
	for _, n := range notes {
		b.WriteString(n + "\n")
	}
	return b.String()
}

func walk(files []File) string {
	w := 0
	for _, f := range files {
		if len(f.Name) > w {
			w = len(f.Name)
		}
	}
	var b strings.Builder
	for _, f := range files {
		fmt.Fprintf(&b, "%-*s  open\n", w, f.Name)
	}
	return b.String()
}

// WalkStartedError is Write refusing to render over a walk already begun. A rerun of step 6 and 7 would reset every
// state to open and lose what clint did.
type WalkStartedError struct {
	Lines []string
}

func (e *WalkStartedError) Error() string {
	return "walk started, a rewrite would lose it: " + strings.Join(e.Lines, ", ")
}

var walkLineRe = regexp.MustCompile(`^(.*?\.txt)\s+(\S+)(?:\s+(.*?))?\s*$`)

// WalkLine splits one walk.txt line into the finding's file name, its state, and whatever follows the state (a time
// and a URL). The name ends at the first ".txt" followed by space, since a name can hold a space (git quotes one). ok
// is false for a line with no name and state.
func WalkLine(line string) (name, state, rest string, ok bool) {
	m := walkLineRe.FindStringSubmatch(strings.TrimRight(line, "\r"))
	if m == nil {
		return "", "", "", false
	}
	return m[1], m[2], m[3], true
}

// started lists the walk.txt lines whose state is not open.
func started(walkTxt string) []string {
	var out []string
	for _, l := range strings.Split(walkTxt, "\n") {
		if name, state, _, ok := WalkLine(l); ok && state != "open" {
			out = append(out, name+" "+state)
		}
	}
	return out
}

// Write puts a result under runDir: findings/*.txt and walk.txt. It refuses with a *WalkStartedError when walk.txt
// already holds a line that is not open. Otherwise it removes the findings/*.txt this render did not make, so that
// `ls findings/` is the walk. It touches nothing else.
func Write(runDir string, r Result) error {
	dir := filepath.Join(runDir, "findings")
	if old, err := os.ReadFile(filepath.Join(runDir, "walk.txt")); err == nil {
		if s := started(string(old)); len(s) > 0 {
			return &WalkStartedError{Lines: s}
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	keep := map[string]bool{}
	for _, f := range r.Files {
		keep[f.Name] = true
	}
	if ents, err := os.ReadDir(dir); err == nil {
		for _, e := range ents {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".txt") && !keep[e.Name()] {
				if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
					return err
				}
			}
		}
	}
	for _, f := range r.Files {
		if err := os.WriteFile(filepath.Join(dir, f.Name), []byte(f.Text), 0o644); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(runDir, "walk.txt"), []byte(r.Walk), 0o644)
}
