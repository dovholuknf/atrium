package persona

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/dovholuknf/atrium/internal/safepath"
)

// The lessons review, drawn on the board. The skill it follows is
// dotfiles/claude/skills/lessons-review/SKILL.md, and the rule it keeps is the
// design's: WHAT "NEW" MEANS IS A GIT FACT. A persona's review covers every
// change under its memory since the most recent commit carrying
// `Lessons-reviewed: <id>`, plus anything uncommitted. There is no marker file
// and no state outside git.

// Lesson is one memory file with something to decide about.
type Lesson struct {
	// File is relative to the persona folder, `memory/x.md` or
	// `memory/inbox/x.md`.
	File string `json:"file"`
	// Status is added, modified, deleted, untracked, or unreviewed for a
	// persona whose whole history is in scope.
	Status      string `json:"status"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Repo        string `json:"repo,omitempty"`
	Why         string `json:"why,omitempty"`
	Diff        string `json:"diff"`
	// Inbox is a lesson written by a runner without native memory, waiting to
	// be merged into memory/.
	Inbox bool `json:"inbox,omitempty"`
}

// Tally is what this board has done in one persona's review so far. Held in
// memory only, to fill in the commit message: the record of what was decided
// is the commit, not this.
type Tally struct {
	Promoted int `json:"promoted"`
	Kept     int `json:"kept"`
	Deleted  int `json:"deleted"`
}

// Review is one persona's lessons view.
type Review struct {
	Persona string `json:"persona"`
	// Baseline is the last commit with the trailer, empty when the persona has
	// never been reviewed.
	Baseline        string   `json:"baseline"`
	BaselineSubject string   `json:"baseline_subject,omitempty"`
	Lessons         []Lesson `json:"lessons"`
	Rejections      []string `json:"rejections"`
	Tally           Tally    `json:"tally"`
	// Commit is the message clint runs, trailer included, and Command the
	// lines that run it. Atrium never runs either.
	Commit  string `json:"commit"`
	Command string `json:"command"`
}

// Service holds the tallies and serializes edits, so two clicks on one file
// cannot interleave a read of MEMORY.md with a write of it.
type Service struct {
	mu      sync.Mutex
	tallies map[string]*Tally
}

// NewService makes a lessons service.
func NewService() *Service { return &Service{tallies: map[string]*Tally{}} }

func (s *Service) tally(id string) Tally {
	if t := s.tallies[id]; t != nil {
		return *t
	}
	return Tally{}
}

// Read builds one persona's lessons view.
func (s *Service) Read(pack, id string) (*Review, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read(pack, id)
}

func (s *Service) read(pack, id string) (*Review, error) {
	p, err := Find(pack, id)
	if err != nil {
		return nil, err
	}
	dir := p.dir
	top, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("the persona pack is not in a git repository: %w", err)
	}
	r := &Review{Persona: id, Lessons: []Lesson{}, Rejections: []string{}}

	// The trailer on a line of its own. No path filter: a review that kept
	// every lesson touches no file in the folder and still counts.
	log, err := git(dir, "log", "-1", "--format=%H%x00%s",
		"--grep=^Lessons-reviewed: "+regexp.QuoteMeta(id)+"$")
	if err != nil && !strings.Contains(err.Error(), "does not have any commits") {
		return nil, err
	}
	if log != "" {
		parts := strings.SplitN(log, "\x00", 2)
		r.Baseline = parts[0]
		if len(parts) > 1 {
			r.BaselineSubject = parts[1]
		}
	}

	seen := map[string]bool{}
	add := func(file, status string) {
		file = filepath.ToSlash(file)
		base := filepath.Base(file)
		if seen[file] || base == "MEMORY.md" || base == "rejected.md" {
			return
		}
		seen[file] = true
		l := Lesson{File: file, Status: status, Inbox: strings.HasPrefix(file, "memory/inbox/")}
		if status != "deleted" {
			if raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(file))); err == nil {
				meta := parseLesson(string(raw))
				l.Name, l.Description, l.Repo, l.Why = meta.Name, meta.Description, meta.Repo, meta.Why
			}
		}
		l.Diff = s.diffOf(dir, r.Baseline, file, status)
		r.Lessons = append(r.Lessons, l)
	}

	if r.Baseline != "" {
		out, err := git(dir, "diff", "--relative", "--no-renames", "--name-status", r.Baseline, "--", "memory")
		if err != nil {
			return nil, err
		}
		for _, l := range lines(out) {
			f := strings.SplitN(l, "\t", 2)
			if len(f) != 2 {
				continue
			}
			status := map[string]string{"A": "added", "M": "modified", "D": "deleted"}[f[0][:1]]
			if status == "" {
				status = "modified"
			}
			add(f[1], status)
		}
	} else {
		// NEVER REVIEWED: THE WHOLE HISTORY IS IN SCOPE, which is every file
		// the folder holds now.
		out, err := git(dir, "ls-files", "--", "memory")
		if err != nil {
			return nil, err
		}
		for _, f := range lines(out) {
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f))); err == nil {
				add(f, "unreviewed")
			}
		}
	}
	// And whatever is not in git yet, in both cases.
	out, err := git(dir, "ls-files", "--others", "--exclude-standard", "--", "memory")
	if err != nil {
		return nil, err
	}
	for _, f := range lines(out) {
		add(f, "untracked")
	}
	sort.Slice(r.Lessons, func(i, j int) bool { return r.Lessons[i].File < r.Lessons[j].File })

	r.Rejections = newRejections(dir, r.Baseline)
	r.Tally = s.tally(id)
	rel, _ := git(dir, "rev-parse", "--show-prefix")
	rel = strings.TrimSuffix(filepath.ToSlash(rel), "/")
	if rel == "" {
		rel = "."
	}
	r.Commit = fmt.Sprintf("personas: lessons review, %d promoted, %d kept, %d deleted\n\nLessons-reviewed: %s",
		r.Tally.Promoted, r.Tally.Kept, r.Tally.Deleted, id)
	r.Command = fmt.Sprintf("cd \"%s\"\ngit add -- \"%s\"\n"+
		"git commit -m \"personas: lessons review, %d promoted, %d kept, %d deleted\" -m \"Lessons-reviewed: %s\"\n"+
		"# then /safe-to-push before any push: memory quotes code from the repos these personas review",
		top, rel, r.Tally.Promoted, r.Tally.Kept, r.Tally.Deleted, id)
	return r, nil
}

// maxDiff bounds one lesson's diff on the wire. A lesson is a paragraph; a
// file bigger than this is not one, and the view says it was cut.
const maxDiff = 64 << 10

func (s *Service) diffOf(dir, base, file, status string) string {
	var out string
	if base != "" && status != "untracked" {
		out, _ = git(dir, "diff", "--relative", "--no-renames", base, "--", file)
	}
	if out == "" && status != "deleted" {
		// Untracked, or never reviewed: the whole file is new to the reviewer.
		raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(file)))
		if err == nil {
			var b strings.Builder
			for _, l := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
				b.WriteString("+" + l + "\n")
			}
			out = b.String()
		}
	}
	if len(out) > maxDiff {
		out = out[:maxDiff] + "\n[cut: the diff is longer than " + fmt.Sprint(maxDiff) + " bytes]\n"
	}
	return out
}

// newRejections is the lines of memory/rejected.md that the baseline did not
// have. Counted, so a line that appears twice now and once before is new once.
func newRejections(dir, base string) []string {
	raw, err := os.ReadFile(filepath.Join(dir, "memory", "rejected.md"))
	if err != nil {
		return []string{}
	}
	old := map[string]int{}
	if base != "" {
		if prev, err := git(dir, "show", base+":./memory/rejected.md"); err == nil {
			for _, l := range strings.Split(strings.ReplaceAll(prev, "\r\n", "\n"), "\n") {
				old[strings.TrimSpace(l)]++
			}
		}
	}
	out := []string{}
	for _, l := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(l)
		if !strings.HasPrefix(t, "- ") {
			continue
		}
		if old[t] > 0 {
			old[t]--
			continue
		}
		out = append(out, t)
	}
	return out
}

// lessonMeta is what a memory file says about itself.
type lessonMeta struct{ Name, Description, Repo, Why string }

var (
	fmLine  = regexp.MustCompile(`^\s*(name|description|repo):\s*(.*?)\s*$`)
	whyLine = regexp.MustCompile(`^\s*\**Why\**:\**\s*(.+?)\s*$`)
)

// parseLesson reads a memory file's frontmatter and its `Why:` line. Loose on
// purpose: the files are model-written, and `repo:` may sit at the top level
// or under `metadata:`.
func parseLesson(s string) lessonMeta {
	var m lessonMeta
	s = strings.ReplaceAll(s, "\r\n", "\n")
	body := s
	if fm, rest, ok := splitFrontmatter(s); ok {
		body = rest
		for _, l := range strings.Split(fm, "\n") {
			if g := fmLine.FindStringSubmatch(l); g != nil {
				v := strings.Trim(g[2], `"'`)
				switch g[1] {
				case "name":
					if m.Name == "" {
						m.Name = v
					}
				case "description":
					if m.Description == "" {
						m.Description = v
					}
				case "repo":
					m.Repo = v
				}
			}
		}
	}
	for _, l := range strings.Split(body, "\n") {
		if g := whyLine.FindStringSubmatch(l); g != nil {
			m.Why = g[1]
			break
		}
	}
	return m
}

// splitFrontmatter returns the yaml between the opening and closing `---`, and
// everything after.
func splitFrontmatter(s string) (fm, rest string, ok bool) {
	if !strings.HasPrefix(s, "---\n") {
		return "", s, false
	}
	end := strings.Index(s[4:], "\n---")
	if end < 0 {
		return "", s, false
	}
	fm = s[4 : 4+end]
	rest = s[4+end+4:]
	rest = strings.TrimPrefix(rest, "\n")
	return fm, rest, true
}

// Decision is one button pressed in the lessons view.
type Decision struct {
	File   string `json:"file"`
	Action string `json:"action"`
	// Repo, Why and Text fill in what the lesson does not say. Repo picks the
	// knowledge file a promote writes to and is the `repo:` line a keep adds.
	// Why is the `Why:` line a keep adds, and the reason a promote records.
	// Text replaces the promoted bullet, which is otherwise the description.
	Repo string `json:"repo,omitempty"`
	Why  string `json:"why,omitempty"`
	Text string `json:"text,omitempty"`
}

// Apply makes one decision, in the dotagents working tree and nowhere else.
//
// The file has to be one the view is showing right now, which is the first
// check: a caller cannot name an arbitrary path and have it deleted. The
// second is safepath, against the pack path, on every file read or written.
func (s *Service) Apply(pack, id string, d Decision) (*Review, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.read(pack, id)
	if err != nil {
		return nil, err
	}
	var l *Lesson
	for i := range r.Lessons {
		if r.Lessons[i].File == d.File {
			l = &r.Lessons[i]
		}
	}
	if l == nil {
		return nil, fmt.Errorf("%s is not a lesson under review for %s", d.File, id)
	}
	if l.Status == "deleted" {
		return nil, fmt.Errorf("%s is already deleted. the commit records that", d.File)
	}
	e := editor{pack: pack, dir: filepath.Join(filepath.FromSlash(pack), id)}
	switch d.Action {
	case "promote":
		err = e.promote(*l, d)
	case "keep":
		err = e.keep(*l, d)
	case "delete":
		err = e.remove(*l)
	default:
		return nil, fmt.Errorf("%q is not promote, keep or delete", d.Action)
	}
	if err != nil {
		return nil, err
	}
	t := s.tallies[id]
	if t == nil {
		t = &Tally{}
		s.tallies[id] = t
	}
	switch d.Action {
	case "promote":
		t.Promoted++
	case "keep":
		t.Kept++
	case "delete":
		t.Deleted++
	}
	return s.read(pack, id)
}

// editor writes inside one persona folder, through safepath against the pack.
type editor struct{ pack, dir string }

// path resolves a file relative to the persona folder, refusing anything that
// lands outside the pack.
func (e editor) path(rel string) (string, error) {
	full := filepath.Join(e.dir, filepath.FromSlash(rel))
	if err := noLinksBelow(filepath.FromSlash(e.pack), full); err != nil {
		return "", err
	}
	return safepath.Contained(e.pack, full)
}

// noLinksBelow refuses a path with a link anywhere between the pack and it.
//
// safepath resolves symlinks, and that is not enough on Windows: since Go 1.23
// filepath.EvalSymlinks does not follow a junction, which any user can make
// without privilege, so a junction out of the pack passes its check. Nothing
// in a persona folder needs to be a link, so any link or reparse point on the
// way is refused rather than followed.
func noLinksBelow(root, full string) error {
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return safepath.ErrOutside
	}
	cur := root
	for _, seg := range strings.Split(rel, string(filepath.Separator)) {
		if seg == "" || seg == "." {
			continue
		}
		cur = filepath.Join(cur, seg)
		fi, err := os.Lstat(cur)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return fmt.Errorf("%s is a link, and the lessons view does not write through one: %w",
				filepath.ToSlash(cur), safepath.ErrOutside)
		}
	}
	return nil
}

func (e editor) promote(l Lesson, d Decision) error {
	repo := strings.TrimSpace(firstNonEmpty(d.Repo, l.Repo))
	if repo == "" {
		return errors.New("this lesson has no repo: line. say which repo it is about, or general")
	}
	if !ValidRepoKey(repo) {
		return fmt.Errorf("%q is not a repo key like github/org/repo, or general", repo)
	}
	text := strings.TrimSpace(firstNonEmpty(d.Text, l.Description, l.Name))
	if text == "" {
		return errors.New("this lesson has no description to promote. give the text")
	}
	why := strings.TrimSpace(firstNonEmpty(d.Why, l.Why))
	bullet := "- " + oneLine(text)
	if why != "" {
		bullet += " Why: " + oneLine(why)
	}

	kf, err := e.path(KnowledgeFile(repo))
	if err != nil {
		return err
	}
	cur, err := os.ReadFile(kf)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	next := string(cur)
	if len(cur) == 0 {
		title := repo
		if repo == "general" {
			title = "general"
		}
		next = "# " + title + "\n\nReviewed facts, promoted from memory by a lessons review. They win a conflict " +
			"with memory.\n\n"
	} else if !strings.HasSuffix(next, "\n") {
		next += "\n"
	}
	next += bullet + "\n"
	if err := os.MkdirAll(filepath.Dir(kf), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(kf, []byte(next), 0o644); err != nil {
		return err
	}
	return e.remove(l)
}

func (e editor) keep(l Lesson, d Decision) error {
	f, err := e.path(l.File)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(f)
	if err != nil {
		return err
	}
	s := strings.ReplaceAll(string(raw), "\r\n", "\n")
	var missing []string
	if l.Repo == "" {
		repo := strings.TrimSpace(d.Repo)
		switch {
		case repo == "":
			missing = append(missing, "a repo")
		case !ValidRepoKey(repo):
			return fmt.Errorf("%q is not a repo key like github/org/repo, or general", repo)
		default:
			s = withRepoLine(s, repo)
		}
	}
	if l.Why == "" {
		why := strings.TrimSpace(d.Why)
		if why == "" {
			missing = append(missing, "a Why line")
		} else {
			if !strings.HasSuffix(s, "\n") {
				s += "\n"
			}
			s += "\n**Why:** " + oneLine(why) + "\n"
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("keeping this lesson adds what it is missing: %s", strings.Join(missing, " and "))
	}

	dest := f
	if l.Inbox {
		// AN INBOX LESSON THAT IS KEPT JOINS MEMORY, and gets its index line.
		// A runner without native memory never rewrites the index on its own,
		// which is why the review does it.
		base := filepath.Base(f)
		if dest, err = e.path("memory/" + base); err != nil {
			return err
		}
		if _, err := os.Stat(dest); err == nil {
			return fmt.Errorf("memory/%s already exists. rename the inbox file first", base)
		}
	}
	if err := os.WriteFile(dest, []byte(s), 0o644); err != nil {
		return err
	}
	if l.Inbox {
		if err := os.Remove(f); err != nil {
			return err
		}
		meta := parseLesson(s)
		return e.addIndexLine(filepath.Base(dest), firstNonEmpty(meta.Name, strings.TrimSuffix(filepath.Base(dest), ".md")),
			meta.Description)
	}
	return nil
}

func (e editor) remove(l Lesson) error {
	f, err := e.path(l.File)
	if err != nil {
		return err
	}
	if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
		return err
	}
	if l.Inbox {
		return nil
	}
	return e.dropIndexLine(filepath.Base(f))
}

// dropIndexLine takes the MEMORY.md line that links to file out of the index.
func (e editor) dropIndexLine(file string) error {
	idx, err := e.path("memory/MEMORY.md")
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(idx)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	crlf := strings.Contains(string(raw), "\r\n")
	src := strings.ReplaceAll(string(raw), "\r\n", "\n")
	var keep []string
	changed := false
	for _, l := range strings.Split(src, "\n") {
		if strings.Contains(l, "("+file+")") {
			changed = true
			continue
		}
		keep = append(keep, l)
	}
	if !changed {
		return nil
	}
	out := strings.Join(keep, "\n")
	if crlf {
		out = strings.ReplaceAll(out, "\n", "\r\n")
	}
	return os.WriteFile(idx, []byte(out), 0o644)
}

func (e editor) addIndexLine(file, name, desc string) error {
	idx, err := e.path("memory/MEMORY.md")
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(idx)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	s := string(raw)
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	line := "- [" + oneLine(name) + "](" + file + ")"
	if desc != "" {
		line += " " + oneLine(desc)
	}
	return os.WriteFile(idx, []byte(s+line+"\n"), 0o644)
}

// withRepoLine puts `repo: <key>` in a file's frontmatter, making one if it has
// none.
func withRepoLine(s, repo string) string {
	if fm, rest, ok := splitFrontmatter(s); ok {
		return "---\n" + fm + "\nrepo: " + repo + "\n---\n" + rest
	}
	return "---\nrepo: " + repo + "\n---\n\n" + s
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
