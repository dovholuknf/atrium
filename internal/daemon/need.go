package daemon

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// needRestartTries is how many times a need's restart is tried, because the asking worker is mid-turn when it asks
// and a runner mid-turn ignores its exit keys. A variable only so tests can shorten the gap.
const needRestartTries = 6

var needRestartGap = 10 * time.Second

// needTags turns what a worker says it lacks into the lean tags that give it, and lists what atrium cannot add.
func needTags(needs []string) (tags, refused []string) {
	for _, n := range needs {
		n = strings.TrimSpace(n)
		kind, name, _ := strings.Cut(n, ":")
		name = strings.TrimSpace(name)
		prefix := map[string]string{"mcp": leanMCPTagPrefix, "agent": leanAgentTagPrefix, "skill": leanSkillTagPrefix}[kind]
		if prefix == "" || name == "" {
			refused = append(refused, n)
			continue
		}
		tags = append(tags, prefix+name)
	}
	return tags, refused
}

// handleNeed is a lean worker naming what it lacks. An mcp, agent or skill is added to the card's tags and the runner
// restarts on the same conversation with a wake telling it to continue. Everything asked is logged on the card.
func (d *Daemon) handleNeed(w http.ResponseWriter, r *http.Request) {
	task, err := d.st.Get(r.PathValue("id"))
	if err != nil {
		task, err = d.st.GetByWireName(d.st.Qualify(r.PathValue("id")))
	}
	if err != nil {
		writeJSONErr(w, http.StatusNotFound, errString("no card called "+r.PathValue("id")))
		return
	}
	var in struct {
		Needs []string `json:"needs"`
		By    string   `json:"by"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil || len(in.Needs) == 0 {
		writeJSONErr(w, http.StatusBadRequest, errString("say what you lack, like mcp:name, agent:name or skill:name"))
		return
	}
	tags, refused := needTags(in.Needs)
	if err := d.st.AppendEvent(task.ID, "need", map[string]any{"needs": in.Needs, "by": in.By, "refused": refused}); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	log.Printf("[atrium] %s needs %s", task.ID, strings.Join(in.Needs, ", "))
	out := map[string]any{"card": task.ID, "refused": refused}
	switch {
	case len(tags) == 0:
		out["note"] = "atrium can add an mcp server, an agent or a skill. for anything else, atrium_say your launcher."
	case !hasTag(task.Tags, LeanTag):
		out["note"] = "this card is not lean, so it already has what its harness has. nothing was added."
	default:
		if err := d.st.SetTags(task.ID, mergeTags(task.Tags, tags)); err != nil {
			writeJSONErr(w, http.StatusInternalServerError, err)
			return
		}
		if _, _, err := d.queueWake(task.ID, "you now have "+strings.Join(in.Needs, ", ")+". continue.", "atrium"); err != nil {
			writeJSONErr(w, http.StatusInternalServerError, err)
			return
		}
		go d.restartForNeed(task.ID)
		out["restarting"] = true
		out["note"] = "atrium will restart you with it on this conversation and tell you to continue. end your turn now."
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// restartForNeed restarts the card until its runner leaves when asked.
func (d *Daemon) restartForNeed(taskID string) {
	for i := 0; i < needRestartTries; i++ {
		time.Sleep(needRestartGap)
		_, err := d.RestartRunner(taskID)
		if err == nil {
			return
		}
		log.Printf("[atrium] restart for %s's need, try %d: %v", taskID, i+1, err)
	}
}
