package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/safepath"
	"github.com/dovholuknf/atrium/internal/store"
)

// THE ROOM'S HALF OF A MOVE. The hub drives a move between rooms by calling these, in the order of
// docs/rnd/room-handoff-design.md section 3. Every route is a POST with a JSON body and is idempotent by move id:
// the hub repeats a step it did not see answered. Room A runs freeze, renew, check, capture, bundle, park, moved,
// queue, ack, release and undo. Room B runs receive, deliver and adopt. A successor is launched through
// /v1/launch with moved_from, and ended through /v1/tasks/{id}/exit.

// maxMoveBundle is the most one bundle carries, so a hub never streams an unbounded transcript.
const maxMoveBundle = 64 << 20

var (
	moveConvName   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	moveMemoryName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	moveFileName   = regexp.MustCompile(`^(BRIEF\.md|HANDOFF\.[A-Za-z0-9._-]+\.md)$`)
)

// moveFile is one file in a bundle. Data is base64 on the wire.
type moveFile struct {
	Name string `json:"name"`
	Data []byte `json:"data"`
}

// moveBundle is a card's conversation and the files that go with it.
type moveBundle struct {
	Conversation string     `json:"conversation"`
	Cwd          string     `json:"cwd,omitempty"`
	Jsonl        []byte     `json:"jsonl,omitempty"`
	Memory       []moveFile `json:"memory,omitempty"`
	Files        []moveFile `json:"files,omitempty"`
}

type moveAsk struct {
	ID        string   `json:"id"`
	MoveID    string   `json:"move_id"`
	To        string   `json:"to"`
	Room      string   `json:"room"`
	Lease     int      `json:"lease_seconds"`
	Wip       bool     `json:"wip"`
	MsgID     string   `json:"msg_id"`
	MsgIDs    []string `json:"msg_ids"`
	From      string   `json:"from"`
	Text      string   `json:"text"`
	Alias     string   `json:"alias"`
	Pinned    bool     `json:"pinned"`
	PinOrder  int      `json:"pin_order"`
	Gated     bool     `json:"gated"`
	Auto      bool     `json:"auto_approve"`
	WaitTurn  bool     `json:"wait_turn"`
	Cwd       string   `json:"cwd"`
	rawBundle json.RawMessage
}

func (d *Daemon) moveHandler() http.Handler {
	mux := http.NewServeMux()
	steps := map[string]func(http.ResponseWriter, *http.Request, *moveAsk){
		"freeze": d.moveFreeze, "renew": d.moveRenew, "check": d.moveCheck, "capture": d.moveCapture,
		"bundle": d.moveBundle, "park": d.movePark, "moved": d.moveMoved, "queue": d.moveQueue,
		"ack": d.moveAck, "release": d.moveRelease, "undo": d.moveUndo,
		"receive": d.moveReceive, "deliver": d.moveDeliver, "adopt": d.moveAdopt,
	}
	for name, fn := range steps {
		fn := fn
		mux.HandleFunc("POST /v1/move/"+name, func(w http.ResponseWriter, r *http.Request) {
			raw, err := io.ReadAll(io.LimitReader(r.Body, maxMoveBundle+(1<<20)))
			if err != nil {
				writeJSONErr(w, http.StatusBadRequest, err)
				return
			}
			var in moveAsk
			if err := json.Unmarshal(raw, &in); err != nil {
				writeJSONErr(w, http.StatusBadRequest, err)
				return
			}
			in.rawBundle = raw
			fn(w, r, &in)
		})
	}
	return mux
}

// moveCard is the card a step is about, or writes the refusal.
func (d *Daemon) moveCard(w http.ResponseWriter, in *moveAsk) *store.Task {
	id := strings.TrimSpace(in.ID)
	if i := strings.IndexByte(id, '~'); i > 0 {
		id = id[i+1:]
	}
	if id == "" {
		writeJSONErr(w, http.StatusBadRequest, errors.New("say which card"))
		return nil
	}
	t, err := d.st.Get(id)
	if err != nil {
		writeJSONErr(w, http.StatusNotFound, fmt.Errorf("no card %s here", id))
		return nil
	}
	in.ID = t.ID
	return t
}

// moveRefusal writes a store refusal as the status the hub reads.
func moveRefusal(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	switch {
	case errors.Is(err, store.ErrMoved), errors.Is(err, store.ErrLeaseExpired),
		errors.Is(err, store.ErrNotFrozen), errors.Is(err, store.ErrFrozenByOther):
		code = http.StatusConflict
	}
	writeJSONErr(w, code, err)
}

func (d *Daemon) moveFreeze(w http.ResponseWriter, _ *http.Request, in *moveAsk) {
	t := d.moveCard(w, in)
	if t == nil {
		return
	}
	lease := time.Duration(in.Lease) * time.Second
	f, err := d.st.Freeze(t.ID, in.MoveID, lease)
	if err != nil {
		moveRefusal(w, err)
		return
	}
	d.publishTask(t.ID)
	writeJSONCode(w, http.StatusOK, map[string]any{"ok": true, "freeze": f})
}

func (d *Daemon) moveRenew(w http.ResponseWriter, _ *http.Request, in *moveAsk) {
	t := d.moveCard(w, in)
	if t == nil {
		return
	}
	if err := d.st.RenewFreeze(t.ID, in.MoveID, time.Duration(in.Lease)*time.Second); err != nil {
		moveRefusal(w, err)
		return
	}
	writeJSONCode(w, http.StatusOK, map[string]any{"ok": true})
}

// moveGitArgs keeps atrium's own files out of the clean-tree question, and out of a wip commit.
var moveGitArgs = []string{"--", ".", ":(exclude)BRIEF.md", ":(glob,exclude)HANDOFF.*.md"}

func moveGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	hideWindow(cmd)
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// moveCheck is checks 1 and 2 of the design, run again after the freeze: the card is idle and its tree is clean.
// With wip, a dirty tree is committed instead of refused.
func (d *Daemon) moveCheck(w http.ResponseWriter, _ *http.Request, in *moveAsk) {
	t := d.moveCard(w, in)
	if t == nil {
		return
	}
	var fails []string
	if d.act.midTurn(t.ID) || d.nctx.holding(t.ID) {
		fails = append(fails, "busy: "+t.DisplayTitle()+" is mid-turn")
	}
	wip := false
	if t.Worktree != "" {
		if _, err := os.Stat(filepath.Join(t.Worktree, ".git")); err == nil {
			out, err := moveGit(t.Worktree, append([]string{"status", "--porcelain"}, moveGitArgs...)...)
			if err != nil {
				fails = append(fails, "dirty: "+err.Error())
			} else if strings.TrimSpace(out) != "" {
				lines := len(strings.Split(strings.TrimSpace(out), "\n"))
				if !in.Wip {
					fails = append(fails, fmt.Sprintf("dirty: %d files", lines))
				} else {
					room := strings.TrimSpace(in.Room)
					if _, err := moveGit(t.Worktree, append([]string{"add", "-A"}, moveGitArgs...)...); err != nil {
						fails = append(fails, "dirty: "+err.Error())
					} else if _, err := moveGit(t.Worktree, "commit", "-m", "WIP: move to "+room); err != nil {
						fails = append(fails, "dirty: "+err.Error())
					} else {
						wip = true
					}
				}
			}
		}
	}
	writeJSONCode(w, http.StatusOK, map[string]any{"ok": len(fails) == 0, "failures": fails, "wip": wip})
}

// moveCapture writes the card's handoff and answers when it is written. It is the idle handoff's capture, run
// to the end inside the request.
func (d *Daemon) moveCapture(w http.ResponseWriter, _ *http.Request, in *moveAsk) {
	t := d.moveCard(w, in)
	if t == nil {
		return
	}
	file := HandoffName(t)
	if d.sup.get(t.ID) == nil {
		// Parked or gone: nothing to type into. A handoff already in the cwd stays the read-me.
		_, err := os.Stat(filepath.Join(t.Worktree, file))
		writeJSONCode(w, http.StatusOK, map[string]any{"ok": true, "file": file, "skipped": "no runner", "exists": err == nil})
		return
	}
	gen, ok := d.nctx.beginCaptureOnly(t.ID, file, d.conversationOf(t))
	if !ok {
		writeJSONErr(w, http.StatusConflict, errors.New("busy: a capture is already running on this card"))
		return
	}
	step, err := d.ncCapture(t.ID, gen, file)
	d.nctx.finish(t.ID, gen)
	d.releaseHeld(t.ID)
	if err != nil {
		writeJSONErr(w, http.StatusBadGateway, fmt.Errorf("capture failed at %s: %w", step, err))
		return
	}
	writeJSONCode(w, http.StatusOK, map[string]any{"ok": true, "file": file})
}

func readCapped(path string, budget *int64) ([]byte, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a file", filepath.Base(path))
	}
	if fi.Size() > *budget {
		return nil, errTooLarge
	}
	*budget -= fi.Size()
	return os.ReadFile(path)
}

var errTooLarge = errors.New("transcript too large")

// moveBundle reads the card's last conversation, its project memory and its atrium files. A card with no
// transcript still answers, with the files, so a --fresh move works.
func (d *Daemon) moveBundle(w http.ResponseWriter, _ *http.Request, in *moveAsk) {
	t := d.moveCard(w, in)
	if t == nil {
		return
	}
	b := moveBundle{Conversation: d.conversationOf(t), Cwd: t.Worktree}
	budget := int64(maxMoveBundle)
	fail := func(err error) {
		code := http.StatusInternalServerError
		if errors.Is(err, errTooLarge) {
			code = http.StatusRequestEntityTooLarge
		}
		writeJSONErr(w, code, err)
	}
	proj, err := api.ProjectDirFor(t.Worktree)
	if err == nil && b.Conversation != "" && moveConvName.MatchString(b.Conversation) {
		data, rerr := readCapped(filepath.Join(proj, b.Conversation+".jsonl"), &budget)
		switch {
		case rerr == nil:
			b.Jsonl = data
		case errors.Is(rerr, errTooLarge):
			fail(rerr)
			return
		}
	}
	if err == nil {
		ents, _ := os.ReadDir(filepath.Join(proj, "memory"))
		for _, e := range ents {
			if e.IsDir() || !moveMemoryName.MatchString(e.Name()) {
				continue
			}
			data, rerr := readCapped(filepath.Join(proj, "memory", e.Name()), &budget)
			if errors.Is(rerr, errTooLarge) {
				fail(rerr)
				return
			}
			if rerr == nil {
				b.Memory = append(b.Memory, moveFile{Name: e.Name(), Data: data})
			}
		}
	}
	ents, _ := os.ReadDir(t.Worktree)
	for _, e := range ents {
		if e.IsDir() || !moveFileName.MatchString(e.Name()) {
			continue
		}
		data, rerr := readCapped(filepath.Join(t.Worktree, e.Name()), &budget)
		if errors.Is(rerr, errTooLarge) {
			fail(rerr)
			return
		}
		if rerr == nil {
			b.Files = append(b.Files, moveFile{Name: e.Name(), Data: data})
		}
	}
	sort.Slice(b.Files, func(i, j int) bool { return b.Files[i].Name < b.Files[j].Name })
	writeJSONCode(w, http.StatusOK, b)
}

// movePark parks the old runner as an idle park does: it exits, and the card stays, frozen and resumable.
func (d *Daemon) movePark(w http.ResponseWriter, _ *http.Request, in *moveAsk) {
	t := d.moveCard(w, in)
	if t == nil {
		return
	}
	if f, _ := d.st.Frozen(t.ID); f == nil {
		writeJSONErr(w, http.StatusConflict, store.ErrNotFrozen)
		return
	}
	if isParked(t) && d.sup.get(t.ID) == nil {
		writeJSONCode(w, http.StatusOK, map[string]any{"ok": true, "parked": true})
		return
	}
	was := t.Status
	if r := d.sup.get(t.ID); r != nil {
		if _, dup := d.idle.parking.LoadOrStore(t.ID, true); dup {
			writeJSONErr(w, http.StatusConflict, errors.New("a park is already running on this card"))
			return
		}
		defer d.idle.parking.Delete(t.ID)
		idleLeave(d, r, t.ID)
		deadline := time.Now().Add(idleGoneWait)
		for d.sup.get(t.ID) != nil && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		if d.sup.get(t.ID) != nil {
			writeJSONErr(w, http.StatusGatewayTimeout, errors.New("the runner did not leave"))
			return
		}
	}
	if err := d.parkCard(t.ID, was, map[string]any{"by": "move"}); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSONCode(w, http.StatusOK, map[string]any{"ok": true, "parked": true})
}

// moveMoved sets `moved_to`, the cut-over. After the lease it is refused.
func (d *Daemon) moveMoved(w http.ResponseWriter, _ *http.Request, in *moveAsk) {
	t := d.moveCard(w, in)
	if t == nil {
		return
	}
	if t.MovedTo != "" && t.MovedTo == in.To {
		writeJSONCode(w, http.StatusOK, map[string]any{"ok": true, "moved_to": t.MovedTo})
		return
	}
	if _, _, ok := splitMovedTo(in.To); !ok {
		writeJSONErr(w, http.StatusBadRequest, errors.New("to is room~card"))
		return
	}
	if err := d.st.SetMovedTo(t.ID, in.MoveID, in.To); err != nil {
		moveRefusal(w, err)
		return
	}
	d.publishTask(t.ID)
	writeJSONCode(w, http.StatusOK, map[string]any{"ok": true, "moved_to": in.To})
}

func (d *Daemon) moveQueue(w http.ResponseWriter, _ *http.Request, in *moveAsk) {
	t := d.moveCard(w, in)
	if t == nil {
		return
	}
	q, err := d.st.FreezeQueue(t.ID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	if q == nil {
		q = []store.FrozenMessage{}
	}
	writeJSONCode(w, http.StatusOK, map[string]any{"ok": true, "queue": q})
}

func (d *Daemon) moveAck(w http.ResponseWriter, _ *http.Request, in *moveAsk) {
	t := d.moveCard(w, in)
	if t == nil {
		return
	}
	if err := d.st.AckFrozen(t.ID, in.MsgIDs...); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSONCode(w, http.StatusOK, map[string]any{"ok": true})
}

// moveRelease closes the old card: its alias is released with a note, it is marked done, and the freeze ends once
// its queue has gone to the successor. Its handle stays reserved while `moved_to` is set.
func (d *Daemon) moveRelease(w http.ResponseWriter, _ *http.Request, in *moveAsk) {
	t := d.moveCard(w, in)
	if t == nil {
		return
	}
	if t.MovedTo == "" {
		writeJSONErr(w, http.StatusConflict, errors.New("the card has not moved: moved_to is not set"))
		return
	}
	q, err := d.st.FreezeQueue(t.ID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	if len(q) > 0 {
		writeJSONErr(w, http.StatusConflict, fmt.Errorf("%d messages are not forwarded yet", len(q)))
		return
	}
	if err := d.st.ReleaseAlias(t.ID, "moved to "+t.MovedTo); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	if t.Status != store.StatusDone {
		if err := d.st.SetStatus(t.ID, store.StatusDone); err != nil {
			writeJSONErr(w, http.StatusInternalServerError, err)
			return
		}
	}
	if _, err := d.st.Unfreeze(t.ID, in.MoveID, false); err != nil {
		moveRefusal(w, err)
		return
	}
	d.publishTask(t.ID)
	writeJSONCode(w, http.StatusOK, map[string]any{"ok": true})
}

// moveUndo leaves the card as it was: resumed, unfrozen, its queue replayed in order. Refused once it has moved.
func (d *Daemon) moveUndo(w http.ResponseWriter, _ *http.Request, in *moveAsk) {
	t := d.moveCard(w, in)
	if t == nil {
		return
	}
	if t.MovedTo != "" {
		moveRefusal(w, fmt.Errorf("%w: %s", store.ErrMoved, t.MovedTo))
		return
	}
	if err := d.undoCard(t.ID, in.MoveID); err != nil {
		moveRefusal(w, err)
		return
	}
	writeJSONCode(w, http.StatusOK, map[string]any{"ok": true})
}

// undoCard resumes a parked card and ends its freeze with the queue replayed. The lease sweep uses it as well.
func (d *Daemon) undoCard(id, moveID string) error {
	if f, _ := d.st.Frozen(id); f == nil {
		return nil
	}
	if err := d.unpark(id, "move"); err != nil {
		return err
	}
	if _, err := d.st.Unfreeze(id, moveID, true); err != nil {
		return err
	}
	d.releaseHeld(id)
	return nil
}

// moveReceive is B writing the bundle. Only through safepath, and only names that pass an allowlist: the jsonl
// under B's own encoded cwd, the memory folder beside it, and the two file patterns into the cwd.
func (d *Daemon) moveReceive(w http.ResponseWriter, _ *http.Request, in *moveAsk) {
	var b moveBundle
	if err := json.Unmarshal(in.rawBundle, &b); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err)
		return
	}
	cwdDir := strings.TrimSpace(in.Cwd)
	if fi, err := os.Stat(cwdDir); err != nil || !fi.IsDir() {
		writeJSONErr(w, http.StatusBadRequest, errors.New("cwd is not a folder here"))
		return
	}
	proj, err := api.ProjectDirFor(cwdDir)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	write := func(root, name string, data []byte) error {
		p, err := safepath.Contained(root, filepath.Join(root, name))
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		return os.WriteFile(p, data, 0o644)
	}
	wrote := 0
	if len(b.Jsonl) > 0 {
		if !moveConvName.MatchString(b.Conversation) {
			writeJSONErr(w, http.StatusBadRequest, errors.New("that conversation name is not allowed"))
			return
		}
		if err := write(proj, b.Conversation+".jsonl", b.Jsonl); err != nil {
			writeJSONErr(w, http.StatusBadRequest, err)
			return
		}
		wrote++
	}
	for _, f := range b.Memory {
		if f.Name != safepath.SafeName(f.Name) || !moveMemoryName.MatchString(f.Name) {
			writeJSONErr(w, http.StatusBadRequest, fmt.Errorf("memory file %q is not allowed", f.Name))
			return
		}
		if err := write(filepath.Join(proj, "memory"), f.Name, f.Data); err != nil {
			writeJSONErr(w, http.StatusBadRequest, err)
			return
		}
		wrote++
	}
	for _, f := range b.Files {
		if !moveFileName.MatchString(f.Name) {
			writeJSONErr(w, http.StatusBadRequest, fmt.Errorf("file %q is not allowed", f.Name))
			return
		}
		if err := write(cwdDir, f.Name, f.Data); err != nil {
			writeJSONErr(w, http.StatusBadRequest, err)
			return
		}
		wrote++
	}
	writeJSONCode(w, http.StatusOK, map[string]any{"ok": true, "wrote": wrote})
}

// moveDeliver is B taking one forwarded message by id. A repeat is answered `acked` and not delivered again.
func (d *Daemon) moveDeliver(w http.ResponseWriter, r *http.Request, in *moveAsk) {
	t := d.moveCard(w, in)
	if t == nil {
		return
	}
	if strings.TrimSpace(in.MsgID) == "" || strings.TrimSpace(in.Text) == "" {
		writeJSONErr(w, http.StatusBadRequest, errors.New("a delivery needs msg_id and text"))
		return
	}
	fresh, err := d.st.SeenMove(in.MsgID, t.ID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	if !fresh {
		writeJSONCode(w, http.StatusOK, map[string]any{"acked": true, "msg_id": in.MsgID, "duplicate": true})
		return
	}
	from := strings.TrimSpace(in.From)
	if from == "" {
		from = "atrium"
	}
	when := WhenImmediate
	if in.WaitTurn {
		when = WhenDone
	}
	raw, _ := json.Marshal(map[string]any{"text": in.Text, "from": from, "when": when})
	inner, err := http.NewRequestWithContext(r.Context(), http.MethodPost, "/v1/tasks/"+t.ID+"/message", bytes.NewReader(raw))
	if err != nil {
		_ = d.st.ForgetMove(in.MsgID)
		writeJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	inner.SetPathValue("id", t.ID)
	rec := &answerRecorder{code: http.StatusOK}
	d.handleMessage(rec, inner)
	if rec.code >= 400 {
		_ = d.st.ForgetMove(in.MsgID)
		writeJSONCode(w, rec.code, map[string]any{"error": strings.TrimSpace(rec.body.String())})
		return
	}
	writeJSONCode(w, http.StatusOK, map[string]any{"acked": true, "msg_id": in.MsgID})
}

// moveAdopt is the cut-over note to the successor: it takes the alias, the pin and the gate settings.
func (d *Daemon) moveAdopt(w http.ResponseWriter, _ *http.Request, in *moveAsk) {
	t := d.moveCard(w, in)
	if t == nil {
		return
	}
	if alias := strings.TrimSpace(in.Alias); alias != "" {
		if err := d.st.SetAlias(t.ID, alias); err != nil {
			writeJSONErr(w, http.StatusConflict, err)
			return
		}
	}
	if in.Pinned {
		if err := d.st.SetPinSlot(t.ID, in.PinOrder); err != nil {
			writeJSONErr(w, http.StatusInternalServerError, err)
			return
		}
	}
	if err := d.st.SetGated(t.ID, in.Gated); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	if err := d.st.SetAutoApprove(t.ID, in.Auto); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	d.publishTask(t.ID)
	writeJSONCode(w, http.StatusOK, map[string]any{"ok": true})
}
