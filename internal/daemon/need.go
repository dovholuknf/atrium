package daemon

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/small"
)

// needRestartTries is how many times a need's restart is tried, because the asking worker is mid-turn when it asks
// and a runner mid-turn ignores its exit keys.
const needRestartTries = 6

// needQuiet is how long a card must have posted no activity before its need restarts it: its turn has ended.
const needQuiet = 3 * time.Second

// needQuietWait bounds the wait for that quiet in one try, and needQuietTick is how often it looks.
const (
	needQuietWait = 60 * time.Second
	needQuietTick = 250 * time.Millisecond
)

// ElevatedTag gives a worker card atrium_launch and atrium_launch_worker. The hub's class lookup reads it.
const ElevatedTag = "atrium:elevated"

// needLaunch is the kind that asks for ElevatedTag, which any card may be given, lean or not.
const needLaunch = "tool:atrium_launch"

// needTags turns what a worker says it lacks into the lean tags that give it, and lists what atrium cannot add.
// needLaunch is neither: the caller adds ElevatedTag for it.
func needTags(needs []string) (tags, refused []string) {
	for _, n := range needs {
		n = strings.TrimSpace(n)
		kind, name, _ := strings.Cut(n, ":")
		name = strings.TrimSpace(name)
		if kind == "tool" {
			switch name {
			case "Skill", "Agent":
				tags = append(tags, leanToolTagPrefix+name)
			case "atrium_launch":
			case "Read", "Edit", "Write", "Grep", "Glob", "Bash", "PowerShell", "WebFetch":
				tags = append(tags, leanToolTagPrefix+name)
			default:
				refused = append(refused, n)
			}
			continue
		}
		prefix := map[string]string{"mcp": leanMCPTagPrefix, "agent": leanAgentTagPrefix, "skill": leanSkillTagPrefix}[kind]
		if prefix == "" || name == "" {
			refused = append(refused, n)
			continue
		}
		tags = append(tags, prefix+name)
	}
	return tags, refused
}

// handleNeed is a worker naming what it lacks and why. An mcp, agent, skill, or the Skill or Agent tool is added to a
// lean card's tags, atrium_launch to any card's, and the runner restarts on the same conversation with a wake telling
// it to continue. Everything asked is logged on the card's work item, and `granted` is what the hub tells the human.
func (d *Daemon) handleNeed(w http.ResponseWriter, r *http.Request) {
	task, err := d.st.Get(r.PathValue("id"))
	if err != nil {
		task, err = d.st.GetByWireName(d.st.Qualify(r.PathValue("id")))
	}
	if err != nil {
		small.WriteJSONErr(w, http.StatusNotFound, small.ErrString("no card called "+r.PathValue("id")))
		return
	}
	var in struct {
		Needs []string `json:"needs"`
		Why   string   `json:"why"`
		By    string   `json:"by"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil || len(in.Needs) == 0 {
		small.WriteJSONErr(w, http.StatusBadRequest, small.ErrString("say what you lack, like mcp:name, agent:name, "+
			"skill:name, tool:Skill, tool:Agent or tool:atrium_launch"))
		return
	}
	if in.Why = strings.TrimSpace(in.Why); in.Why == "" {
		small.WriteJSONErr(w, http.StatusBadRequest, small.ErrString("say why you need it, in one line"))
		return
	}
	tags, refused := needTags(in.Needs)
	// NOT AN EVENT: `need` is not a kind the event table's CHECK takes, so an event here refused every need.
	line := "need by " + in.By + ": " + strings.Join(in.Needs, ", ") + ". why: " + in.Why
	if len(refused) > 0 {
		line += ". refused: " + strings.Join(refused, ", ")
	}
	if err := d.st.LogWorkAtrium(task.ID, line); err != nil {
		small.WriteJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	log.Printf("[atrium] %s needs %s: %s", task.ID, strings.Join(in.Needs, ", "), logLine(in.Why))
	out := map[string]any{"card": task.ID, "refused": refused}
	lean := small.HasTag(task.Tags, LeanTag)
	if !small.HasTag(task.Tags, ChatTag) {
		var kept []string
		for _, t := range tags {
			if name, ok := strings.CutPrefix(t, leanToolTagPrefix); ok && isChatTool(name) {
				refused = append(refused, "tool:"+name)
				continue
			}
			kept = append(kept, t)
		}
		tags = kept
	}
	var given []string
	for _, n := range in.Needs {
		n = strings.TrimSpace(n)
		if n == needLaunch {
			tags = append(tags, ElevatedTag)
			given = append(given, n)
		} else if lean && !small.HasTag(refused, n) {
			given = append(given, n)
		}
	}
	if !lean {
		tags = onlyTag(tags, ElevatedTag)
	}
	switch {
	case len(tags) == 0 && len(refused) == len(in.Needs):
		out["note"] = "atrium can add an mcp server, an agent, a skill, the Skill or Agent tool, or atrium_launch. " +
			"for anything else, atrium_say your launcher."
	case len(tags) == 0:
		out["note"] = "this card is not lean, so it already has what its harness has. nothing was added."
	default:
		if err := d.st.SetTags(task.ID, mergeTags(task.Tags, tags)); err != nil {
			small.WriteJSONErr(w, http.StatusInternalServerError, err)
			return
		}
		if _, _, err := d.QueueWake(task.ID, "you now have "+strings.Join(given, ", ")+". continue.", "atrium"); err != nil {
			small.WriteJSONErr(w, http.StatusInternalServerError, err)
			return
		}
		go d.restartForNeed(task.ID)
		out["restarting"] = true
		out["granted"] = given
		out["title"] = task.DisplayTitle()
		out["note"] = "atrium will restart you with it on this conversation and tell you to continue. end your turn now."
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// onlyTag is the tags in in that equal keep.
func onlyTag(in []string, keep string) []string {
	var out []string
	for _, t := range in {
		if t == keep {
			out = append(out, t)
		}
	}
	return out
}

// restartForNeed restarts the card once it has been quiet for needQuiet, retrying while the runner will not leave.
func (d *Daemon) restartForNeed(taskID string) {
	quiet, wait, tick := d.hk.needQuiet, needQuietWait, needQuietTick
	if quiet == 0 {
		quiet = needQuiet
	}
	if d.hk.needQuietTick != 0 {
		tick, wait = d.hk.needQuietTick, d.hk.needQuietTick*40
	}
	for i := 0; i < needRestartTries; i++ {
		for waited := time.Duration(0); d.act.quietFor(taskID) < quiet && waited < wait; waited += tick {
			time.Sleep(tick)
		}
		restart := d.RestartRunner
		if d.hk.needRestart != nil {
			restart = d.hk.needRestart
		}
		_, err := restart(taskID)
		if err == nil {
			return
		}
		log.Printf("[atrium] restart for %s's need, try %d: %v", taskID, i+1, err)
		time.Sleep(4 * tick)
	}
}
