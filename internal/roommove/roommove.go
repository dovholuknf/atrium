package roommove

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// `atrium move <card> --from <room> --to <room>`: the driver of a move between rooms, over the room routes of
// daemon/moveroutes.go. docs/rnd/room-move-spike.md is the design, and docs/rnd/room-handoff-design.md section 3
// the order. Every request goes to a room, through the hub by X-Atrium-Room or straight to the room's own board.

type Opts struct {
	BoardURL, From, To, FromURL, ToURL, Repo string
	Wip, Fresh, NoCapture, Check             bool
	Lease                                    int
	Wait                                     time.Duration
	StopAfter                                string
	// ID names the move instead of a new one, so a caller can follow it with Load while it runs.
	ID string
	// Stop, when it answers true before the park, has the driver undo what it did and end.
	Stop func() bool
	// OnStep hears each step the record is saved at, with the new card once there is one.
	OnStep func(step, newID string)
}

// Record is the driver's own record of one move, so an undo or a resume knows where it stopped.
type Record struct {
	ID         string    `json:"id"`
	Card       string    `json:"card"`
	From       string    `json:"from"`
	To         string    `json:"to"`
	FromURL    string    `json:"from_url"`
	ToURL      string    `json:"to_url"`
	Board      string    `json:"board"`
	Step       string    `json:"step"`
	NewID      string    `json:"new_id,omitempty"`
	Cwd        string    `json:"cwd,omitempty"`
	CwdCreated bool      `json:"cwd_created,omitempty"`
	Branch     string    `json:"branch,omitempty"`
	Alias      string    `json:"alias,omitempty"`
	Note       string    `json:"note,omitempty"`
	Pinned     bool      `json:"pinned,omitempty"`
	PinOrder   int       `json:"pin_order,omitempty"`
	Gated      bool      `json:"gated,omitempty"`
	Auto       bool      `json:"auto_approve,omitempty"`
	Started    time.Time `json:"started"`
	Updated    time.Time `json:"updated"`

	onStep func(step, newID string)
}

// The steps in order. Before stepMoved the move is undone on a failure, from it on it only goes forward.
const (
	stepFrozen   = "frozen"
	stepChecked  = "checked"
	stepWorktree = "worktree"
	stepParked   = "parked"
	stepLaunched = "launched"
	stepMoved    = "moved"
	stepDone     = "done"
)

// moveTarget is one room as the driver reaches it: a base url and, through the hub, the room's name in a header.
type moveTarget struct {
	name, base string
	viaHub     bool
}

func (m moveTarget) do(method, path string, body, out any) (int, error) {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, strings.TrimRight(m.base, "/")+path, rd)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if m.viaHub {
		req.Header.Set("X-Atrium-Room", m.name)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Minute}).Do(req)
	if err != nil {
		return 0, fmt.Errorf("%s did not answer %s: %w", m.name, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 200<<20))
	if resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		if e.Error == "" {
			e.Error = strings.TrimSpace(string(raw))
		}
		return resp.StatusCode, fmt.Errorf("%s %s: %d %s", m.name, path, resp.StatusCode, e.Error)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.StatusCode, fmt.Errorf("%s %s: %w", m.name, path, err)
		}
	}
	return resp.StatusCode, nil
}

func (m moveTarget) step(name string, body map[string]any, out any) error {
	code, err := m.do(http.MethodPost, "/v1/move/"+name, body, out)
	if err != nil && moveUnreachable(code) {
		return fmt.Errorf("%w: %v", errMoveUnreachable, err)
	}
	return err
}

func moveRooms(r *Record) (a, b moveTarget) {
	a = moveTarget{name: r.From, base: r.FromURL}
	b = moveTarget{name: r.To, base: r.ToURL}
	if a.base == "" {
		a.base, a.viaHub = r.Board, true
	}
	if b.base == "" {
		b.base, b.viaHub = r.Board, true
	}
	return a, b
}

func roomURL(s string) string {
	if s = strings.TrimSpace(s); s != "" && !strings.Contains(s, "://") {
		s = "http://" + s
	}
	return s
}

// moveCardView is the half of a card the driver reads.
type moveCardView struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Why         string   `json:"why"`
	Worktree    string   `json:"worktree"`
	Runner      string   `json:"runner"`
	Status      string   `json:"status"`
	Alias       string   `json:"alias"`
	WireName    string   `json:"wire_name"`
	Model       string   `json:"model"`
	Effort      string   `json:"effort"`
	Tags        []string `json:"tags"`
	Pinned      bool     `json:"pinned"`
	PinOrder    int      `json:"pin_order"`
	Gated       bool     `json:"gated"`
	Auto        bool     `json:"auto_approve"`
	SpawnedBy   string   `json:"spawned_by"`
	SpawnedByID string   `json:"spawned_by_id"`
	MovedTo     string   `json:"moved_to"`
	Theme       string   `json:"theme"`
	Sound       string   `json:"sound"`
	Icon        string   `json:"icon"`
	PeerTyping  bool     `json:"peer_typing"`
}

func bareID(id string) string {
	if i := strings.LastIndexByte(id, '~'); i >= 0 {
		return id[i+1:]
	}
	return id
}

func moveDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".atrium", "moves")
	return dir, os.MkdirAll(dir, 0o755)
}

func (r *Record) save(step string) error {
	if step != "" {
		r.Step = step
		if r.onStep != nil {
			r.onStep(step, r.NewID)
		}
	}
	r.Updated = time.Now().UTC()
	dir, err := moveDir()
	if err != nil {
		return err
	}
	raw, _ := json.MarshalIndent(r, "", "  ")
	tmp := filepath.Join(dir, r.ID+".json.tmp")
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, r.ID+".json"))
}

// direct points an undo or a resume straight at the rooms, for when the hub it began through is gone.
func (r *Record) direct(o Opts) {
	if u := roomURL(o.FromURL); u != "" {
		r.FromURL = u
	}
	if u := roomURL(o.ToURL); u != "" {
		r.ToURL = u
	}
	if o.BoardURL != "" {
		r.Board = roomURL(o.BoardURL)
	}
}

// stopAt saves the step, and with --stop-after at that step ends the driver there, as a crash would.
func (r *Record) stopAt(step, stopAfter string) error {
	if err := r.save(step); err != nil {
		return err
	}
	if step == stopAfter {
		return fmt.Errorf("stopped after %s, move id %s", step, r.ID)
	}
	return nil
}

func Load(id string) (*Record, error) {
	dir, err := moveDir()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, filepath.Base(id)+".json"))
	if err != nil {
		return nil, fmt.Errorf("no move %s here: %w", id, err)
	}
	var r Record
	return &r, json.Unmarshal(raw, &r)
}

func NewID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "mv-" + hex.EncodeToString(b)
}

func Run(out io.Writer, card string, o Opts) error {
	from, id := strings.TrimSpace(o.From), strings.TrimSpace(card)
	if i := strings.IndexByte(id, '~'); i > 0 {
		if from == "" {
			from = id[:i]
		}
		id = id[i+1:]
	}
	to := strings.TrimSpace(o.To)
	if from == "" || to == "" {
		return errors.New("say --from and --to, the room names")
	}
	if from == to {
		return errors.New("--from and --to are the same room")
	}
	id0 := o.ID
	if id0 == "" {
		id0 = NewID()
	}
	rec := &Record{ID: id0, Card: id, From: from, To: to, FromURL: roomURL(o.FromURL),
		ToURL: roomURL(o.ToURL), Board: roomURL(o.BoardURL), Started: time.Now().UTC(), onStep: o.OnStep}
	a, b := moveRooms(rec)
	say := func(f string, args ...any) { fmt.Fprintf(out, "[%s] "+f+"\n", append([]any{rec.ID}, args...)...) }

	var t moveCardView
	if _, err := a.do(http.MethodGet, "/v1/tasks/"+id, nil, &t); err != nil {
		return err
	}
	t.ID = bareID(t.ID)
	rec.Card = t.ID
	var fails []string
	if t.MovedTo != "" {
		fails = append(fails, "the card has already moved to "+t.MovedTo)
	}
	var hs struct {
		Harnesses []struct {
			ID      string `json:"id"`
			Enabled bool   `json:"enabled"`
		} `json:"harnesses"`
	}
	if _, err := b.do(http.MethodGet, "/v1/harnesses", nil, &hs); err != nil {
		fails = append(fails, "room "+to+" is not answering: "+err.Error())
	} else if t.Runner != "" {
		found := false
		for _, h := range hs.Harnesses {
			found = found || (h.ID == t.Runner && h.Enabled)
		}
		if !found {
			fails = append(fails, to+" has no runner "+t.Runner)
		}
	}
	if len(fails) > 0 {
		return fmt.Errorf("refused, nothing moved:\n  %s", strings.Join(fails, "\n  "))
	}
	if o.Check {
		fmt.Fprintf(out, "check passed: %s~%s can move to %s\n", from, t.ID, to)
		return nil
	}
	rec.Alias, rec.Pinned, rec.PinOrder, rec.Gated, rec.Auto = t.Alias, t.Pinned, t.PinOrder, t.Gated, t.Auto

	ask := func(extra map[string]any) map[string]any {
		m := map[string]any{"id": t.ID, "move_id": rec.ID}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	fail := func(err error) error {
		say("failed at %s: %v. undoing", rec.Step, err)
		if uerr := undoMove(out, rec); uerr != nil {
			return fmt.Errorf("%w, and the undo failed: %v (move id %s)", err, uerr, rec.ID)
		}
		return fmt.Errorf("%w (undone, move id %s)", err, rec.ID)
	}

	stopped := func() error {
		if o.Stop != nil && o.Stop() {
			return fail(errStopped)
		}
		return nil
	}

	if err := a.step("freeze", ask(map[string]any{"lease_seconds": o.Lease}), nil); err != nil {
		return err
	}
	if err := rec.save(stepFrozen); err != nil {
		return fail(err)
	}
	if err := rec.stopAt(stepFrozen, o.StopAfter); err != nil {
		return err
	}
	say("frozen %s~%s", from, t.ID)
	var chk struct {
		OK       bool     `json:"ok"`
		Failures []string `json:"failures"`
		Wip      bool     `json:"wip"`
	}
	if err := stopped(); err != nil {
		return err
	}
	if err := a.step("check", ask(map[string]any{"room": to, "wip": o.Wip}), &chk); err != nil {
		return fail(err)
	}
	if !chk.OK {
		return fail(errors.New(strings.Join(chk.Failures, ", ")))
	}
	if chk.Wip {
		say("committed the dirty tree as WIP: move to %s", to)
	}
	if err := rec.stopAt(stepChecked, o.StopAfter); err != nil {
		return err
	}
	if err := stopped(); err != nil {
		return err
	}
	if !o.NoCapture {
		var cap struct {
			File    string `json:"file"`
			Skipped string `json:"skipped"`
		}
		if err := a.step("capture", ask(nil), &cap); err != nil {
			return fail(err)
		}
		say("handoff %s %s", cap.File, cap.Skipped)
	}

	var pack struct {
		Skipped string `json:"skipped"`
		Branch  string `json:"branch"`
		Base    string `json:"base"`
		Tip     string `json:"tip"`
		Origin  string `json:"origin"`
		Git     []byte `json:"git"`
	}
	if err := stopped(); err != nil {
		return err
	}
	if err := a.step("gitpack", ask(nil), &pack); err != nil {
		return fail(err)
	}
	if pack.Skipped != "" {
		return fail(fmt.Errorf("%s: a move carries the card's branch, so the card has to be in a git worktree", pack.Skipped))
	}
	slug := strings.TrimSpace(o.Repo)
	if pack.Skipped == "" {
		if slug == "" {
			got, err := RepoFromRemote(pack.Origin)
			if err != nil {
				return fail(err)
			}
			slug = got
		}
		var wt struct {
			Cwd           string `json:"cwd"`
			Created       bool   `json:"created"`
			BranchCreated bool   `json:"branch_created"`
		}
		body := map[string]any{"repo": slug, "branch": pack.Branch, "base": pack.Base, "git": pack.Git}
		code, err := b.do(http.MethodPost, "/v1/move/worktree", body, &wt)
		if err != nil && code == http.StatusConflict && pack.Base != "" {
			say("%s cannot take a thin bundle, sending the whole branch", to)
			if err = a.step("gitpack", ask(map[string]any{"full": true}), &pack); err == nil {
				body = map[string]any{"repo": slug, "branch": pack.Branch, "git": pack.Git}
				_, err = b.do(http.MethodPost, "/v1/move/worktree", body, &wt)
			}
		}
		if err != nil {
			return fail(err)
		}
		rec.Cwd, rec.CwdCreated = wt.Cwd, wt.Created
		if wt.BranchCreated {
			rec.Branch = pack.Branch
		}
		if err := rec.stopAt(stepWorktree, o.StopAfter); err != nil {
			return err
		}
		say("branch %s at %s carried to %s:%s (%d bytes)", pack.Branch, short(pack.Tip), to, wt.Cwd, len(pack.Git))
	}

	var bundle map[string]any
	if err := stopped(); err != nil {
		return err
	}
	if err := a.step("bundle", ask(nil), &bundle); err != nil {
		return fail(err)
	}
	conv, _ := bundle["conversation"].(string)
	if o.Fresh {
		delete(bundle, "jsonl")
		conv = ""
	}
	bundle["cwd"] = rec.Cwd
	var recv struct {
		Wrote int `json:"wrote"`
	}
	if err := b.step("receive", bundle, &recv); err != nil {
		return fail(err)
	}
	say("wrote %d files on %s (conversation %q)", recv.Wrote, to, conv)

	if err := stopped(); err != nil {
		return err
	}
	if err := a.step("park", ask(nil), nil); err != nil {
		return fail(err)
	}
	if err := rec.stopAt(stepParked, o.StopAfter); err != nil {
		return err
	}
	say("parked %s~%s", from, t.ID)

	title := t.Title
	if title == "" {
		title = t.WireName
	}
	launch := map[string]any{
		"harness": t.Runner, "cwd": rec.Cwd, "title": title, "why": t.Why, "resume": conv,
		"moved_from": from + "~" + t.ID, "pinned": t.Pinned, "pin_order": t.PinOrder, "model": t.Model,
		"effort": t.Effort, "tags": t.Tags, "theme": t.Theme, "branch": pack.Branch,
		"spawned_by": t.SpawnedBy, "spawned_by_id": t.SpawnedByID,
	}
	note := movePrompt(from, to, t.Worktree, rec.Cwd, chk.Wip)
	if conv == "" {
		launch["prompt"] = note
	} else {
		rec.Note = note
	}
	if parts := strings.Split(slug, "/"); len(parts) == 3 {
		launch["host"], launch["org"], launch["repo"] = parts[0], parts[1], parts[2]
	}
	var nc moveCardView
	if _, err := b.do(http.MethodPost, "/v1/launch", launch, &nc); err != nil {
		return fail(err)
	}
	rec.NewID = bareID(nc.ID)
	say("launched %s~%s", to, rec.NewID)
	if err := rec.stopAt(stepLaunched, o.StopAfter); err != nil {
		return err
	}
	return finishMove(out, rec, o.StopAfter, o.Wait)
}

// finishMove is the cut-over, from stepLaunched on. Every step is idempotent, so a resume runs it all again.
func finishMove(out io.Writer, rec *Record, stopAfter string, wait time.Duration) error {
	a, b := moveRooms(rec)
	say := func(f string, args ...any) { fmt.Fprintf(out, "[%s] "+f+"\n", append([]any{rec.ID}, args...)...) }
	to := rec.To + "~" + rec.NewID
	if rec.Step == stepLaunched {
		err := waitStarted(b, rec.NewID, wait)
		if err == nil && rec.Note != "" {
			// A resumed conversation takes no launch prompt, so the note is the first delivery, deduped by its id.
			err = b.step("deliver", map[string]any{"id": rec.NewID, "msg_id": rec.ID + "-note", "from": "atrium",
				"text": rec.Note}, nil)
		}
		if errors.Is(err, errMoveUnreachable) {
			return fmt.Errorf("%w. the move stays at %s: --resume %s when %s answers, with --from-url and --to-url if the hub is down",
				err, rec.Step, rec.ID, rec.To)
		}
		if err != nil {
			say("the successor is not ready: %v. undoing", err)
			if uerr := undoMove(out, rec); uerr != nil {
				return fmt.Errorf("%w, and the undo failed: %v", err, uerr)
			}
			return err
		}
	}
	if err := a.step("moved", map[string]any{"id": rec.Card, "move_id": rec.ID, "to": to}, nil); err != nil {
		if errors.Is(err, errMoveUnreachable) {
			return fmt.Errorf("%w. the move stays at %s: --resume %s when %s answers", err, rec.Step, rec.ID, rec.From)
		}
		if rec.Step != stepMoved {
			say("moved_to refused: %v. undoing", err)
			if uerr := undoMove(out, rec); uerr != nil {
				return fmt.Errorf("%w, and the undo failed: %v", err, uerr)
			}
		}
		return err
	}
	if err := rec.stopAt(stepMoved, stopAfter); err != nil {
		return err
	}
	say("cut over: %s~%s moved_to %s", rec.From, rec.Card, to)
	if err := b.step("adopt", map[string]any{"id": rec.NewID, "alias": rec.Alias, "pinned": rec.Pinned,
		"pin_order": rec.PinOrder, "gated": rec.Gated, "auto_approve": rec.Auto}, nil); err != nil {
		return err
	}
	var q struct {
		Queue []struct {
			ID       string `json:"id"`
			Text     string `json:"text"`
			FromPeer string `json:"from_peer"`
			WaitTurn bool   `json:"wait_turn"`
		} `json:"queue"`
	}
	if err := a.step("queue", map[string]any{"id": rec.Card}, &q); err != nil {
		return err
	}
	for _, m := range q.Queue {
		if err := b.step("deliver", map[string]any{"id": rec.NewID, "msg_id": m.ID, "from": m.FromPeer, "text": m.Text,
			"wait_turn": m.WaitTurn}, nil); err != nil {
			return err
		}
		if err := a.step("ack", map[string]any{"id": rec.Card, "msg_ids": []string{m.ID}}, nil); err != nil {
			return err
		}
	}
	say("forwarded %d held messages", len(q.Queue))
	if err := a.step("release", map[string]any{"id": rec.Card, "move_id": rec.ID}, nil); err != nil {
		return err
	}
	_ = rec.save(stepDone)
	say("done: %s~%s is now %s", rec.From, rec.Card, to)
	return nil
}

// undoMove rolls back a move that has not set moved_to: the successor ended, a worktree it made removed, and the old
// card unparked and unfrozen with its queue replayed.
func undoMove(out io.Writer, rec *Record) error {
	a, b := moveRooms(rec)
	var errs []string
	if rec.NewID != "" {
		if _, err := b.do(http.MethodPost, "/v1/tasks/"+rec.NewID+"/exit", map[string]any{}, nil); err != nil {
			errs = append(errs, err.Error())
		}
		if _, err := b.do(http.MethodPatch, "/v1/tasks/"+rec.NewID, map[string]any{"status": "done"}, nil); err != nil {
			fmt.Fprintf(out, "[%s] the successor %s~%s is not marked done: %v\n", rec.ID, rec.To, rec.NewID, err)
		}
	}
	if rec.CwdCreated && rec.Cwd != "" {
		if err := b.step("unworktree", map[string]any{"cwd": rec.Cwd, "branch": rec.Branch}, nil); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if err := a.step("undo", map[string]any{"id": rec.Card, "move_id": rec.ID}, nil); err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		_ = rec.save("")
		return errors.New(strings.Join(errs, ". "))
	}
	_ = rec.save("undone")
	fmt.Fprintf(out, "[%s] undone: %s~%s is back as it was\n", rec.ID, rec.From, rec.Card)
	return nil
}

func Undo(out io.Writer, id string, o Opts) error {
	rec, err := Load(id)
	if err != nil {
		return err
	}
	rec.direct(o)
	if rec.Step == stepMoved || rec.Step == stepDone {
		return fmt.Errorf("move %s is past the cut-over (%s): it only goes forward. use --resume", id, rec.Step)
	}
	return undoMove(out, rec)
}

func Resume(out io.Writer, id string, o Opts) error {
	rec, err := Load(id)
	if err != nil {
		return err
	}
	rec.direct(o)
	switch rec.Step {
	case stepDone:
		fmt.Fprintf(out, "move %s is done\n", id)
		return nil
	case stepLaunched, stepMoved:
		return finishMove(out, rec, "", o.Wait)
	}
	return fmt.Errorf("move %s stopped at %s, before the launch. use --undo", id, rec.Step)
}

// waitStarted waits for the successor to have a runner, or to be refused.
func waitStarted(b moveTarget, id string, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	for {
		var t struct {
			Status string `json:"status"`
			PID    int    `json:"pid"`
		}
		code, err := b.do(http.MethodGet, "/v1/tasks/"+id, nil, &t)
		if moveUnreachable(code) {
			return fmt.Errorf("%w: %v", errMoveUnreachable, err)
		}
		if err == nil {
			switch {
			case t.Status == "dead":
				return fmt.Errorf("the successor %s died on start", id)
			case t.PID > 0:
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the successor %s did not start in %s", id, wait)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func movePrompt(from, to, oldCwd, newCwd string, wip bool) string {
	p := fmt.Sprintf("You were moved from room %s to room %s by `atrium move`. Your worktree is now %s.", from, to, newCwd)
	if oldCwd != "" && oldCwd != newCwd {
		p += fmt.Sprintf(" Paths in your history under %s are now under %s.", oldCwd, newCwd)
	}
	if wip {
		p += fmt.Sprintf(" Your uncommitted work was committed as `WIP: move to %s`. `git reset --soft HEAD~1` puts it back"+
			" as uncommitted changes if you want that.", to)
	}
	return p + " Read HANDOFF.*.md and BRIEF.md if they are there, then carry on with your task."
}

func short(sha string) string {
	if len(sha) > 9 {
		return sha[:9]
	}
	return sha
}

// errMoveUnreachable is a room that gave no answer at all. A launched move waits for it rather than undoing what it
// cannot see.
// errStopped is a move stopped on request before the park, and undone.
var errStopped = errors.New("stopped on request")

var errMoveUnreachable = errors.New("no answer")

func moveUnreachable(code int) bool {
	return code == 0 || code == http.StatusBadGateway || code == http.StatusServiceUnavailable || code == http.StatusGatewayTimeout
}

// RepoFromRemote reads host/org/repo from an origin url, https or scp form.
func RepoFromRemote(remote string) (string, error) {
	host, path := "", ""
	switch {
	case strings.Contains(remote, "://"):
		u, err := url.Parse(remote)
		if err != nil {
			return "", fmt.Errorf("cannot read the repo from origin %q", remote)
		}
		host, path = u.Hostname(), u.Path
	case strings.Contains(remote, "@") && strings.Contains(remote, ":"):
		rest := remote[strings.Index(remote, "@")+1:]
		i := strings.Index(rest, ":")
		host, path = rest[:i], rest[i+1:]
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	parts := strings.Split(path, "/")
	if host == "" || len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("cannot read host/org/repo from origin %q. pass --repo host/org/repo", remote)
	}
	return strings.ToLower(host) + "/" + parts[0] + "/" + parts[1], nil
}
