// Package persona reads clint's persona pack and does the three things atrium
// does with it: lists the personas, sets up a run directory for one to review a
// card's diff, and draws the lessons review. See docs/personas-design.md,
// "What atrium does, and what it does not".
//
// ATRIUM DRIVES THE PACK AND DOES NOT BECOME IT. Every git command here reads.
// The only files written inside the pack are the lessons view's promote, keep
// and delete edits, and every one of those goes through internal/safepath
// against the pack path, so nothing outside it can be touched. The commit is
// the human's.
package persona

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
	"go.yaml.in/yaml/v3"
)

// RunsDirName is the folder under atrium's data directory that holds one
// scratch directory per persona run.
const RunsDirName = "persona-runs"

// idPattern is what a persona id may be. The id is a folder name, a native
// memory directory name and a path segment in the run directory, so anything
// that could climb or collide is refused before it is used as any of them.
var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// ValidID reports whether id is safe to use as a persona id.
func ValidID(id string) bool {
	return idPattern.MatchString(id) && !strings.Contains(id, "..")
}

// Reviews is what a persona says it reviews, read verbatim from its yaml.
type Reviews struct {
	Paths    []string `yaml:"paths" json:"paths"`
	Surfaces []string `yaml:"surfaces" json:"surfaces"`
	SkipWhen string   `yaml:"skip_when" json:"skip_when,omitempty"`
}

// Persona is one row of the catalog.
type Persona struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Runners is what the yaml declares. Renders is the subset with a rendered
	// file on disk, which is the only set a review can be launched on.
	Runners []string `json:"runners"`
	Renders []string `json:"renders"`
	Reviews *Reviews `json:"reviews,omitempty"`
	// LastRun is when a run directory was last made for it, empty for never.
	LastRun string `json:"last_run,omitempty"`
	// Problem is why the row is not usable, said rather than dropped.
	Problem string `json:"problem,omitempty"`

	dir string
}

// Dir is the persona's folder in the pack.
func (p Persona) Dir() string { return p.dir }

// RendersFor reports whether a review can be launched on runner.
func (p Persona) RendersFor(runner string) bool {
	for _, r := range p.Renders {
		if strings.EqualFold(r, runner) {
			return true
		}
	}
	return false
}

// stringList reads `runners: claude` and `runners: [claude, codex]` alike,
// since the pack has written the first and the design implies the second.
type stringList []string

func (l *stringList) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.ScalarNode:
		*l = nil
		for _, f := range strings.FieldsFunc(n.Value, func(r rune) bool { return r == ',' || r == ' ' }) {
			*l = append(*l, f)
		}
		return nil
	case yaml.SequenceNode:
		var out []string
		if err := n.Decode(&out); err != nil {
			return err
		}
		*l = out
		return nil
	}
	return fmt.Errorf("runners must be a name or a list of names")
}

type personaYAML struct {
	ID          string     `yaml:"id"`
	Name        string     `yaml:"name"`
	Description string     `yaml:"description"`
	Runners     stringList `yaml:"runners"`
	Reviews     *Reviews   `yaml:"reviews"`
	Claude      struct {
		Frontmatter string `yaml:"frontmatter"`
	} `yaml:"claude"`
}

// Catalog reads every persona in the pack. runsDir is where run directories
// live, read for each persona's last run, and may be empty.
//
// A folder with no persona.yaml is not a persona and is skipped. One whose yaml
// does not parse is listed with the reason, because a persona that vanished
// from the list without a word is one somebody spends an evening looking for.
func Catalog(pack, runsDir string) ([]Persona, error) {
	pack = strings.TrimSpace(pack)
	if pack == "" {
		return nil, errors.New("no persona pack is configured. set " + store.SettingPersonaPackPath)
	}
	entries, err := os.ReadDir(filepath.FromSlash(pack))
	if err != nil {
		return nil, fmt.Errorf("the persona pack at %s cannot be read: %w", pack, err)
	}
	var out []Persona
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir := filepath.Join(filepath.FromSlash(pack), e.Name())
		if _, err := os.Stat(filepath.Join(dir, "persona.yaml")); err != nil {
			continue
		}
		p := Read(dir)
		if runsDir != "" && ValidID(p.ID) {
			p.LastRun = lastRun(filepath.Join(runsDir, p.ID))
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Find reads one persona by id.
func Find(pack, id string) (Persona, error) {
	if !ValidID(id) {
		return Persona{}, fmt.Errorf("%q is not a persona id", id)
	}
	dir := filepath.Join(filepath.FromSlash(pack), id)
	if _, err := os.Stat(filepath.Join(dir, "persona.yaml")); err != nil {
		return Persona{}, fmt.Errorf("the pack has no persona %s", id)
	}
	p := Read(dir)
	if p.Problem != "" {
		return p, errors.New(p.Problem)
	}
	return p, nil
}

// Read parses one persona folder.
func Read(dir string) Persona {
	folder := filepath.Base(dir)
	p := Persona{ID: folder, Name: folder, dir: dir}
	raw, err := os.ReadFile(filepath.Join(dir, "persona.yaml"))
	if err != nil {
		p.Problem = "persona.yaml cannot be read: " + err.Error()
		return p
	}
	var y personaYAML
	if err := yaml.Unmarshal(raw, &y); err != nil {
		p.Problem = "persona.yaml does not parse: " + err.Error()
		return p
	}
	// THE ID IS THE FOLDER NAME AND NEVER CHANGES. A yaml that disagrees is
	// a rename half done, and launching it would key its memory on one name
	// and its folder on the other.
	if y.ID != "" && y.ID != folder {
		p.Problem = fmt.Sprintf("persona.yaml says its id is %s but its folder is %s", y.ID, folder)
	}
	if !ValidID(folder) {
		p.Problem = fmt.Sprintf("%q is not usable as a persona id", folder)
	}
	if y.Name != "" {
		p.Name = y.Name
	}
	p.Description = y.Description
	if p.Description == "" && y.Claude.Frontmatter != "" {
		// Stage 1 kept Claude's frontmatter verbatim, so that is where the
		// description still lives.
		var fm struct {
			Description string `yaml:"description"`
		}
		if yaml.Unmarshal([]byte(y.Claude.Frontmatter), &fm) == nil {
			p.Description = fm.Description
		}
	}
	p.Runners = []string(y.Runners)
	if p.Runners == nil {
		p.Runners = []string{}
	}
	p.Reviews = y.Reviews
	p.Renders = []string{}
	for _, r := range p.Runners {
		if renderFile(dir, folder, r) != "" {
			p.Renders = append(p.Renders, r)
		}
	}
	return p
}

// renderFile is the rendered file a runner loads, or empty when there is none.
//
// Claude's is one named file. The other runners' shapes are not measured yet
// (design stage 5), so for them a non-empty render folder is taken as "renders
// for", and nothing launches on them today.
func renderFile(dir, id, runner string) string {
	runner = strings.ToLower(strings.TrimSpace(runner))
	if runner == "" || strings.ContainsAny(runner, `/\.`) {
		return ""
	}
	if runner == "claude" {
		f := filepath.Join(dir, "render", "claude", id+".md")
		if fi, err := os.Stat(f); err == nil && !fi.IsDir() {
			return f
		}
		return ""
	}
	sub := filepath.Join(dir, "render", runner)
	if items, err := os.ReadDir(sub); err == nil && len(items) > 0 {
		return sub
	}
	return ""
}

// lastRun is the newest run directory's time, as RFC 3339.
func lastRun(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var newest time.Time
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}
	if newest.IsZero() {
		return ""
	}
	return newest.UTC().Format(time.RFC3339)
}
