package daemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// The persona pack nag. See docs/personas-design.md, "Backup: commit and push
// are clint's" and "What atrium does, and what it does not".
//
// The pack is a directory of personas inside a git checkout the operator owns
// (dotagents). Reviews change its memory files, and nothing commits or pushes
// them for him. So the room looks at the checkout once a minute and the board
// says, on the stuck-agent backoff, how many files are not committed and how
// many commits are not pushed.
//
// OFF UNTIL A PATH IS SET. `persona_pack_path` empty means nothing here runs a
// command at all.
//
// A FAILURE IS A LINE ON THE BOARD AND NOTHING MORE. A path that is gone, a
// directory that is not a checkout, a git that is missing or hangs: each reads
// as "cannot read the persona pack: <why>" and the next tick tries again. None
// of them rings, and none of them touches anything else the daemon does.

// PersonaPackEvery is how often the pack is read. Its own ticker rather than
// the reaper's, because a git call can take up to PersonaPackTimeout and the
// reaper's tick is liveness for every card.
var PersonaPackEvery = envDuration("ATRIUM_PERSONA_PACK_EVERY", time.Minute)

// PersonaPackTimeout bounds each git call. A variable so a test can make it
// expire.
var PersonaPackTimeout = 10 * time.Second

// packGit is the git binary. A variable so a test can name one that is not
// there.
var packGit = "git"

// packReads is EVERY git command atrium runs against the persona pack, and it
// is an allow-list: packGitRead refuses any name not in it.
//
// ATRIUM NEVER RUNS A GIT COMMAND THAT WRITES, NEVER PUSHES, AND HOLDS NO
// CREDENTIAL. The commit and the push are the operator's. Both commands below
// only read, and `GIT_OPTIONAL_LOCKS=0` (see packGitRead) stops `git status`
// from refreshing the index as a side effect, which is the one write it would
// otherwise do on its own. `GIT_TERMINAL_PROMPT=0` means a git that wants a
// credential fails instead of waiting for one. Neither command talks to a
// remote: `@{u}` is the remote-tracking ref already on disk.
//
// Anything added here has to be a read. If it is not, it does not belong in
// atrium.
var packReads = map[string][]string{
	// Every changed or new file under the pack, one per entry. `-z` so a path
	// with a space is not quoted, and `all` so each new memory file counts
	// rather than its new directory counting once.
	"status": {"status", "--porcelain=v1", "-z", "--untracked-files=all", "--", "."},
	// How many commits the branch has that its upstream does not.
	"ahead": {"rev-list", "--count", "@{u}..HEAD"},
}

// errNoUpstream is the branch having nothing to compare against. Not a failure:
// the board says "no upstream configured" where the count would go.
var errNoUpstream = errors.New("no upstream configured")

// packGitRead runs one allow-listed read in dir and returns its stdout.
func packGitRead(ctx context.Context, dir, which string) ([]byte, error) {
	args, ok := packReads[which]
	if !ok {
		return nil, fmt.Errorf("%q is not a persona pack read", which)
	}
	ctx, cancel := context.WithTimeout(ctx, PersonaPackTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, packGit, append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	hideWindow(cmd)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	if err == nil {
		return out.Bytes(), nil
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("git did not answer within %s", PersonaPackTimeout)
	}
	if errors.Is(err, exec.ErrNotFound) {
		return nil, errors.New("git is not installed or not on PATH")
	}
	msg := strings.TrimSpace(errOut.String())
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "no upstream"):
		return nil, errNoUpstream
	case strings.Contains(low, "not a git repository"):
		return nil, fmt.Errorf("%s is not in a git checkout", dir)
	}
	if msg == "" {
		msg = err.Error()
	}
	// First line only. git's advice follows on the lines after, and the board
	// has room for the reason, not the essay.
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	return nil, errors.New(strings.TrimPrefix(msg, "fatal: "))
}

// packRead is what one look at the pack found.
type packRead struct {
	// Repo is the checkout holding the pack, where /safe-to-push is run.
	Repo string
	// Changed is each porcelain entry: two status letters, a space, the path
	// relative to the checkout.
	Changed []string
	// Personas are the pack directories the changed files are in.
	Personas   []string
	Unpushed   int
	NoUpstream bool
}

// readPack looks at the pack once. The error is the reason the board shows.
func readPack(ctx context.Context, path string) (packRead, error) {
	var r packRead
	path = filepath.Clean(filepath.FromSlash(strings.TrimSpace(path)))
	if fi, err := os.Stat(path); err != nil {
		return r, fmt.Errorf("%s does not exist", filepath.ToSlash(path))
	} else if !fi.IsDir() {
		return r, fmt.Errorf("%s is not a directory", filepath.ToSlash(path))
	}
	out, err := packGitRead(ctx, path, "status")
	if err != nil {
		return r, err
	}
	r.Repo, r.Changed = checkoutOf(path), porcelainEntries(out)
	r.Personas = personasOf(r.Changed, r.Repo, path)
	out, err = packGitRead(ctx, path, "ahead")
	switch {
	case errors.Is(err, errNoUpstream):
		r.NoUpstream = true
	case err != nil:
		return r, err
	default:
		n, perr := strconv.Atoi(strings.TrimSpace(string(out)))
		if perr != nil {
			return r, fmt.Errorf("git answered %q for the unpushed count", strings.TrimSpace(string(out)))
		}
		r.Unpushed = n
	}
	return r, nil
}

// porcelainEntries splits `status --porcelain=v1 -z` output. A rename or copy
// carries its old path as the next entry, which is skipped: it is one change.
func porcelainEntries(out []byte) []string {
	var got []string
	parts := strings.Split(string(out), "\x00")
	for i := 0; i < len(parts); i++ {
		e := parts[i]
		if len(e) < 4 {
			continue
		}
		got = append(got, e)
		if e[0] == 'R' || e[0] == 'C' {
			i++
		}
	}
	return got
}

// checkoutOf finds the checkout holding dir by looking for `.git` on the way
// up, so there is no third git command. A worktree's `.git` is a file, which
// counts the same. Empty when there is none, which git will already have said.
func checkoutOf(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for d := abs; ; {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return d
		}
		up := filepath.Dir(d)
		if up == d {
			return ""
		}
		d = up
	}
}

// personasOf names the pack directories the changed files sit in. Porcelain
// paths are relative to the checkout, so the pack's own place in the checkout
// is stripped first and the next segment is the persona id. A file at the top
// of the pack, the README say, belongs to no persona.
func personasOf(entries []string, repo, pack string) []string {
	if repo == "" {
		return nil
	}
	abs, err := filepath.Abs(pack)
	if err != nil {
		return nil
	}
	prefix, err := filepath.Rel(repo, abs)
	if err != nil {
		return nil
	}
	prefix = filepath.ToSlash(prefix)
	if prefix == "." {
		prefix = ""
	} else {
		prefix += "/"
	}
	seen := map[string]bool{}
	var ids []string
	for _, e := range entries {
		p := e[3:]
		if !strings.HasPrefix(strings.ToLower(p), strings.ToLower(prefix)) {
			continue
		}
		rest := p[len(prefix):]
		i := strings.IndexByte(rest, '/')
		if i <= 0 {
			continue
		}
		if id := rest[:i]; !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// PackView is the nag as the board reads it: in `GET /v1/settings` under
// `persona_pack`, and pushed as a `persona-pack` event whenever it changes.
type PackView struct {
	Path string `json:"path"`
	Repo string `json:"repo,omitempty"`
	// Error is why the pack could not be read. Set, and nothing else is.
	Error       string   `json:"error,omitempty"`
	Uncommitted int      `json:"uncommitted"`
	Unpushed    int      `json:"unpushed"`
	NoUpstream  bool     `json:"no_upstream,omitempty"`
	Personas    []string `json:"personas,omitempty"`
	// Nag is whether there is anything to back up.
	Nag bool `json:"nag"`
	// Key names this exact state. It changes when the state does, which is
	// what resets the backoff, and the board keys a snooze on it.
	Key string `json:"key,omitempty"`
	// Since is when this state was first seen. The backoff counts from here.
	Since *time.Time `json:"since,omitempty"`
	// Count is how many steps of the backoff have passed. The board rings each
	// time it goes up.
	Count   int       `json:"count"`
	Text    string    `json:"text"`
	Detail  string    `json:"detail,omitempty"`
	Checked time.Time `json:"checked"`
}

type packNag struct {
	mu    sync.Mutex
	view  *PackView
	nudge chan struct{}
}

// packViewFor turns one read into what the board shows. prev carries the
// backoff across ticks: the same key keeps its Since, a new one starts over.
func packViewFor(path string, r packRead, err error, prev *PackView, now time.Time) *PackView {
	v := &PackView{Path: filepath.ToSlash(path), Checked: now}
	if err != nil {
		v.Error = err.Error()
		v.Text = "cannot read the persona pack: " + v.Error
		return v
	}
	v.Repo = filepath.ToSlash(r.Repo)
	v.Uncommitted, v.Unpushed, v.NoUpstream, v.Personas = len(r.Changed), r.Unpushed, r.NoUpstream, r.Personas
	v.Nag = v.Uncommitted > 0 || v.Unpushed > 0 || v.NoUpstream
	if !v.Nag {
		v.Text = "persona pack: committed and pushed"
		return v
	}
	var parts []string
	if v.Uncommitted > 0 {
		parts = append(parts, plural(v.Uncommitted, "file")+" not committed")
	}
	if v.NoUpstream {
		parts = append(parts, "no upstream configured")
	} else if v.Unpushed > 0 {
		parts = append(parts, plural(v.Unpushed, "commit")+" not pushed")
	}
	v.Text = "persona pack: " + strings.Join(parts, ", ")
	var detail []string
	if len(v.Personas) > 0 {
		detail = append(detail, "changed: "+strings.Join(v.Personas, ", ")+".")
	}
	where := v.Repo
	if where == "" {
		where = v.Path
	}
	detail = append(detail, "atrium never commits or pushes this. before you push, run /safe-to-push in "+where+".")
	v.Detail = strings.Join(detail, " ")
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%t|%s",
		v.Path, v.Unpushed, v.NoUpstream, strings.Join(r.Changed, "\x00"))))
	v.Key = hex.EncodeToString(sum[:8])
	since := now
	if prev != nil && prev.Key == v.Key && prev.Since != nil {
		since = *prev.Since
	}
	v.Since = &since
	v.Count = escalationStep(now.Sub(since))
	return v
}

// samePack reports whether the board would see no difference.
func samePack(a, b *PackView) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Key == b.Key && a.Count == b.Count && a.Text == b.Text && a.Error == b.Error
}

// personaPackView is what `GET /v1/settings` carries. Nil when the nag is off.
func (d *Daemon) personaPackView() any {
	d.pack.mu.Lock()
	defer d.pack.mu.Unlock()
	if d.pack.view == nil {
		return nil
	}
	out := *d.pack.view
	return &out
}

// nudgePack asks for a look now rather than on the next tick. Called when the
// path setting is saved, so the board answers the save rather than a minute
// after it.
func (d *Daemon) nudgePack() {
	select {
	case d.pack.nudge <- struct{}{}:
	default:
	}
}

// checkPack reads the pack once, and tells the board when what it would show
// changed.
func (d *Daemon) checkPack(ctx context.Context, now time.Time) {
	path, err := d.st.Setting(store.SettingPersonaPackPath)
	path = strings.TrimSpace(path)
	var next *PackView
	switch {
	case err != nil:
		next = &PackView{Checked: now, Error: err.Error(), Text: "cannot read the persona pack: " + err.Error()}
	case path != "":
		r, rerr := readPack(ctx, path)
		d.pack.mu.Lock()
		prev := d.pack.view
		d.pack.mu.Unlock()
		next = packViewFor(path, r, rerr, prev, now)
	}
	d.pack.mu.Lock()
	changed := !samePack(d.pack.view, next)
	d.pack.view = next
	d.pack.mu.Unlock()
	if changed {
		if next == nil {
			d.ap.Broadcast("persona-pack", map[string]any{"off": true})
		} else {
			d.ap.Broadcast("persona-pack", next)
		}
	}
}

// watchPack is the timer. It never runs on a request.
func (d *Daemon) watchPack(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = PersonaPackEvery
	}
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		d.checkPack(ctx, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-d.pack.nudge:
		}
	}
}
