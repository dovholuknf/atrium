package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/base/safepath"
	"github.com/dovholuknf/atrium/internal/store"
)

// STAMPING A RECORD (item lifecycle L4). `atrium_record` is the hub's tool, and the hub cannot see a card's worktree, so it asks
// the room that holds the card to read it. The room answers three things about the card's OWN directory and nothing the
// card said: the commit it is at, whether anything there is uncommitted, and the branch. It also keeps the log.
//
//   - THE CARD NEVER NAMES A SHA. There is no field for one. `at_sha` is `git rev-parse HEAD` in the card's worktree at the
//     moment of the call, so an old test cannot be passed off as current. See docs/rnd/item-lifecycle-design.md section 4.
//   - THE LOG IS KEPT OUTSIDE EVERY REPO, in `<state dir>/items/<backlog id>/tests/`, folders 0700 and files 0600, because a
//     test log can hold env values and tokens and the atrium repo is public. A log file the card names is resolved through
//     internal/safepath against the card's own directory, so it cannot point anywhere else.
//   - THE ROOM DECIDES NOTHING ABOUT THE ITEM. Whether the branch is the item's is the hub's to say, since the hub owns items.

// stampRequest is what the hub asks.
type stampRequest struct {
	// Item is the backlog id, for the folder.
	Item string `json:"item"`
	// Log is the text of a log, and LogFile a path in the card's own directory to copy. Either, or neither.
	Log     string `json:"log,omitempty"`
	LogFile string `json:"log_file,omitempty"`
}

// StampReply is what the room read.
type StampReply struct {
	AtSHA string `json:"at_sha"`
	Dirty bool   `json:"dirty"`
	// Branch is the branch the worktree is on, "HEAD" when it is detached.
	Branch string `json:"branch"`
	// LogPath is where the log was kept on this room, "" when there was none.
	LogPath string `json:"log_path,omitempty"`
}

// StampLogMax is the most of a log that is kept, in bytes. The tail is what a log is read for, so it is the tail that stays.
const StampLogMax = 512 << 10

var stampItemRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,80}$`)

// stampGit runs git in a directory and answers its trimmed output. A variable so a test does not need a repository.
var stampGit = func(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", filepath.FromSlash(dir)}, args...)...)
	hideWindow(cmd)
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// handleStamp is `POST /v1/tasks/{id}/stamp`.
func (d *Daemon) handleStamp(w http.ResponseWriter, r *http.Request) {
	var in stampRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(&in); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err)
		return
	}
	task, err := d.st.Get(r.PathValue("id"))
	if err != nil {
		writeJSONErr(w, http.StatusNotFound, err)
		return
	}
	rep, err := stamp(StateDir(), task, in)
	if err != nil {
		writeJSONErr(w, http.StatusConflict, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rep)
}

// stamp reads the card's worktree and keeps the log.
func stamp(stateDir string, task *store.Task, in stampRequest) (StampReply, error) {
	var rep StampReply
	dir := strings.TrimSpace(task.Worktree)
	if dir == "" {
		return rep, errors.New("this card has no directory, so there is nothing to stamp a record with")
	}
	item := strings.TrimSpace(in.Item)
	if !stampItemRe.MatchString(item) || strings.Contains(item, "..") {
		return rep, fmt.Errorf("%q is not an item id this room will make a folder for", item)
	}
	head, err := stampGit(dir, "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	if err != nil || head == "" {
		return rep, errors.New("this card's directory has no commit yet, so a record cannot be pinned to one")
	}
	rep.AtSHA = strings.ToLower(head)
	if rep.Branch, err = stampGit(dir, "rev-parse", "--abbrev-ref", "HEAD"); err != nil {
		return rep, fmt.Errorf("could not read the branch of this card's directory: %w", err)
	}
	status, err := stampGit(dir, "status", "--porcelain")
	if err != nil {
		return rep, fmt.Errorf("could not read the state of this card's directory: %w", err)
	}
	rep.Dirty = dirtyStatus(status)

	var text []byte
	switch {
	case in.Log != "" && in.LogFile != "":
		return rep, errors.New("give the log as text or as a file, not both")
	case in.Log != "":
		text = []byte(in.Log)
	case in.LogFile != "":
		abs, err := safepath.Contained(filepath.FromSlash(dir), filepath.FromSlash(in.LogFile))
		if err != nil {
			return rep, fmt.Errorf("log_file has to be a file inside this card's directory: %w", err)
		}
		if text, err = readTail(abs, StampLogMax); err != nil {
			return rep, fmt.Errorf("could not read log_file: %w", err)
		}
	}
	if len(text) > StampLogMax {
		text = text[len(text)-StampLogMax:]
	}
	if len(text) > 0 {
		folder := filepath.Join(stateDir, "items", item, "tests")
		if err := os.MkdirAll(folder, 0o700); err != nil {
			return rep, fmt.Errorf("could not make the log folder: %w", err)
		}
		_ = os.Chmod(folder, 0o700)
		name := time.Now().UTC().Format("20060102T150405Z") + "-" + rep.AtSHA[:7] + ".log"
		path := filepath.Join(folder, name)
		if err := os.WriteFile(path, text, 0o600); err != nil {
			return rep, fmt.Errorf("could not keep the log: %w", err)
		}
		rep.LogPath = filepath.ToSlash(path)
	}
	return rep, nil
}

// atriumFiles are the untracked files atrium puts in a card's directory itself. A worktree with only these in it is clean.
var atriumFiles = map[string]bool{"BRIEF.md": true, "REPORT.md": true, "LEARNINGS.md": true}

// dirtyStatus is whether `git status --porcelain` output shows a change that is not atrium's own brief. An untracked file of
// the card's own counts, since a test may have run against it.
func dirtyStatus(status string) bool {
	for _, l := range strings.Split(status, "\n") {
		l = strings.TrimRight(l, "\r")
		if strings.TrimSpace(l) == "" {
			continue
		}
		if name, ok := strings.CutPrefix(l, "?? "); ok && atriumFiles[strings.Trim(name, `"`)] {
			continue
		}
		return true
	}
	return false
}

// readTail reads at most max bytes from the end of a file.
func readTail(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		return nil, errors.New("that is a folder")
	}
	if st.Size() > max {
		if _, err := f.Seek(st.Size()-max, io.SeekStart); err != nil {
			return nil, err
		}
	}
	return io.ReadAll(io.LimitReader(f, max))
}
