package api

import (
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

// List repositories under the allowed browse roots and ask git for their worktrees.
// Limit scan depth to keep large directory trees manageable.
//
// Creation uses a command template, defaulting to gwt, so the existing tool
// controls checkout layout. Run it in the repository and read the new path from git.

// SettingProjectDepth limits repository scan depth below each browse root.
// The default of two covers <root>/<org>/<repo>; roots can also be checkouts.
const SettingProjectDepth = "project_scan_depth"

// SettingWorktreeCommand configures worktree creation. The template accepts
// {branch} and {repo} and runs in the repository directory. An empty setting
// uses the default; "off" disables creation.
const SettingWorktreeCommand = "worktree_command"

// DefaultWorktreeCommand uses gwt new with -y because the daemon provides
// no stdin for confirmation prompts.
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
	// worktreeScanWorkers is how many of those run at once. A serial scan of
	// fifty repositories is fifty process spawns end to end, which on Windows
	// is the difference between a list that appears and a list that arrives.
	worktreeScanWorkers = 8
	// makeWorktreeTimeout allows time for cloning while bounding a stuck command.
	makeWorktreeTimeout = 3 * time.Minute
)

// project describes a repository and its existing worktrees.
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

// projectScanDepth returns the configured depth, falling back to the default
// when the value is unreadable or invalid.
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

// scanProjects walks each root to the depth limit, stopping at checkouts.
// Recognize both .git directories and the .git files used by worktrees.
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
			// Skip unreadable directories, consistent with the file picker.
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

// isCheckout, worktreesOf, parseWorktreeList, projectWorktree and
// worktreeListTimeout are shared with the providers and live in providers.go.

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

// legalBranch restricts branch names before they enter a shell command.
// Allow a conservative subset of git names, excluding shell metacharacters
// and leading dashes, without depending on one shell's quoting rules.
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

	// Return an existing worktree for this branch. Set existed so the board
	// can distinguish reuse from creation.
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

	// Read the resulting path from git; command output may contain colour,
	// progress messages, or wording that changes between versions.
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

// worktreeTemplate returns the stored command or the default. Use "off"
// to disable it because the store treats missing and empty values alike.
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

// shellCommand runs the template in the shell chosen by shellpick.
// Load its profile so functions such as gwt are available.
func shellCommand(line string) (string, []string) {
	shell, _ := shellpick.Pick()
	return shellArgsFor(shell, line)
}

// shellArgsFor selects command flags for a shell. Keep it separate from
// PATH lookup so tests do not depend on installed shells.
func shellArgsFor(shell, line string) (string, []string) {
	base := strings.ToLower(shellBase(shell))
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

// shellBase returns the last path component using either separator.
// filepath.Base follows the host OS, leaving Windows paths intact on Linux
// and causing cmd.exe to receive the wrong command flag.
func shellBase(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// tail keeps the final output lines, where commands usually report errors.
func tail(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\r\n"), "\n")
	if len(lines) > 12 {
		lines = lines[len(lines)-12:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
