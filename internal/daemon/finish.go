package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// An agent saying it finished.
//
// The largest hole in what atrium does, and it is a hole in the shape of a
// missing verb. Everything an agent reported landed in `needs-input`, so the
// board could not tell "finished, go and look at the result" from "stuck,
// answer me", and only a human moving a card by hand ever produced `done`.
//
// A command rather than a tool, because a command is the one channel every
// runner already has. An agent that can run `ls` can run `atrium finish`, with
// no MCP server, no tool description and no cooperation from the harness. That
// matters more here than anywhere else in atrium: this has to work for codex
// and for a bare shell, not only for the runner that happens to have a tool
// surface.
//
// THE SAME VERB IS THE WORKER'S REPORT. A card launched by another session
// owes that session a report, and `atrium_report` on the control MCP is the
// door a launched claude session is told to use. It lands in `finish` below,
// the same function this endpoint does, so the two cannot drift. A report is
// validated, recorded, and queued to the launcher. See
// docs/runtime/a2a-reliability-design.md.
//
// Same posture as every other agent-facing endpoint. It answers, it never
// fails a session, and an unknown agent is not an error.

// Report statuses. `done` and `needs-input` are what `atrium finish` has
// always taken. The other three are the worker contract.
const (
	ReportDone     = "done"
	ReportBlocked  = "blocked"
	ReportQuestion = "question"
	ReportProgress = "progress"
)

// FinishRequest is a session declaring its work over, or reporting on it.
type FinishRequest struct {
	// Agent is the wire name, the same one every hook uses.
	Agent string `json:"agent"`
	// TaskID is used when the session was launched by atrium and told which
	// card it belongs to, which is more reliable than a name.
	TaskID string `json:"task_id,omitempty"`
	// Recap is what it did, in its own words. Optional for a human's session,
	// and the card says so when it is missing rather than inventing one.
	Recap string `json:"recap,omitempty"`
	// Status is where to put the card. `done` unless something says
	// otherwise. `needs-input` hands the work back without claiming it is
	// finished. `blocked`, `question` and `progress` are a worker reporting.
	Status string `json:"status,omitempty"`
	// SHA is the commit a `done` names. Required for an agent-launched card
	// unless NoCommit says why there is none.
	SHA      string `json:"sha,omitempty"`
	NoCommit string `json:"no_commit,omitempty"`
	// Ask is what a `blocked` or `question` needs. Required for both.
	Ask string `json:"ask,omitempty"`
}

// handleFinish answers a session declaring its work over.
func (d *Daemon) handleFinish(w http.ResponseWriter, r *http.Request) {
	var in FinishRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(in.Agent) == "" && in.TaskID == "" {
		writeJSONErr(w, http.StatusBadRequest,
			errString("say which session this is, with agent or task_id"))
		return
	}

	task, err := d.taskFor(in.Agent, in.TaskID)
	if err != nil {
		// An agent atrium has never heard of is not an error. A session that
		// was never gated is entitled to say it finished and atrium is
		// entitled to have nowhere to put that, and failing here would mean a
		// session could fail at the moment it tried to end tidily.
		log.Printf("[atrium] %s said it finished and has no card here", in.Agent)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"recorded":false}`))
		return
	}
	d.answerFinish(w, task, in)
}

// handleReport is the same thing addressed by card, for the hub's
// `atrium_report`, which knows the caller's card and not its wire name.
func (d *Daemon) handleReport(w http.ResponseWriter, r *http.Request) {
	var in FinishRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err)
		return
	}
	task, err := d.st.Get(r.PathValue("id"))
	if err != nil {
		writeJSONErr(w, http.StatusNotFound, err)
		return
	}
	d.answerFinish(w, task, in)
}

func (d *Daemon) answerFinish(w http.ResponseWriter, task *store.Task, in FinishRequest) {
	out, code, err := d.finish(task, in)
	if err != nil {
		writeJSONErr(w, code, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// shaShape is what a commit id looks like: seven to forty hex characters.
var shaShape = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)

// validReport refuses a report that leaves its reader guessing. The text is
// handed back to the model in the same turn, so it says exactly what to add.
//
// A human's `atrium finish` keeps working as it always has: the sha rule is
// for a card another session launched, because that session has to be able
// to find the work without asking.
func validReport(task *store.Task, in FinishRequest) error {
	switch in.Status {
	case "", ReportDone, store.StatusNeedsInput, ReportProgress:
	case ReportBlocked, ReportQuestion:
		if strings.TrimSpace(in.Ask) == "" {
			return fmt.Errorf("a %s report needs ask: what you need, and from whom", in.Status)
		}
	default:
		return fmt.Errorf("status %q is not one of done, blocked, question or progress", in.Status)
	}
	if sha := strings.TrimSpace(in.SHA); sha != "" && !shaShape.MatchString(sha) {
		return fmt.Errorf("sha %q is not a commit id", sha)
	}
	isDone := in.Status == "" || in.Status == ReportDone
	if isDone && agentLaunched(task) && strings.TrimSpace(in.SHA) == "" && strings.TrimSpace(in.NoCommit) == "" {
		return errString("a done report needs sha, the commit the work landed as, or no_commit saying " +
			"why there is none")
	}
	return nil
}

// commitExists reports whether a commit is in a worktree. A variable so a
// test does not need a repository. Bounded, because it runs inside a request.
var commitExists = func(dir, sha string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", filepath.FromSlash(dir), "cat-file", "-e", sha+"^{commit}")
	hideWindow(cmd)
	return cmd.Run() == nil
}

// finish records a report and says what it did. The error carries the HTTP
// status to answer with.
func (d *Daemon) finish(task *store.Task, in FinishRequest) (map[string]any, int, error) {
	in.Status = strings.ToLower(strings.TrimSpace(in.Status))
	if err := validReport(task, in); err != nil {
		return nil, http.StatusBadRequest, err
	}

	status := store.StatusDone
	reason := ""
	switch in.Status {
	case store.StatusNeedsInput:
		// Handing the work back without claiming it is over. A different thing
		// and worth being able to say.
		status = store.StatusNeedsInput
	case ReportBlocked, ReportQuestion:
		status, reason = store.StatusNeedsInput, in.Status
	case ReportProgress:
		// Still working. The card stays where it is.
		status = task.Status
	}

	recap := strings.TrimSpace(in.Recap)
	if ask := strings.TrimSpace(in.Ask); ask != "" {
		if recap != "" {
			recap += "\n\n"
		}
		recap += "needs: " + ask
	}

	// The commit, checked against the worktree. Accepted either way: a worker
	// that committed somewhere else is doing its job, and the card flags what
	// could not be found so a human can look. Checked BEFORE the transaction
	// below, never inside it: a git call holding the store's only connection
	// would hold up every other write for as long as git takes.
	sha := strings.TrimSpace(in.SHA)
	unverified := false
	if status == store.StatusDone && sha != "" {
		unverified = !commitExists(task.Worktree, sha)
	}

	if status == store.StatusDone || in.Status == store.StatusNeedsInput {
		// A session whose work is over is not waiting on an answer any more,
		// so the question comes off the card. Without this, a session that
		// asked something, got unstuck on its own and then finished leaves a
		// card in `done` still asking, and the one field that means "somebody
		// owes this session something" starts collecting cards where nobody
		// does.
		d.askAnswered(task.ID, "the work finished")
	}
	// Recorded as coming from the session, so `done` that an agent declared
	// and `done` that a human dragged are distinguishable afterwards. They are
	// the same column and they are not the same claim.
	ev := map[string]any{
		"by": "agent", "kind": "finished", "status": status, "recap": recap != "",
	}
	if in.Status != "" && in.Status != ReportDone && in.Status != store.StatusNeedsInput {
		ev["kind"], ev["report"] = "report", in.Status
	}
	if sha != "" {
		ev["sha"], ev["unverified"] = sha, unverified
	}

	// ONE TRANSACTION for everything a report records: the recap, the commit,
	// the event, the column, `reported_at`, the work item and the notice to the
	// launcher. These were separate writes, and a failure between two of them
	// left a card half reported. See store.RecordReport.
	//
	// A card put down by hand stays put. Shelving is an answer, and a session
	// inside a shelved worktree announcing it is done does not overrule it.
	//
	// A worker's `done` still moves its card to `done`: the column is what the
	// session is doing. Its work item goes to `reported`, which is NOT finished.
	// Only the launcher's verdict closes work.
	write := store.ReportWrite{
		TaskID: task.ID, Recap: recap, Event: ev,
		SetSHA: status == store.StatusDone, SHA: sha, Unverified: unverified,
		MoveStatus: task.Status != store.StatusShelved && status != task.Status,
		Status:     status, Reason: reason,
		ReportStatus: in.Status,
	}
	if status == store.StatusDone {
		out := &store.WorkOutputs{NoCommit: strings.TrimSpace(in.NoCommit)}
		if sha != "" {
			out.Commits = []string{sha}
			if unverified {
				out.Unverified = []string{sha}
			}
		}
		write.Outputs = out
	}
	// The launcher hears it, verbatim, queued in the same transaction so a
	// crash cannot record the report and lose the notice. Keyed on the moment,
	// so every report is sent once. A card nobody launched has nobody to tell.
	if launcher := d.launcherOf(task); launcher != nil {
		write.Notice = &store.NoticeSpec{
			ToID: launcher.ID, From: task.WireName, Source: NoticeReport,
			Key:  time.Now().UTC().Format(time.RFC3339Nano),
			Text: truncatePeer(reportBody(task, in.Status, sha, unverified, recap)),
		}
	} else {
		// A launcher on another room hears it the same way, held in the relay
		// outbox inside the same transaction. See relay.go.
		write.Relay = d.launcherRelay(task, reportBody(task, in.Status, sha, unverified, recap))
	}
	// A REPORT TO A PARKED LAUNCHER RESUMES IT, as if wake=true: the launcher is
	// the one who has to act on it, and a report nobody reads is the failure
	// parking must not cause. The notice itself is queued by the ledger and typed
	// or carried by a hook like any other, so it is never typed here. See park.go.
	// A launcher that holds reports reads them when it asks, so it stays parked.
	if write.Notice != nil {
		if launcher, err := d.st.Get(write.Notice.ToID); err == nil && isParked(launcher) && !holdsReports(launcher) {
			if err := d.unpark(launcher.ID, "report"); err != nil {
				log.Printf("[atrium] could not resume %s for a report: %v", launcher.DisplayTitle(), err)
			}
		}
	}
	res, err := d.st.RecordReport(write)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	if res.Relayed {
		d.kickRelays()
	}
	if status == store.StatusDone || in.Status == store.StatusNeedsInput {
		// Whatever it was doing, it is not doing now. Not for a worker's
		// report mid-work, which is still doing it.
		d.act.forget(task.ID)
	}
	d.publishTask(task.ID)
	told := res.Notice != nil || res.Relayed

	// The room announces the session handing its work over, which the hub reads
	// as a card changing column without knowing the session declared it done. See
	// lifecycle.go.
	if status == store.StatusDone || in.Status == store.StatusNeedsInput {
		finishLine := task.DisplayTitle() + " reported it finished (" + status + ")"
		if recap != "" {
			finishLine += " with a recap"
		}
		d.emitLifecycle("session-finish", finishLine)
	}

	log.Printf("[atrium] %s reports %s%s", task.DisplayTitle(), map[bool]string{true: in.Status, false: "done"}[in.Status != ""],
		map[bool]string{true: ", and left a recap", false: " and said nothing about what it did"}[recap != ""])

	out := map[string]any{
		"ok": true, "recorded": true, "task_id": task.ID, "status": status,
		"unverified": unverified, "launcher_told": told,
	}
	if res.Item != nil {
		// Said back so a worker reads, in the same turn, that `done` handed
		// the work over rather than closed it.
		out["work_state"] = res.Item.State
	}
	return out, http.StatusOK, nil
}

// truncatePeer bounds a notice to what a peer message may carry.
func truncatePeer(text string) string {
	if len(text) > maxPeerMessage {
		return text[:maxPeerMessage]
	}
	return text
}

// reportBody is what a launcher reads when a worker reports.
func reportBody(task *store.Task, status, sha string, unverified bool, recap string) string {
	if status == "" {
		status = ReportDone
	}
	head := "report from " + task.WireName + ": " + status
	if sha != "" {
		head += " at " + sha
		if unverified {
			head += " (unverified: that commit is not in " + task.Worktree + ")"
		}
	}
	body := head + "\ncard " + task.ID
	if recap != "" {
		body += "\n\n" + recap
	}
	return body
}

// taskFor resolves a session to its card, by id when it has one and by wire
// name otherwise.
//
// Never creates one. Everywhere else a session announcing itself is a reason
// to make a card; here it is not, because a card created at the moment a
// session says it finished would be a card holding one fact and no history.
func (d *Daemon) taskFor(agent, taskID string) (*store.Task, error) {
	if taskID != "" {
		if t, err := d.st.Get(taskID); err == nil {
			return t, nil
		}
	}
	return d.st.GetByWireName(d.st.Qualify(agent))
}

// errString is an error from a literal, without reaching for fmt.
type errString string

func (e errString) Error() string { return string(e) }
