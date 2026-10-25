// Package cardreview holds what a review card does on its room: it measures against a base, turns a finding's edits
// into a diff on a scratch copy, and applies stored edits inside the card's directory and nowhere else.
package cardreview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/base/nowindow"
	"github.com/dovholuknf/atrium/internal/base/safepath"
	"github.com/dovholuknf/atrium/internal/store"
)

const gitBound = 30 * time.Second

// Categories are the kinds of slop a finding can name.
var Categories = []string{"SCOPE", "DEAD", "DRY", "CHURN", "HYGIENE", "TEST SCOPE", "COMMENT", "SIMPLIFY"}

// Edit is one change, shaped like an Edit call: Old must appear exactly once in the file.
type Edit struct {
	Path string `json:"path"`
	Old  string `json:"old"`
	New  string `json:"new"`
}

// File is one changed file of the review's base, with its line counts.
type File struct {
	Path    string `json:"path"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
}

// FindingIn is what a finding or a batch item carries.
type FindingIn struct {
	Cat    string `json:"cat"`
	Kind   string `json:"kind"`
	Origin string `json:"origin"`
	Site   string `json:"site"`
	Title  string `json:"title"`
	Edits  []Edit `json:"edits"`
	Note   string `json:"note"`
	Footer string `json:"footer"`
}

// StartOut is what start answers.
type StartOut struct {
	Base      string `json:"base"`
	MergeBase string `json:"merge_base"`
	Files     []File `json:"files"`
}

// FindingOut is what a finding answers: its number and the measured count.
type FindingOut struct {
	Num   int `json:"num"`
	Lines int `json:"lines_vs_base"`
}

// View is what the board reads.
type View struct {
	Base      string          `json:"base"`
	MergeBase string          `json:"merge_base"`
	Files     []File          `json:"files"`
	Findings  []store.Finding `json:"findings"`
}

// Service is the review of one room.
type Service struct {
	St *store.Store
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitBound)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	nowindow.Hide(cmd)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	if err != nil {
		// git diff --no-index exits 1 when the sides differ
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 1 && len(args) > 1 && args[0] == "diff" && out.Len() > 0 {
			return out.String(), nil
		}
		return out.String(), fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// Start finds the merge-base with base, lists the changed files and records the review on the card.
func (s *Service) Start(ctx context.Context, card, dir, base string) (StartOut, error) {
	base = strings.TrimSpace(base)
	if base == "" || strings.HasPrefix(base, "-") {
		return StartOut{}, errors.New("say the base branch")
	}
	mb, err := git(ctx, dir, "merge-base", "HEAD", base)
	if err != nil {
		return StartOut{}, fmt.Errorf("no merge-base between HEAD and %s: %w", base, err)
	}
	mb = strings.TrimSpace(mb)
	num, err := git(ctx, dir, "diff", "--numstat", mb)
	if err != nil {
		return StartOut{}, err
	}
	files := parseNumstat(num)
	raw, _ := json.Marshal(files)
	if err := s.St.ReviewStart(card, base, mb, string(raw)); err != nil {
		return StartOut{}, err
	}
	return StartOut{Base: base, MergeBase: mb, Files: files}, nil
}

func parseNumstat(out string) []File {
	files := []File{}
	for _, ln := range strings.Split(out, "\n") {
		p := strings.Split(ln, "\t")
		if len(p) < 3 {
			continue
		}
		a, _ := strconv.Atoi(p[0])
		r, _ := strconv.Atoi(p[1])
		files = append(files, File{Path: p[2], Added: a, Removed: r})
	}
	return files
}

// Finding computes the diff of the edits, stores the finding and answers its number and measured count.
func (s *Service) Finding(ctx context.Context, card, dir string, in FindingIn) (FindingOut, error) {
	return s.add(ctx, card, dir, in, "", "single", false)
}

// Batch applies each item to the card's directory and stores it as applied for the veto.
func (s *Service) Batch(ctx context.Context, card, dir string, items []FindingIn) ([]FindingOut, error) {
	if len(items) == 0 {
		return nil, errors.New("a batch needs items")
	}
	out := make([]FindingOut, 0, len(items))
	for i, it := range items {
		o, err := s.add(ctx, card, dir, it, store.FindingApplied, "batch", true)
		if err != nil {
			return out, fmt.Errorf("batch item %d: %w", i+1, err)
		}
		out = append(out, o)
	}
	return out, nil
}

func (s *Service) add(ctx context.Context, card, dir string, in FindingIn, state, group string, apply bool) (FindingOut, error) {
	rv, err := s.St.ReviewOf(card)
	if err != nil {
		return FindingOut{}, errors.New("start the review first")
	}
	cat := strings.ToUpper(strings.TrimSpace(in.Cat))
	if !contains(Categories, cat) {
		return FindingOut{}, fmt.Errorf("cat %q is not one of %s", in.Cat, strings.Join(Categories, ", "))
	}
	if len(in.Edits) == 0 {
		return FindingOut{}, errors.New("a finding needs edits")
	}
	diff, baseDiff, lines, err := s.measure(ctx, dir, rv.MergeBase, in.Edits)
	if err != nil {
		return FindingOut{}, err
	}
	if apply {
		if err := applyEdits(dir, in.Edits, false); err != nil {
			return FindingOut{}, err
		}
	}
	raw, _ := json.Marshal(in.Edits)
	num, err := s.St.FindingAdd(store.Finding{
		Card: card, Cat: cat, Kind: in.Kind, Origin: in.Origin, Site: in.Site, Title: in.Title, Edits: string(raw),
		Note: in.Note, Footer: in.Footer, Diff: diff, BaseDiff: baseDiff, Lines: lines, Grp: group, State: state,
	})
	return FindingOut{Num: num, Lines: lines}, err
}

// Apply writes a stored finding's edits into the card's directory and marks it applied.
func (s *Service) Apply(card, dir string, num int) error {
	f, err := s.St.FindingGet(card, num)
	if err != nil {
		return err
	}
	if f.State == store.FindingApplied {
		return errors.New("that finding is applied already")
	}
	var edits []Edit
	if err := json.Unmarshal([]byte(f.Edits), &edits); err != nil {
		return err
	}
	if err := applyEdits(dir, edits, false); err != nil {
		return err
	}
	return s.St.FindingSetState(card, num, store.FindingApplied, "")
}

// Close marks a finding withdrawn, kept or vetoed. A vetoed finding that was applied is put back.
func (s *Service) Close(card, dir string, num int, state, reason string) error {
	switch state {
	case store.FindingWithdrawn, store.FindingKept, store.FindingVetoed:
	default:
		return fmt.Errorf("state %q is not withdrawn, kept or vetoed", state)
	}
	f, err := s.St.FindingGet(card, num)
	if err != nil {
		return err
	}
	if state == store.FindingVetoed && f.State == store.FindingApplied {
		var edits []Edit
		if err := json.Unmarshal([]byte(f.Edits), &edits); err != nil {
			return err
		}
		if err := applyEdits(dir, edits, true); err != nil {
			return fmt.Errorf("could not put it back: %w", err)
		}
	}
	return s.St.FindingSetState(card, num, state, reason)
}

// View is the review the board draws.
func (s *Service) View(card string) (View, error) {
	rv, err := s.St.ReviewOf(card)
	if err != nil {
		return View{}, err
	}
	fs, err := s.St.Findings(card)
	if err != nil {
		return View{}, err
	}
	v := View{Base: rv.Base, MergeBase: rv.MergeBase, Files: []File{}, Findings: fs}
	if v.Findings == nil {
		v.Findings = []store.Finding{}
	}
	_ = json.Unmarshal([]byte(rv.Files), &v.Files)
	return v, nil
}

// replaceOnce is Edit's rule: old has to be in the text exactly once.
func replaceOnce(text, old, nw string) (string, error) {
	if old == "" {
		return "", errors.New("an edit needs old text")
	}
	switch strings.Count(text, old) {
	case 0:
		return "", errors.New("old text is not in the file")
	case 1:
		return strings.Replace(text, old, nw, 1), nil
	}
	return "", errors.New("old text is in the file more than once")
}

// applyEdits writes the edits inside dir. Every path is resolved and every edit applied in memory first, so a refused
// path or a text that does not match writes nothing. reverse swaps old and new to undo.
func applyEdits(dir string, edits []Edit, reverse bool) error {
	files, order, err := stage(dir, edits, reverse)
	if err != nil {
		return err
	}
	for _, full := range order {
		if err := os.WriteFile(full, []byte(files[full]), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func stage(dir string, edits []Edit, reverse bool) (map[string]string, []string, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, nil, errors.New("this card has no directory")
	}
	files := map[string]string{}
	var order []string
	for _, e := range edits {
		full, err := safepath.Contained(filepath.FromSlash(dir), e.Path)
		if err != nil {
			return nil, nil, safepath.ErrOutside
		}
		cur, ok := files[full]
		if !ok {
			raw, err := os.ReadFile(full)
			if err != nil {
				return nil, nil, fmt.Errorf("%s: cannot read it", e.Path)
			}
			cur = string(raw)
			order = append(order, full)
		}
		old, nw := e.Old, e.New
		if reverse {
			old, nw = nw, old
		}
		next, err := replaceOnce(cur, old, nw)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", e.Path, err)
		}
		files[full] = next
	}
	return files, order, nil
}

// measure applies the edits to a scratch copy and answers the diff of them, the diff of the result against the
// merge-base, and the count of changed lines of that second diff.
func (s *Service) measure(ctx context.Context, dir, mergeBase string, edits []Edit) (diff, baseDiff string, lines int, err error) {
	files, _, err := stage(dir, edits, false)
	if err != nil {
		return "", "", 0, err
	}
	scratch, err := os.MkdirTemp("", "atrium-review-")
	if err != nil {
		return "", "", 0, err
	}
	defer os.RemoveAll(scratch)

	rels := map[string]string{}
	for full := range files {
		rel, err := filepath.Rel(realDir(dir), full)
		if err != nil {
			return "", "", 0, safepath.ErrOutside
		}
		rels[full] = filepath.ToSlash(rel)
	}
	fulls := make([]string, 0, len(files))
	for full := range files {
		fulls = append(fulls, full)
	}
	sort.Strings(fulls)
	for _, full := range fulls {
		rel := rels[full]
		orig, err := os.ReadFile(full)
		if err != nil {
			return "", "", 0, err
		}
		if err := put(filepath.Join(scratch, "a", filepath.FromSlash(rel)), orig); err != nil {
			return "", "", 0, err
		}
		if err := put(filepath.Join(scratch, "b", filepath.FromSlash(rel)), []byte(files[full])); err != nil {
			return "", "", 0, err
		}
		if base, err := git(ctx, dir, "show", mergeBase+":"+rel); err == nil {
			if err := put(filepath.Join(scratch, "c", filepath.FromSlash(rel)), []byte(base)); err != nil {
				return "", "", 0, err
			}
		}
	}
	if err := os.MkdirAll(filepath.Join(scratch, "c"), 0o755); err != nil {
		return "", "", 0, err
	}
	diff, err = git(ctx, scratch, "diff", "--no-index", "--no-color", "a", "b")
	if err != nil {
		return "", "", 0, err
	}
	bd, err := git(ctx, scratch, "diff", "--no-index", "--no-color", "c", "b")
	if err != nil {
		return "", "", 0, err
	}
	return tidy(diff, "a"), tidy(bd, "c"), countChanged(bd), nil
}

// tidy drops the scratch folder names git puts in a diff's file headers, leaving a/<path> and b/<path>.
func tidy(diff, aDir string) string {
	lines := strings.Split(diff, "\n")
	for i, ln := range lines {
		switch {
		case strings.HasPrefix(ln, "diff --git "), strings.HasPrefix(ln, "--- "), strings.HasPrefix(ln, "+++ "):
			ln = strings.ReplaceAll(ln, "a/"+aDir+"/", "a/")
			ln = strings.ReplaceAll(ln, "b/b/", "b/")
			lines[i] = ln
		}
	}
	return strings.Join(lines, "\n")
}

func realDir(dir string) string {
	if r, err := filepath.EvalSymlinks(filepath.FromSlash(dir)); err == nil {
		if a, err := filepath.Abs(r); err == nil {
			return a
		}
	}
	return filepath.FromSlash(dir)
}

func put(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func countChanged(diff string) int {
	n := 0
	for _, ln := range strings.Split(diff, "\n") {
		if strings.HasPrefix(ln, "+++") || strings.HasPrefix(ln, "---") {
			continue
		}
		if strings.HasPrefix(ln, "+") || strings.HasPrefix(ln, "-") {
			n++
		}
	}
	return n
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}
