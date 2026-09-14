package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/shellpick"
)

// The repositories on this machine, and the worktrees that already exist for
// each, so work can start from the board instead of from a directory tree.
//
// ATRIUM RUNS `gwt`. IT DOES NOT REIMPLEMENT IT, and that is the whole design
// rather than a detail of it. `internal/daemon/recognise.go` says of the
// recogniser path that making a worktree is `gwt`'s job and atrium has no
// business owning a checkout layout, and that stays true here. Running
// `git worktree add` from the daemon would mean atrium deciding where a
// worktree lives, what it is called, and what happens after it is made, and
// the layout on this machine is a convention that another tool maintains.
//
// So the creation half is a COMMAND TEMPLATE, the way a harness and a source
// already are. Atrium holds the name of a command and never the thing behind
// it. It supplies the branch, runs the template in the repository, and reads
// the resulting path back out of git rather than out of the command's output.
//
// The listing half owns no layout either:
//
//   - The repositories come from the directories the picker is already allowed
//     to open, `browseRootsFor`, walked a fixed depth rather than recursively.
//     A walk of a drive looking for every `.git` is a scan somebody waits on.
//   - The worktrees come from `git worktree list`, asked of each repository.
//     That is git's own answer, so a machine that keeps its worktrees
//     somewhere else is still described correctly, which a path convention
//     spelled out here could not manage.

// SettingProjectDepth is how many levels under a browse root a repository may
// be found.
//
// Two, because the convention on this machine is `<root>/<org>/<repo>` and a
// root IS sometimes the checkout itself. A setting rather than a constant
// because it is a fact about one machine's layout, exactly like the roots it
// walks, and the machine that nests one level deeper should not need a build.
const SettingProjectDepth = "project_scan_depth"

// SettingWorktreeCommand is the command that makes a worktree.
//
// Unlike `editor_command` and `terminal_command` this one HAS a default,
// because it is not atrium picking a program to run on your machine: it is
// atrium naming the tool that already owns this job on the machine it was
// built for. Clearing it turns the make button off the same way an empty
// editor turns the open button off.
//
// `{branch}` is the branch, `{repo}` is the repository's directory. A template
// with neither still runs, in the repository, which is enough for a tool that
// asks for its arguments.
const SettingWorktreeCommand = "worktree_command"

// DefaultWorktreeCommand is `gwt new`, with the confirmation already answered.
//
// `-y` IS PART OF THE DEFAULT AND NOT AN OPINION ABOUT PROMPTS. The daemon
// gives the command no stdin, so a template that stops to ask a question is a
// template that hangs until the timeout and then reports a failure that reads
// like the tool is broken.
const DefaultWorktreeCommand = "gwt new {branch} -y"

const (
	// defaultProjectDepth is the convention: `<root>/<org>/<repo>`.
	defaultProjectDepth = 2
	// maxProjectDepth is where "a bounded scan" stops being true. Four levels
	// of a directory of any size is already a wait.
	maxProjectDepth = 4
	// maxProjects and maxScanDirs bound the walk WHILE it reads rather than
	// after it, so a root pointed at something enormous costs a moment rather
	// than the whole of it.
	maxProjects = 400
	maxScanDirs = 4000
	// worktreeListTimeout is per repository. `git worktree list` is a read of
	// a file in `.git`, so a second is generous, and a repository on a network
	// share that cannot answer in one must not hold the whole scan.
	worktreeListTimeout = 2 * time.Second
	// worktreeScanWorkers is how many of those run at once. A serial scan of
	// fifty repositories is fifty process spawns end to end, which on Windows
	// is the difference between a list that appears and a list that arrives.
	worktreeScanWorkers = 8
	// makeWorktreeTimeout bounds the template. `gwt new` clones the repository
	// when it is missing, so this is minutes rather than seconds, and it is
	// here at all because a daemon holding a request open forever is how one
	// wedged command takes the board with it.
	makeWorktreeTimeout = 3 * time.Minute
)

// projectWorktree is one worktree that already exists for a repository.
type projectWorktree struct {
	Branch string `json:"branch"`
	Path   string `json:"path"`
}

// project is one repository, with what already exists for it.
//
// `Worktrees` carries the answer to the second question somebody asks a moment
// after the first: "make me a worktree" and "take me to the one I already
// have" are the same question, and a row that only answers the first sends
// them back to the picker.
type project struct {
	Name string `json:"name"`
	// Group is the directory above the repository, which on this machine is
	// the org. Named for what it is rather than what it usually holds, since
	// nothing here requires a root to be arranged by org.
	Group     string            `json:"group"`
	Path      string            `json:"path"`
	Worktrees []projectWorktree `json:"worktrees"`
}

// listProjects is the scan.
func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	roots := s.browseRootsFor()
	depth := projectScanDepth(s)

	repos, truncated := scanProjects(roots, depth)
	fillWorktrees(repos)

	tmpl, _ := s.st.Setting(SettingWorktreeCommand)
	out := make([]project, 0, len(repos))
	for _, p := range repos {
		out = append(out, *p)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"projects":  out,
		"roots":     roots,
		"depth":     depth,
		"truncated": truncated,
		// What the make button will run, so the form can say so and can turn
		// itself off when the template has been cleared.
		"worktree_command": worktreeTemplate(tmpl),
	})
}

// projectScanDepth reads the setting, with the convention as the default.
//
// An unreadable or nonsensical value is the default rather than an error. This
// is a bound on a scan, not a permission, and refusing to list anything
// because a number was typed badly helps nobody.
func projectScanDepth(s *Server) int {
	v, err := s.st.Setting(SettingProjectDepth)
	if err != nil {
		return defaultProjectDepth
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 1 {
		return defaultProjectDepth
	}
	if n > maxProjectDepth {
		return maxProjectDepth
	}
	return n
}

// scanProjects walks each root looking for checkouts, and stops descending as
// soon as it finds one.
//
// A worktree has a `.git` too, as a FILE rather than a directory, and both are
// checkouts as far as this is concerned: pointing the launch form at either is
// a working directory. What stops the walk turning a worktree root into
// hundreds of rows is the depth, which is the same bound that keeps it quick.
func scanProjects(roots []string, depth int) ([]*project, bool) {
	seen := map[string]bool{}
	var found []*project
	visited := 0
	truncated := false

	var walk func(dir string, level int)
	walk = func(dir string, level int) {
		if truncated {
			return
		}
		if visited++; visited > maxScanDirs {
			truncated = true
			return
		}
		if isCheckout(dir) {
			key := strings.ToLower(filepath.Clean(dir))
			if !seen[key] {
				seen[key] = true
				if len(found) >= maxProjects {
					truncated = true
					return
				}
				found = append(found, &project{
					Name:  filepath.Base(dir),
					Group: filepath.Base(filepath.Dir(dir)),
					Path:  filepath.ToSlash(dir),
				})
			}
			// Not descended into. A checkout's subdirectories are its source,
			// and the one that is not is `.git`, which is skipped below.
			return
		}
		if level >= depth {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			// Silently. A root that cannot be read is a root that contributes
			// nothing, and it is not this endpoint's job to say why: the
			// picker gives one answer for outside, missing and unreadable for
			// the same reason.
			return
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			walk(filepath.Join(dir, e.Name()), level+1)
			if truncated {
				return
			}
		}
	}

	for _, root := range roots {
		walk(root, 0)
	}
	sort.Slice(found, func(i, j int) bool {
		if !strings.EqualFold(found[i].Group, found[j].Group) {
			return strings.ToLower(found[i].Group) < strings.ToLower(found[j].Group)
		}
		return strings.ToLower(found[i].Name) < strings.ToLower(found[j].Name)
	})
	return found, truncated
}

// isCheckout reports whether a directory holds a `.git`, either kind.
func isCheckout(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// fillWorktrees asks git about every repository, several at a time.
func fillWorktrees(repos []*project) {
	if len(repos) == 0 {
		return
	}
	workers := worktreeScanWorkers
	if len(repos) < workers {
		workers = len(repos)
	}
	ch := make(chan *project)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range ch {
				p.Worktrees = worktreesOf(p.Path)
			}
		}()
	}
	for _, p := range repos {
		ch <- p
	}
	close(ch)
	wg.Wait()
}

// worktreesOf asks git what worktrees a repository has, and drops the
// repository itself.
//
// The main checkout is the first entry git lists and it is not an answer to
// "where could I work": it is the directory that was already on the row. A
// detached worktree has no branch to offer, so it is listed under its own
// directory name, which is what somebody would recognise it by anyway.
func worktreesOf(repo string) []projectWorktree {
	ctx, cancel := context.WithTimeout(context.Background(), worktreeListTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "worktree", "list", "--porcelain")
	cmd.Dir = filepath.FromSlash(repo)
	out, err := cmd.Output()
	if err != nil {
		// A repository git will not talk about contributes no worktrees. It is
		// still a repository and still a directory to start in, so this is not
		// a reason to drop the row.
		return nil
	}
	return parseWorktreeList(string(out), repo)
}

// parseWorktreeList reads `--porcelain`, which is the stable one: the human
// format puts the branch in brackets and has changed its mind about spacing.
func parseWorktreeList(out, repo string) []projectWorktree {
	var found []projectWorktree
	var cur projectWorktree
	flush := func() {
		if cur.Path != "" && !eqPath(filepath.FromSlash(cur.Path), filepath.FromSlash(repo)) {
			if cur.Branch == "" {
				cur.Branch = filepath.Base(filepath.FromSlash(cur.Path))
			}
			found = append(found, cur)
		}
		cur = projectWorktree{}
	}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur.Path = filepath.ToSlash(strings.TrimPrefix(line, "worktree "))
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(
				strings.TrimPrefix(line, "branch "), "refs/heads/")
		}
	}
	flush()
	sort.Slice(found, func(i, j int) bool {
		return strings.ToLower(found[i].Branch) < strings.ToLower(found[j].Branch)
	})
	return found
}

// legalBranch is the fence on the one piece of this that reaches a shell.
//
// `editor_command` can split its template into a program and arguments because
// the part it does not control is a FILENAME and a filename is data. This
// cannot: `gwt` is a PowerShell function rather than a program on PATH, so the
// template has to be hosted by a shell, and in a shell every character of the
// branch name is live.
//
// So the branch is checked against what a branch may contain BEFORE it reaches
// the line, rather than quoted afterwards. Quoting is a claim about one shell's
// grammar and this runs under three. A name outside this set is refused, which
// costs somebody an exotic branch name and takes the injection away entirely.
var legalBranch = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,199}$`)

type makeWorktreeRequest struct {
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
}

// makeWorktree runs the template, and answers with the directory to work in.
func (s *Server) makeWorktree(w http.ResponseWriter, r *http.Request) {
	var body makeWorktreeRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("could not read that request"))
		return
	}

	// The same bound as the picker, and the same silence about what is outside
	// it. A repository reached through this is a repository atrium was already
	// allowed to list.
	repo, ok := insideARoot(s.browseRootsFor(), filepath.Clean(filepath.FromSlash(body.Repo)))
	if !ok {
		writeErr(w, http.StatusForbidden, errOutsideRoots)
		return
	}
	if !isCheckout(repo) {
		writeErr(w, http.StatusBadRequest, errors.New("that directory is not a checkout"))
		return
	}

	branch := strings.TrimSpace(body.Branch)
	if !legalBranch.MatchString(branch) {
		writeErr(w, http.StatusBadRequest, errors.New(
			"a branch name here may hold letters, digits, and `. _ - /`, and must start "+
				"with a letter or a digit. it is put on a command line, so the set is the "+
				"set rather than a preference"))
		return
	}

	// ALREADY THERE IS NOT AN ERROR, it is the answer. The common case for
	// "make me a worktree for this branch" is that one exists, and what the
	// person wants then is to go to it. Reported as `existed` so the board can
	// say which of the two happened.
	if wt := findWorktree(repo, branch); wt != "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"path": wt, "branch": branch, "existed": true,
		})
		return
	}

	tmpl, err := s.st.Setting(SettingWorktreeCommand)
	if err != nil {
		s.fail(w, err)
		return
	}
	tmpl = worktreeTemplate(tmpl)
	if tmpl == "" {
		writeErr(w, http.StatusBadRequest, errors.New(
			"no worktree command is configured. set `worktree_command` in settings, "+
				"for example `"+DefaultWorktreeCommand+"`"))
		return
	}

	line := strings.ReplaceAll(tmpl, "{branch}", branch)
	line = strings.ReplaceAll(line, "{repo}", filepath.FromSlash(repo))

	ctx, cancel := context.WithTimeout(r.Context(), makeWorktreeTimeout)
	defer cancel()
	name, args := shellCommand(line)
	cmd := exec.CommandContext(ctx, name, args...)
	// IN the repository, because the tool works out which repository it is in
	// from where it was run. That is also why the default template does not
	// mention `{repo}` at all.
	cmd.Dir = filepath.FromSlash(repo)
	// No stdin. A template that stops to ask something gets an immediate end
	// of file rather than a wait, which turns a hang into an error with the
	// question in it.
	cmd.Stdin = nil
	out, runErr := cmd.CombinedOutput()

	// READ BACK FROM GIT, NOT FROM THE OUTPUT. What the tool prints is for a
	// person: it is coloured, it is several lines, and its wording is not a
	// promise. Where the worktree ended up is a question git answers exactly,
	// and asking it also means a template doing something entirely different
	// still works as long as a worktree comes out of it.
	if wt := findWorktree(repo, branch); wt != "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"path": wt, "branch": branch, "existed": false,
			"output": tail(string(out)),
		})
		return
	}
	if runErr != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf(
			"%s did not make a worktree: %v\n%s", name, runErr, tail(string(out))))
		return
	}
	writeErr(w, http.StatusBadRequest, errors.New(
		"the command finished and git still has no worktree for "+branch+"\n"+
			tail(string(out))))
}

// findWorktree is the read back: which directory holds this branch, if any.
func findWorktree(repo, branch string) string {
	for _, wt := range worktreesOf(repo) {
		if wt.Branch == branch {
			return wt.Path
		}
	}
	return ""
}

// worktreeTemplate is the stored value, with the default where nothing is
// stored, and `off` as the way to mean nothing.
//
// `off` RATHER THAN EMPTY, because a setting read out of a store cannot tell
// "never set" from "set to nothing", and those have to be different answers
// here: this is the one command template with a default, so an empty box has
// to keep meaning the default. `off` is the same spelling the housekeeping
// timers already use for the same shape of question.
func worktreeTemplate(stored string) string {
	v := strings.TrimSpace(stored)
	switch v {
	case "":
		return DefaultWorktreeCommand
	case "off":
		return ""
	}
	return v
}

// shellCommand wraps a command line in a shell that can host it.
//
// A SHELL, DELIBERATELY, AND FOR ONE REASON. `gwt` is a PowerShell function
// defined in a profile rather than an executable on PATH, so there is nothing
// for `exec.Command` to spawn. `internal/shellpick` already decided which
// shell this machine has, with the echelon that matters on Windows: `pwsh` is
// PowerShell 7 and a separate install, `powershell` is 5.1 and is on every
// Windows, `cmd` is the floor. That answer is reused rather than asked again,
// because a second chooser is how two parts of one program disagree.
//
// The profile is LOADED rather than skipped, which is the opposite of what
// `-NoProfile` would give and is the whole point: the profile is where the
// function being run is defined.
func shellCommand(line string) (string, []string) {
	shell, _ := shellpick.Pick()
	return shellArgsFor(shell, line)
}

// shellArgsFor is the flag that means "and here is the line", per shell.
//
// Split out because the choice is worth checking and the chooser is not: which
// shell this machine has is a search of its PATH, and a test that asks would
// be a test of the machine it ran on.
func shellArgsFor(shell, line string) (string, []string) {
	base := strings.ToLower(filepath.Base(shell))
	switch {
	case strings.HasPrefix(base, "pwsh"), strings.HasPrefix(base, "powershell"):
		return shell, []string{"-NoLogo", "-Command", line}
	case strings.HasPrefix(base, "cmd"):
		return shell, []string{"/c", line}
	default:
		if runtime.GOOS == "windows" {
			return shell, []string{"-Command", line}
		}
		return shell, []string{"-c", line}
	}
}

// tail keeps the last few lines of a command's output, for the error.
//
// The end rather than the beginning: a tool that failed says why last, after
// whatever it was doing when it got there.
func tail(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\r\n"), "\n")
	if len(lines) > 12 {
		lines = lines[len(lines)-12:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
