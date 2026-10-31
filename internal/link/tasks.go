package link

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link/ctl"
	"github.com/dovholuknf/atrium/internal/link/wire"
)

// TASKS OF AN ITEM, ON THE HUB.

const (
	taskTick     = 30 * time.Second
	taskSyncBody = announceMax
)

type taskReq struct {
	Do      string   `json:"do"`
	By      string   `json:"by"`
	Task    string   `json:"task"`
	Title   string   `json:"title"`
	Brief   string   `json:"brief"`
	Kind    string   `json:"kind"`
	Repo    string   `json:"repo"`
	Base    string   `json:"base"`
	After   []string `json:"after"`
	Outputs []string `json:"outputs"`
	Room    string   `json:"room"`
	Model   string   `json:"model"`
	Context string   `json:"context"`
}

func taskFail(w http.ResponseWriter, err error) {
	code := http.StatusServiceUnavailable
	switch {
	case errors.Is(err, hubstore.ErrNoWork), errors.Is(err, hubstore.ErrNoTask), errors.Is(err, hubstore.ErrNoItem):
		code = http.StatusNotFound
	case hubstore.IsRefusal(err):
		code = http.StatusBadRequest
	}
	crFail(w, code, err.Error())
}

func (p *Proxy) serveTasks(w http.ResponseWriter, r *http.Request, st *hubstore.Store, id string, write bool) {
	if !write {
		tasks, err := st.TaskList(id)
		if err != nil {
			taskFail(w, err)
			return
		}
		if tasks == nil {
			tasks = []hubstore.Task{}
		}
		ctxText, _ := st.TaskContext(id)
		learn, _ := st.Learnings(id, 0)
		if learn == nil {
			learn = []hubstore.Learning{}
		}
		crJSON(w, http.StatusOK, map[string]any{"tasks": tasks, "context": ctxText, "learnings": learn})
		return
	}
	var in taskReq
	if err := crDecode(r, &in); err != nil {
		crFail(w, http.StatusBadRequest, "could not read that: "+err.Error())
		return
	}
	if _, err := st.WorkByID(id); err != nil {
		taskFail(w, err)
		return
	}
	if ok, why := p.taskMayAct(r.Context(), st, id, in.By); !ok {
		crFail(w, http.StatusForbidden, why)
		return
	}
	do := strings.ToLower(strings.TrimSpace(in.Do))
	by := strings.TrimSpace(in.By)
	task := strings.TrimSpace(in.Task)
	if do != "add" && do != "context" && task == "" {
		crFail(w, http.StatusBadRequest, do+" needs task: the id of one of the item's tasks, like t1")
		return
	}
	note := ""
	switch do {
	case "add":
		if strings.ToLower(strings.TrimSpace(in.Kind)) == hubstore.TaskCode {
			crFail(w, http.StatusBadRequest, "code tasks are not built yet. a task is a scratch task: a throwaway folder and a recap")
			return
		}
		if len(in.After) > 0 {
			crFail(w, http.StatusBadRequest, "after is not built yet. tasks of an item run one at a time, in the order they were added")
			return
		}
		if room := strings.TrimSpace(in.Room); room != "" && !validRoomName(room) {
			crFail(w, http.StatusBadRequest, "room is not a room name")
			return
		}
		if c := strings.TrimSpace(in.Context); c != "" {
			if err := st.SetTaskContext(id, c); err != nil {
				taskFail(w, err)
				return
			}
		}
		t, err := st.TaskAdd(hubstore.TaskInput{BacklogID: id, Title: in.Title, Brief: in.Brief, Kind: in.Kind, Repo: in.Repo,
			Base: in.Base, After: in.After, Outputs: in.Outputs, Room: in.Room, Model: in.Model, CreatedBy: by})
		if err != nil {
			taskFail(w, err)
			return
		}
		p.RecordAudit(t.Room, "task-add", id+"/"+t.TaskID)
		p.taskKick()
		crJSON(w, http.StatusOK, map[string]any{"ok": true, "task": t})
		return
	case "context":
		if err := st.SetTaskContext(id, in.Context); err != nil {
			taskFail(w, err)
			return
		}
		crJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	case "hold", "release":
		moved, err := st.TaskSetHeld(id, task, do == "hold")
		if err != nil {
			taskFail(w, err)
			return
		}
		if !moved {
			p.taskNotMoved(w, st, id, task, do)
			return
		}
		if do == "release" {
			p.taskKick()
		}
	case "retry":
		t, err := st.TaskGet(id, task)
		if err != nil {
			taskFail(w, err)
			return
		}
		if t.State == hubstore.TaskCaptureFailed {
			crFail(w, http.StatusConflict, "a capture that failed is kept for a person to look at: retrying it or discarding it is not built yet. "+
				"the folder is "+t.Folder+" and the reason is: "+t.Reason)
			return
		}
		moved, err := st.TaskRetry(id, task)
		if err != nil {
			taskFail(w, err)
			return
		}
		if !moved {
			p.taskNotMoved(w, st, id, task, do)
			return
		}
		p.taskKick()
	case "cancel":
		t, err := st.TaskGet(id, task)
		if err != nil {
			taskFail(w, err)
			return
		}
		moved, err := st.TaskCancel(id, task, by)
		if err != nil {
			taskFail(w, err)
			return
		}
		if !moved {
			p.taskNotMoved(w, st, id, task, do)
			return
		}
		if t.State == hubstore.TaskRunning || t.State == hubstore.TaskUnconfirmed {
			note = p.taskStopWorker(r.Context(), st, t, by)
		}
		p.taskKick()
	default:
		crFail(w, http.StatusBadRequest, "do is add, context, hold, release, cancel or retry")
		return
	}
	p.RecordAudit("", "task-"+do, id+"/"+task)
	t, err := st.TaskGet(id, task)
	if err != nil {
		taskFail(w, err)
		return
	}
	out := map[string]any{"ok": true, "task": t}
	if note != "" {
		out["note"] = note
	}
	crJSON(w, http.StatusOK, out)
}

func (p *Proxy) taskNotMoved(w http.ResponseWriter, st *hubstore.Store, id, task, do string) {
	t, err := st.TaskGet(id, task)
	if err != nil {
		taskFail(w, err)
		return
	}
	crFail(w, http.StatusConflict, "task "+task+" is "+t.State+", so there is nothing to "+do)
}

// taskMayAct is the item's owner, or the board. An item with no assignment yet has no owner to ask, so only the board.
func (p *Proxy) taskMayAct(ctx context.Context, st *hubstore.Store, id, by string) (bool, string) {
	if strings.EqualFold(strings.TrimSpace(by), "board") {
		return true, ""
	}
	as, err := st.WorkAssignments(id)
	if err != nil || len(as) == 0 {
		return false, "that item has no owner yet, so only the board acts on its tasks"
	}
	return p.ownerMayAct(ctx, st, as[0], by)
}

// taskStopWorker asks the room to cancel the assignment a running task's card holds.
func (p *Proxy) taskStopWorker(ctx context.Context, st *hubstore.Store, t hubstore.Task, by string) string {
	room, card, ok := strings.Cut(t.Card, "~")
	if !ok {
		return "the task is cancelled. its worker's card is not known yet, so nobody was asked to leave"
	}
	as, err := st.WorkAssignments(t.BacklogID)
	if err != nil {
		return "the task is cancelled, and the worker could not be asked to leave: " + err.Error()
	}
	for _, it := range as {
		if !strings.EqualFold(it.Room, room) || it.Card != card || it.State == hubstore.ItemStateFinished {
			continue
		}
		raw, _ := json.Marshal(map[string]string{"by": by})
		code, resp, err := p.callItemRoom(ctx, it.Room, "/v1/items/"+it.ID+"/cancel", raw)
		if err != nil {
			return "the task is cancelled, and its room is not answering, so the worker was not asked to leave: " + err.Error()
		}
		if code >= 300 {
			return "the task is cancelled, and its room refused to stop the worker: " + strings.TrimSpace(string(resp))
		}
		return "the task is cancelled and its worker was asked to leave. what it made is captured if it can be"
	}
	return "the task is cancelled. its worker has no running assignment, so nobody was asked to leave"
}

// taskReport is a room's report about one of its tasks.
type taskReport struct {
	Event     string              `json:"event"`
	Room      string              `json:"room"`
	BacklogID string              `json:"backlog_id"`
	TaskID    string              `json:"task_id"`
	Attempt   int                 `json:"attempt"`
	Card      string              `json:"card"`
	Folder    string              `json:"folder"`
	Reason    string              `json:"reason"`
	Line      string              `json:"line"`
	Why       string              `json:"why"`
	Learned   string              `json:"learned"`
	Recap     string              `json:"recap"`
	Files     []hubstore.TaskFile `json:"files"`
	Path      string              `json:"path"`
	Name      string              `json:"name"`
	Data      []byte              `json:"data"`
}

func (p *Proxy) taskSync(w http.ResponseWriter, r *http.Request, st *hubstore.Store) {
	var ev taskReport
	if err := json.NewDecoder(io.LimitReader(r.Body, taskSyncBody)).Decode(&ev); err != nil {
		crFail(w, http.StatusBadRequest, "could not read that: "+err.Error())
		return
	}
	room := strings.ToLower(strings.TrimSpace(ev.Room))
	if !validRoomName(room) {
		crFail(w, http.StatusBadRequest, "room is not a room name")
		return
	}
	t, err := st.TaskGet(strings.TrimSpace(ev.BacklogID), strings.TrimSpace(ev.TaskID))
	if err != nil {
		taskFail(w, err)
		return
	}
	if t.Room != "" && t.Room != room {
		crFail(w, http.StatusForbidden, "that task runs on "+t.Room+", not on "+room)
		return
	}
	card := ""
	if c := strings.TrimSpace(ev.Card); c != "" {
		card = room + "~" + c
	}
	te := hubstore.TaskEvent{BacklogID: t.BacklogID, TaskID: t.TaskID, Attempt: ev.Attempt, Card: card, Folder: ev.Folder,
		Reason: ev.Reason, Line: ev.Line, Why: ev.Why, Learned: ev.Learned, Recap: ev.Recap, Files: ev.Files}
	applied := false
	switch ev.Event {
	case "capturing":
		applied, err = st.TaskCapturingNow(te)
	case "file":
		p.taskFile(w, st, t, room, card, ev)
		return
	case "captured":
		var got hubstore.Task
		got, applied, err = st.TaskCapturedAs(te)
		if err == nil && applied {
			p.backlogEvent("task", map[string]string{"id": got.BacklogID, "task": got.TaskID, "state": got.State})
			p.taskKick()
		}
	case "capture-failed":
		_, applied, err = st.TaskCaptureFailedAs(te)
	case "failed":
		_, applied, err = st.TaskFailedAs(te)
	case "reaped":
		applied, err = st.TaskReapedAs(te)
	default:
		crFail(w, http.StatusBadRequest, "event is capturing, file, captured, capture-failed, failed or reaped")
		return
	}
	if err != nil {
		taskFail(w, err)
		return
	}
	crJSON(w, http.StatusOK, map[string]any{"ok": true, "applied": applied})
}

// taskFile stores one captured file in the hub's drop, once: the same name and bytes again answers the file already there.
func (p *Proxy) taskFile(w http.ResponseWriter, st *hubstore.Store, t hubstore.Task, room, card string, ev taskReport) {
	if ev.Attempt != t.Attempt {
		crFail(w, http.StatusConflict, "that is attempt "+strconv.Itoa(ev.Attempt)+" and the task is on attempt "+strconv.Itoa(t.Attempt))
		return
	}
	switch t.State {
	case hubstore.TaskRunning, hubstore.TaskUnconfirmed, hubstore.TaskCapturing, hubstore.TaskCancelled, hubstore.TaskCaptureFailed:
	default:
		crFail(w, http.StatusConflict, "task "+t.TaskID+" is "+t.State+" and takes no files")
		return
	}
	name := t.BacklogID + "-" + t.TaskID + "-" + path.Base(strings.ReplaceAll(ev.Path, "\\", "/"))
	sum := sha256.Sum256(ev.Data)
	sha := hex.EncodeToString(sum[:])
	if row, ok, err := st.TaskFileFind(name, sha); err == nil && ok {
		crJSON(w, http.StatusOK, map[string]any{"ok": true, "name": row.Name, "size": row.Size, "sha256": row.SHA})
		return
	}
	days := 0
	if set, err := st.FileSettings(); err == nil {
		days = set.Caps.MaxTTLDays
	}
	row, err := st.FileAdd(hubstore.FileInput{Name: name, Origin: "card", By: "task " + t.BacklogID + "/" + t.TaskID,
		Card: card, Room: room, Note: "captured from task " + t.TaskID + " of " + t.BacklogID, Data: ev.Data, TTLDays: days})
	if err != nil {
		var de *hubstore.DocError
		if errors.As(err, &de) {
			crFail(w, http.StatusBadRequest, de.Msg)
			return
		}
		taskFail(w, err)
		return
	}
	crJSON(w, http.StatusOK, map[string]any{"ok": true, "name": row.Name, "size": row.Size, "sha256": row.SHA})
}

// taskKick runs the scheduler off the caller's path.
func (p *Proxy) taskKick() {
	go p.taskSchedule(context.Background())
}

// RunTasks is the 30 second tick: a task whose room was away, or whose item was busy, gets its turn without a change.
func (p *Proxy) RunTasks(ctx context.Context) {
	t := time.NewTicker(taskTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.taskSchedule(ctx)
		}
	}
}

// taskSchedule starts the oldest pending task of every item that has no task a worker is on. Serial: one pass at a time.
func (p *Proxy) taskSchedule(ctx context.Context) {
	p.taskMu.Lock()
	defer p.taskMu.Unlock()
	st := p.itemStore()
	p.mu.Lock()
	c := p.ctl
	p.mu.Unlock()
	if st == nil || c == nil {
		return
	}
	ids, err := st.TaskItemsWithPending()
	if err != nil {
		return
	}
	for _, id := range ids {
		if busy, err := st.TaskBusy(id); err != nil || busy {
			continue
		}
		tasks, err := st.TaskList(id)
		if err != nil {
			continue
		}
		for _, t := range tasks {
			if t.State == hubstore.TaskPending {
				p.taskStart(ctx, st, c, t)
				break
			}
		}
	}
}

// taskRoomFor is the room a task runs on: its own, else the room of the item's first assignment or card.
func taskRoomFor(st *hubstore.Store, t hubstore.Task) (string, string) {
	if t.Room != "" {
		return t.Room, ""
	}
	if as, _ := st.WorkAssignments(t.BacklogID); len(as) > 0 {
		return strings.ToLower(as[0].Room), ""
	}
	if cards, _ := st.WorkCards(t.BacklogID); len(cards) > 0 {
		return strings.ToLower(cards[0].Room), ""
	}
	return "", "the item has no card on any room yet. name a room with `room` when adding the task"
}

func (p *Proxy) taskStart(ctx context.Context, st *hubstore.Store, c *controlMCP, t hubstore.Task) {
	room, why := taskRoomFor(st, t)
	if room == "" {
		_ = st.TaskNote(t.BacklogID, t.TaskID, why)
		return
	}
	if ans, ok := c.reachable(ctx, room); !ok {
		_ = st.TaskNote(t.BacklogID, t.TaskID, ans.Error)
		return
	}
	begun, ok, err := st.TaskBegin(t.BacklogID, t.TaskID, room)
	if err != nil || !ok {
		return
	}
	work, _ := st.WorkByID(t.BacklogID)
	learn, _ := st.Learnings(t.BacklogID, hubstore.LearningBriefBytes)
	block := &taskBlock{BacklogID: begun.BacklogID, TaskID: begun.TaskID, Attempt: begun.Attempt, Kind: begun.Kind,
		Title: begun.Title, Brief: begun.Brief, Outputs: begun.Outputs, ItemTitle: work.Title}
	block.Context, _ = st.TaskContext(t.BacklogID)
	for _, l := range learn {
		block.Learnings = append(block.Learnings, l.Text)
	}
	in := launchInput{
		Title: t.BacklogID + " " + t.TaskID + ": " + begun.Title, Model: begun.Model, Item: begun.BacklogID, Isolated: true,
		Scratch: true, Tags: []string{SubagentTag, "atrium:task:" + begun.TaskID}, Task: block,
	}
	callerRoom, spawnedBy, spawnedByID := "", "", ""
	owner := p.taskOwner(st, begun.BacklogID, work)
	switch owner.kind {
	case hubstore.OwnerSession:
		name, ownerRoom, _ := strings.Cut(owner.who, "@")
		callerRoom = strings.ToLower(ownerRoom)
		spawnedBy = name
		if callerRoom != "" && callerRoom != room {
			if id, _, err := c.resolvePeer(ctx, callerRoom, name); err == nil {
				spawnedByID = wire.TagFor(callerRoom, id)
			}
			spawnedBy = name + "@" + callerRoom
		}
	case hubstore.OwnerRole:
		in.ItemOwner = owner.who
	default:
		in.ItemOwner = hubstore.OwnerHumanWord
	}
	out, err := c.launchOnRoom(ctx, in, "claude", room, callerRoom, spawnedBy, spawnedByID, "")
	p.taskLaunchDone(st, begun, room, out, err)
}

type taskOwnerOf struct{ kind, who string }

func (p *Proxy) taskOwner(st *hubstore.Store, id string, w hubstore.Work) taskOwnerOf {
	if as, _ := st.WorkAssignments(id); len(as) > 0 {
		return taskOwnerOf{as[0].OwnerKind, as[0].Owner}
	}
	if w.OwnerKind != "" {
		return taskOwnerOf{w.OwnerKind, w.Owner}
	}
	return taskOwnerOf{hubstore.OwnerHuman, ""}
}

// taskLaunchDone writes what a launch came to.
func (p *Proxy) taskLaunchDone(st *hubstore.Store, t hubstore.Task, room string, out launchOutput, err error) {
	if err == nil {
		_, bare := wire.SplitTag(out.Card)
		if e := st.TaskLaunched(t.BacklogID, t.TaskID, room+"~"+bare, false, ""); e != nil {
			log.Printf("[hub] task %s/%s launched on %s and the hub could not write it: %v", t.BacklogID, t.TaskID, room, e)
		}
		p.RecordAudit(room, "task-launch", t.BacklogID+"/"+t.TaskID)
		return
	}
	var ref *ctl.RefusedError
	var be *ctl.BoardError
	switch {
	case errors.As(err, &ref):
		_ = st.TaskWait(t.BacklogID, t.TaskID, ref.Msg)
	case errors.As(err, &be) && be.Code == http.StatusNotFound && be.Bare():
		_ = st.TaskLaunchFailed(t.BacklogID, t.TaskID, "the room "+room+" is too old to run tasks: update it")
	case errors.As(err, &be) && be.Code != http.StatusBadGateway && be.Code != http.StatusServiceUnavailable &&
		be.Code != http.StatusGatewayTimeout && be.Code >= 400 && be.Code < 500:
		_ = st.TaskLaunchFailed(t.BacklogID, t.TaskID, be.Msg)
	default:
		_ = st.TaskLaunched(t.BacklogID, t.TaskID, "", true, "the launch went out and its answer was lost: "+err.Error())
	}
}
