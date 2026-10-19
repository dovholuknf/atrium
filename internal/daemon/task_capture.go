package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/safepath"
	"github.com/dovholuknf/atrium/internal/store"
)

// CAPTURING A TASK'S RESULT. A task ends with done: the room waits for the runner to leave, reads the files the task named
// through safepath, sends each to the hub, and only after the hub has the result removes the folder, by its ledger row. A
// capture that could not keep everything leaves the folder and says why. A worker that ended with no done leaves it too.
// The posts are `/_hub/items/tasks/sync` over the link this room already holds, and a replay is a no-op there.

const (
	taskCaptureFiles   = 20
	taskCaptureFileMax = 5 << 20
	taskCaptureTotal   = 20 << 20
	taskExitWait       = 60 * time.Second
	taskPostWait       = 60 * time.Second
	taskRecapRead      = 64 << 10
	taskRecapFile      = "RECAP.md"
	taskSyncPath       = "/_hub/items/tasks/sync"
)

type capFile struct {
	rel, abs string
	size     int64
}

type taskPostError struct {
	msg       string
	permanent bool
}

func (e *taskPostError) Error() string { return e.msg }

// taskPost sends one event to the hub, three tries for one it did not answer. A refusal is not tried again.
func (d *Daemon) taskPost(body map[string]any) (json.RawMessage, error) {
	ha, _ := d.relay().(HubAsker)
	if ha == nil {
		return nil, &taskPostError{msg: "this room has no link to a hub"}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, &taskPostError{msg: err.Error(), permanent: true}
	}
	var last error
	for i := 0; i < 3; i++ {
		if i > 0 {
			time.Sleep(time.Duration(i) * 2 * time.Second)
		}
		ctx, cancel := context.WithTimeout(context.Background(), taskPostWait)
		res, err := ha.Hub(ctx, "", http.MethodPost, taskSyncPath, raw)
		cancel()
		switch {
		case err != nil:
			last = &taskPostError{msg: err.Error()}
		case res.OK:
			return res.Data, nil
		case res.Code >= 400 && res.Code < 500 && res.Code != http.StatusRequestTimeout && res.Code != http.StatusTooManyRequests:
			return nil, &taskPostError{msg: res.Error, permanent: true}
		default:
			last = &taskPostError{msg: res.Error}
		}
	}
	return nil, last
}

func (d *Daemon) taskEvent(row store.TaskDir, event string, more map[string]any) map[string]any {
	m := map[string]any{"event": event, "backlog_id": row.BacklogID, "task_id": row.TaskID, "attempt": row.Attempt,
		"card": row.Card, "folder": row.Path}
	for k, v := range more {
		m[k] = v
	}
	return m
}

// startTaskCapture begins the capture for a task card whose worker called done, or whose item was cancelled. Once per card.
func (d *Daemon) startTaskCapture(task *store.Task, in FinishRequest, cancelled bool) {
	if !isTaskCard(task) {
		return
	}
	if _, loaded := d.taskHandled.LoadOrStore(task.ID, true); loaded {
		return
	}
	go d.captureTask(task, in, cancelled)
}

func (d *Daemon) captureTask(task *store.Task, in FinishRequest, cancelled bool) {
	row, ok, err := d.st.TaskDirByCard(task.ID)
	if err != nil || !ok || row.State != store.TaskDirLive {
		return
	}
	d.waitRunnerGone(task.ID, taskExitWait)
	line := ""
	if _, after, found := strings.Cut(strings.TrimSpace(in.Recap), ": "); found {
		line = after
	}
	if !cancelled {
		if _, err := d.taskPost(d.taskEvent(row, "capturing", map[string]any{"line": line, "why": in.Why})); err != nil {
			d.captureFailed(task, row, "the hub could not be told the capture began: "+err.Error(), cancelled)
			return
		}
	}
	files, missing, err := resolveTaskOutputs(filepath.FromSlash(row.Path), row.Outputs)
	if err != nil {
		d.captureFailed(task, row, err.Error(), cancelled)
		return
	}
	sent := make([]map[string]any, 0, len(files))
	for _, f := range files {
		data, err := os.ReadFile(f.abs)
		if err != nil {
			d.captureFailed(task, row, "could not read "+f.rel+": "+err.Error(), cancelled)
			return
		}
		ans, err := d.taskPost(d.taskEvent(row, "file", map[string]any{
			"path": strings.ReplaceAll(f.rel, "/", "_"), "data": data}))
		if err != nil {
			d.captureFailed(task, row, "could not send "+f.rel+": "+err.Error(), cancelled)
			return
		}
		var got struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
			SHA  string `json:"sha256"`
		}
		if json.Unmarshal(ans, &got) != nil || got.Name == "" {
			d.captureFailed(task, row, "the hub did not name "+f.rel, cancelled)
			return
		}
		sent = append(sent, map[string]any{"path": f.rel, "name": got.Name, "size": got.Size, "sha256": got.SHA})
	}
	recap := readRecap(filepath.FromSlash(row.Path))
	if recap == "" {
		recap = line
	}
	if len(missing) > 0 {
		recap = strings.TrimSpace(recap + "\n\noutputs not found: " + strings.Join(missing, ", "))
	}
	if _, err := d.taskPost(d.taskEvent(row, "captured", map[string]any{"line": line, "why": in.Why, "learned": in.Learned,
		"recap": recap, "files": sent})); err != nil {
		d.captureFailed(task, row, "the hub did not take the result: "+err.Error(), cancelled)
		return
	}
	d.reapTaskFolder(row)
}

func readRecap(folder string) string {
	f, err := os.Open(filepath.Join(folder, taskRecapFile))
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, taskRecapRead)
	n, _ := f.Read(buf)
	return strings.ToValidUTF8(strings.TrimSpace(string(buf[:n])), "")
}

// resolveTaskOutputs lists the files the task named, each resolved through safepath, under the caps. A folder is every
// regular file in it. What is not there is returned by name, and is not an error.
func resolveTaskOutputs(folder string, outputs []string) (files []capFile, missing []string, err error) {
	root, err := filepath.EvalSymlinks(folder)
	if err != nil {
		return nil, nil, fmt.Errorf("the task folder is not readable: %v", err)
	}
	seen := map[string]bool{}
	var total int64
	add := func(abs string, size int64) error {
		if seen[abs] {
			return nil
		}
		seen[abs] = true
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			return err
		}
		if size > taskCaptureFileMax {
			return fmt.Errorf("%s is %d bytes, over the %d a file may be", filepath.ToSlash(rel), size, taskCaptureFileMax)
		}
		total += size
		if len(files) >= taskCaptureFiles {
			return fmt.Errorf("the task left more than %d files", taskCaptureFiles)
		}
		if total > taskCaptureTotal {
			return fmt.Errorf("the task's files are over %d bytes together", taskCaptureTotal)
		}
		files = append(files, capFile{rel: filepath.ToSlash(rel), abs: abs, size: size})
		return nil
	}
	for _, o := range outputs {
		abs, err := safepath.Contained(root, o)
		if err != nil {
			return nil, nil, fmt.Errorf("output %q is not a path inside the task folder", o)
		}
		fi, err := os.Stat(abs)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			missing = append(missing, o)
		case err != nil:
			return nil, nil, fmt.Errorf("could not read %s: %v", o, err)
		case fi.Mode().IsRegular():
			if err := add(abs, fi.Size()); err != nil {
				return nil, nil, err
			}
		case fi.IsDir():
			var inside []capFile
			werr := filepath.WalkDir(abs, func(p string, e fs.DirEntry, werr error) error {
				if werr != nil {
					return werr
				}
				if !e.Type().IsRegular() {
					return nil
				}
				info, err := e.Info()
				if err != nil {
					return err
				}
				if real, err := safepath.Contained(root, p); err == nil {
					inside = append(inside, capFile{abs: real, size: info.Size()})
				}
				return nil
			})
			if werr != nil {
				return nil, nil, fmt.Errorf("could not read %s: %v", o, werr)
			}
			sort.Slice(inside, func(i, j int) bool { return inside[i].abs < inside[j].abs })
			for _, f := range inside {
				if err := add(f.abs, f.size); err != nil {
					return nil, nil, err
				}
			}
		}
	}
	return files, missing, nil
}

// reapTaskFolder removes a captured task's folder by its ledger row, links as links, and tells the hub it is gone. What the
// hub has not acknowledged stays on the row and is sent again. The kept folders of older attempts go with it.
func (d *Daemon) reapTaskFolder(row store.TaskDir) {
	if err := os.RemoveAll(filepath.FromSlash(row.Path)); err != nil {
		_ = d.st.TaskDirSet(row.Path, store.TaskDirKept, "captured, and the folder could not be removed: "+err.Error(), "")
		log.Printf("[atrium] task %s/%s was captured but %s could not be removed: %v", row.BacklogID, row.TaskID, row.Path, err)
		return
	}
	d.owePost(row, store.TaskDirRemoved, "captured", d.taskEvent(row, "reaped", map[string]any{"reason": "captured"}))
	older, _ := d.st.TaskDirsOf(row.BacklogID, row.TaskID)
	for _, o := range older {
		if o.Attempt < row.Attempt && o.State == store.TaskDirKept {
			if os.RemoveAll(filepath.FromSlash(o.Path)) == nil {
				_ = d.st.TaskDirSet(o.Path, store.TaskDirRemoved, "a later attempt took its place", "")
			}
		}
	}
}

// owePost records a folder's new state with the event the hub is owed, then sends it and clears it on an answer.
func (d *Daemon) owePost(row store.TaskDir, state, reason string, body map[string]any) {
	raw, _ := json.Marshal(body)
	if err := d.st.TaskDirSet(row.Path, state, reason, string(raw)); err != nil {
		log.Printf("[atrium] could not record task folder %s: %v", row.Path, err)
		return
	}
	if _, err := d.taskPost(body); err != nil {
		log.Printf("[atrium] the hub was not told %v of %s, it will be sent again: %v", body["event"], row.Path, err)
		return
	}
	_ = d.st.TaskDirOwedClear(row.Path)
}

// flushTaskOwed sends again what the hub has not acknowledged. Run when the room comes up and after each capture.
func (d *Daemon) flushTaskOwed() {
	rows, err := d.st.TaskDirsOwed()
	if err != nil {
		return
	}
	for _, r := range rows {
		var body map[string]any
		if json.Unmarshal([]byte(r.Owed), &body) != nil {
			_ = d.st.TaskDirOwedClear(r.Path)
			continue
		}
		if _, err := d.taskPost(body); err != nil {
			var pe *taskPostError
			if !errors.As(err, &pe) || !pe.permanent {
				return
			}
		}
		_ = d.st.TaskDirOwedClear(r.Path)
	}
}

// captureFailed keeps the folder, tells the hub which step failed, and tells the owner once.
func (d *Daemon) captureFailed(task *store.Task, row store.TaskDir, reason string, cancelled bool) {
	log.Printf("[atrium] capturing task %s/%s failed: %s", row.BacklogID, row.TaskID, reason)
	d.owePost(row, store.TaskDirKept, reason, d.taskEvent(row, "capture-failed", map[string]any{"reason": reason}))
	if cancelled {
		return
	}
	if it, err := d.st.ItemOfTask(task.ID); err == nil && it != nil {
		key := itemNoticeKey(it.ID, it.Revision, "task-capture-failed:"+row.TaskID)
		d.itemNotifyKeyed(task, it, key, "task "+row.TaskID+" of item "+row.BacklogID+" ended and its result could not be kept: "+
			reason+". the folder is kept on the room: "+row.Path)
	}
}

// taskRunnerEnded files a task whose runner left with no done: the folder stays and the hub is told. A runner atrium ended
// for a restart, or one whose done is being captured, is not a failure.
func (d *Daemon) taskRunnerEnded(x runExit) {
	task, err := d.st.Get(x.taskID)
	if err != nil || !isTaskCard(task) || task.Status == store.StatusDone || x.cause == store.CauseShutdown {
		return
	}
	if _, loaded := d.taskHandled.LoadOrStore(task.ID, true); loaded {
		return
	}
	row, ok, err := d.st.TaskDirByCard(task.ID)
	if err != nil || !ok || row.State != store.TaskDirLive {
		return
	}
	reason := fmt.Sprintf("the worker ended with no done (exit code %d)", x.code)
	if x.tail != "" {
		reason += ": " + firstLine(x.tail)
	}
	d.owePost(row, store.TaskDirKept, reason, d.taskEvent(row, "failed", map[string]any{"reason": reason}))
}
