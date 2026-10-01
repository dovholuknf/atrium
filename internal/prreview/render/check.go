package render

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Rule numbers are those of docs/review/review-memory-design.md. Rule 0 is this package's own: a finding the
// renderer cannot place at all.
const (
	RuleShape    = 0
	RuleLeak     = 5
	RuleLine     = 8 // and 35, which says the same thing from the other side
	RuleExposure = 26
	RuleLeadIn   = 34
	RuleOneFix   = 36
	RulePaths    = 40
	RuleProven   = 43
)

var ruleText = map[int]string{
	RuleShape:    "A finding has a severity of high, med, low or nit, a path and a line.",
	RuleLeak:     "Every leak goes in the findings with its size and stack. The verify pass may correct a leak's severity, and never removes one.",
	RuleLine:     "Line numbers come from the PR head. A comment must sit on a line the PR adds or changes, and on the line that is wrong, not a neighbour.",
	RuleExposure: "Before a finding is rated MED or higher, three questions are answered: who actually hits it, how likely it is, and whether it is opt-in.",
	RuleLeadIn:   `"LLM review says" appears at most once per item, as a lead-in line above the bullets.`,
	RuleOneFix:   `The suggested fix is one concrete change, never "X, or Y".`,
	RulePaths:    "Every path named in a bullet is checked against the tree.",
	RuleProven:   `"Suggested fix:" only for a problem proven by a test we wrote or settled by the code. For an unproven one the fix is a question: "Could we ...?", "What if we ...?", "Should we ...?".`,
}

// hard rules are the ones where the renderer refuses to improve a finding itself. Once the merge fork has had its
// one chance, they fail the run.
var hard = map[int]bool{RuleShape: true, RuleLeak: true, RuleLine: true, RuleExposure: true}

// Check says which finding failed which rule.
type Check struct {
	Rule    int
	Finding string // path:line, or the raw finding's path:line for a missing leak
	Index   int    // into Input.Findings, or -1 for a leak that is missing from it
	Detail  string
	Quote   string // the rule's text, for the prompt that goes back to the merge fork
}

func (c Check) String() string {
	return fmt.Sprintf("%s fails rule %d: %s", c.Finding, c.Rule, c.Detail)
}

func key(f Finding) string { return f.Path + ":" + strconv.Itoa(f.Line) }

func newCheck(rule int, f Finding, idx int, detail string) Check {
	return Check{Rule: rule, Finding: key(f), Index: idx, Detail: detail, Quote: ruleText[rule]}
}

var leadInRe = regexp.MustCompile(`(?i)LLM review says[:,]?\s*`)

var questionRe = regexp.MustCompile(`(?i)^(could we|what if we|should we)\b`)

// twoFixesRe finds "X, or Y" and its cousins.
var twoFixesRe = regexp.MustCompile(`(?i)([,;]\s*or\b|\beither\b.*\bor\b|\balternatively\b)`)

func severity(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "high", "blocking", "critical":
		return "high", true
	case "med", "medium":
		return "med", true
	case "low":
		return "low", true
	case "nit":
		return "nit", true
	}
	return "", false
}

// isQuestion is rule 43's "Could we ...?" form: one sentence, ending in the only question mark.
func isQuestion(s string) bool {
	s = strings.TrimSpace(s)
	return questionRe.MatchString(s) && strings.HasSuffix(s, "?") && strings.Count(s, "?") == 1
}

func leadIns(f Finding) (count int, prefix bool) {
	prefix = leadInRe.FindStringIndex(strings.TrimSpace(f.Says)) != nil &&
		leadInRe.FindStringIndex(strings.TrimSpace(f.Says))[0] == 0
	count = len(leadInRe.FindAllString(f.Says, -1)) + len(leadInRe.FindAllString(f.Fix, -1)) +
		len(leadInRe.FindAllString(f.TestAsk, -1))
	return
}

var pathTokenRe = regexp.MustCompile("`([^`\\s]+)`")

var pathExts = map[string]bool{
	"go": true, "c": true, "h": true, "cc": true, "cpp": true, "hpp": true, "rs": true, "py": true, "js": true,
	"ts": true, "tsx": true, "jsx": true, "java": true, "kt": true, "cs": true, "rb": true, "md": true,
	"json": true, "yaml": true, "yml": true, "toml": true, "txt": true, "sh": true, "ps1": true, "html": true,
	"css": true, "mod": true, "sum": true, "cmake": true, "swift": true, "m": true,
}

// pathsIn returns the backticked tokens of a bullet that look like a file path, without a trailing :line.
func pathsIn(text string) []string {
	var out []string
	for _, m := range pathTokenRe.FindAllStringSubmatch(text, -1) {
		t := m[1]
		if i := strings.LastIndex(t, ":"); i > 0 {
			if _, err := strconv.Atoi(t[i+1:]); err == nil {
				t = t[:i]
			}
		}
		if strings.ContainsAny(t, "()<>*=") {
			continue
		}
		dot := strings.LastIndex(t, ".")
		if dot <= 0 || !pathExts[strings.ToLower(t[dot+1:])] {
			continue
		}
		out = append(out, t)
	}
	return out
}

// tree answers "is there such a file in src/", by exact path, else by base name when the token has no directory.
type tree struct {
	root  string
	names map[string]bool
}

func newTree(runDir string) *tree {
	if runDir == "" {
		return nil
	}
	root := filepath.Join(runDir, "src")
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return nil
	}
	return &tree{root: root}
}

func (t *tree) has(p string) bool {
	p = strings.TrimPrefix(filepath.ToSlash(p), "./")
	if st, err := os.Stat(filepath.Join(t.root, filepath.FromSlash(p))); err == nil && !st.IsDir() {
		return true
	}
	if strings.Contains(p, "/") {
		return false
	}
	if t.names == nil {
		t.names = map[string]bool{}
		_ = filepath.WalkDir(t.root, func(q string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() && d.Name() == ".git" {
				return filepath.SkipDir
			}
			if !d.IsDir() {
				t.names[d.Name()] = true
			}
			return nil
		})
	}
	return t.names[p]
}

// missingPaths lists the paths named in the finding's bullets that src/ does not hold.
func missingPaths(f Finding, t *tree) []string {
	if t == nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, text := range []string{f.Says, f.Fix, f.TestAsk} {
		for _, p := range pathsIn(text) {
			if !seen[p] && !t.has(p) {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// Checks runs the 5.6 table over the merged list.
func Checks(in Input) []Check {
	var out []Check
	d := parseDiff(in.Diff)
	t := newTree(in.RunDir)

	for i, f := range in.Findings {
		sev, ok := severity(f.Sev)
		if !ok || strings.TrimSpace(f.Path) == "" || f.Line <= 0 {
			out = append(out, newCheck(RuleShape, f, i, fmt.Sprintf("sev %q, path %q, line %d", f.Sev, f.Path, f.Line)))
			continue
		}
		if !d.adds(f.Path, f.Line) {
			out = append(out, newCheck(RuleLine, f, i, "the PR does not add or change this line at the head"))
		}
		if sev == "high" || sev == "med" {
			var gone []string
			if strings.TrimSpace(f.Exposure.Who) == "" {
				gone = append(gone, "who")
			}
			if strings.TrimSpace(f.Exposure.Likely) == "" {
				gone = append(gone, "likely")
			}
			if strings.TrimSpace(f.Exposure.OptIn) == "" {
				gone = append(gone, "opt_in")
			}
			if len(gone) > 0 {
				out = append(out, newCheck(RuleExposure, f, i, "exposure is missing "+strings.Join(gone, ", ")))
			}
		}
		if n, prefix := leadIns(f); n > 1 || (n == 1 && !prefix) {
			out = append(out, newCheck(RuleLeadIn, f, i, `"LLM review says" is written in the finding's own text`))
		}
		fix := strings.TrimSpace(f.Fix)
		if fix != "" && twoFixesRe.MatchString(fix) {
			out = append(out, newCheck(RuleOneFix, f, i, "the fix offers more than one change"))
		}
		switch f.Proven {
		case "code":
		case "no":
			if fix != "" && !isQuestion(fix) {
				out = append(out, newCheck(RuleProven, f, i, `proven is "no" and the fix is not a "Could we ...?" question`))
			}
		default:
			out = append(out, newCheck(RuleProven, f, i, fmt.Sprintf(`proven is %q, which is neither "code" nor "no"`, f.Proven)))
		}
		if gone := missingPaths(f, t); len(gone) > 0 {
			out = append(out, newCheck(RulePaths, f, i, "not in src/: "+strings.Join(gone, ", ")))
		}
	}

	for _, r := range in.Raw {
		if strings.TrimSpace(r.Leak) == "" || leakKept(r, in.Findings) {
			continue
		}
		out = append(out, newCheck(RuleLeak, r, -1, fmt.Sprintf("a leak raised by %s is not in the final list: %s", r.RaisedBy, r.Leak)))
	}
	return out
}

func leakKept(raw Finding, final []Finding) bool {
	for _, f := range final {
		if strings.TrimSpace(f.Leak) == "" || filepath.ToSlash(f.Path) != filepath.ToSlash(raw.Path) {
			continue
		}
		if f.Line == raw.Line || (strings.TrimSpace(raw.Code) != "" && strings.TrimSpace(f.Code) == strings.TrimSpace(raw.Code)) {
			return true
		}
	}
	return false
}
