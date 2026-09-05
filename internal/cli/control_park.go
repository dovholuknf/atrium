package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// Not pulling the rug out from under other agents.
//
// Restarting the daemon closes every pseudo terminal it owns, and closing a
// terminal kills the process attached to it. For the session that ASKED for the
// restart that is understood and expected: it said so, and it comes back from
// its resume id. For every other supervised session it is a process killed
// mid-thought, and the cost is not symmetric with the benefit: the restart can
// wait ninety seconds and a half-written file cannot be un-written.
//
// So a restart asks first. Anything atrium owns that is working gets told what
// is about to happen and is given a chance to reach a stopping point.
//
// ONLY SESSIONS ATRIUM OWNS. A session running in a terminal somebody opened
// themselves survives a restart entirely: nothing here kills it, so nothing
// here has to warn it.

// parkWait is how long to give a busy agent to reach a stopping point.
//
// Ninety seconds because that is a long tool call, not a long turn. Waiting
// for a whole turn would mean waiting minutes for a session that is going to
// keep working regardless, and the message queued below is delivered at the
// next tool call rather than at the end of the turn.
const parkWait = 90 * time.Second

// parkPoll is how often to ask whether they have settled.
const parkPoll = 2 * time.Second

// busyCard is one supervised session that is doing something.
type busyCard struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Doing  string `json:"doing,omitempty"`
}

type cardView struct {
	ID           string `json:"id"`
	DisplayTitle string `json:"display_title"`
	Status       string `json:"status"`
	Supervised   bool   `json:"supervised"`
	Activity     *struct {
		What string `json:"what"`
		Tool string `json:"tool"`
	} `json:"activity"`
}

// meID is the card this control server belongs to, when it belongs to one.
//
// The daemon puts `ATRIUM_TASK_ID` in every runner's environment, and an MCP
// server is a child of that runner, so it inherits it. That is what makes
// "everyone except me" answerable at all: without it the caller would be
// counted among the busy sessions it is waiting for, and a restart requested by
// a working agent could never proceed.
func meID() string { return strings.TrimSpace(os.Getenv("ATRIUM_TASK_ID")) }

// busyAgents lists supervised sessions that are working, excluding the caller.
//
// `running` is not the test. A card sits in `running` from the moment it
// starts until something moves it, so it says where the card is filed rather
// than whether a process is mid-tool. What is asked instead is whether the
// daemon believes it is DOING something right now, which is the live activity
// it tracks in memory, plus the status for the case where activity is unknown
// because a session has no hooks installed.
func busyAgents(board string) ([]busyCard, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	res, err := client.Get(board + "/v1/tasks")
	if err != nil {
		return nil, fmt.Errorf("could not ask the board what is running: %w", err)
	}
	defer res.Body.Close()

	var body struct {
		Tasks []cardView `json:"tasks"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("could not read the board's answer: %w", err)
	}

	me := meID()
	var busy []busyCard
	for _, t := range body.Tasks {
		// Only what the restart will actually kill.
		if !t.Supervised {
			continue
		}
		if me != "" && t.ID == me {
			continue
		}
		// Waiting on a human is the definition of a safe moment: the model is
		// not running, nothing is half written, and it will be resumed rather
		// than interrupted.
		if t.Status == "needs-input" || t.Status == "needs-permission" {
			continue
		}
		doing := ""
		if t.Activity != nil && t.Activity.What != "" {
			doing = t.Activity.What
			if t.Activity.Tool != "" {
				doing = t.Activity.What + " " + t.Activity.Tool
			}
		}
		// No activity and not waiting means atrium has heard nothing recently.
		// Counted as busy rather than idle: the whole point is to not kill
		// something that might be working, and "I cannot tell" is not "it is
		// safe".
		busy = append(busy, busyCard{
			ID: t.ID, Title: t.DisplayTitle, Status: t.Status, Doing: doing,
		})
	}
	sort.Slice(busy, func(i, j int) bool { return busy[i].Title < busy[j].Title })
	return busy, nil
}

// tellToPark queues a message asking a session to stop at a safe point.
//
// QUEUED, not typed, which is the rule everywhere in atrium and matters most
// here: the target's terminal may have a human mid-command in it. The message
// is delivered as a BLOCK on the session's next tool call, which is the only
// mechanism that actually interrupts a working agent, and the block carries
// the text so the model reads it as somebody talking rather than as a policy
// refusal.
func tellToPark(board, id, why string) error {
	text := "atrium is about to restart, so this terminal is going to close. " +
		"Stop at a safe point NOW: finish or abandon the edit you are in the middle of, " +
		"do not start anything new, and do not run another tool. " +
		"Your conversation is kept and you will be resumed."
	if strings.TrimSpace(why) != "" {
		text += " The restart is for: " + strings.TrimSpace(why) + "."
	}
	payload, _ := json.Marshal(map[string]string{"text": text})

	client := &http.Client{Timeout: 5 * time.Second}
	res, err := client.Post(board+"/v1/tasks/"+id+"/message",
		"application/json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return fmt.Errorf("the board refused the message: %s", res.Status)
	}
	return nil
}

// parkAgents tells everything that is working to stop, then waits for it.
//
// Returns whoever was still busy when the wait ran out, so the caller can
// decide. It does NOT decide: refusing to restart and restarting anyway are
// both reasonable and the difference is whether somebody said `force`.
func parkAgents(board, why string, wait time.Duration) ([]busyCard, []string, error) {
	busy, err := busyAgents(board)
	if err != nil {
		return nil, nil, err
	}
	if len(busy) == 0 {
		return nil, nil, nil
	}

	// Told once, up front, all of them. A message per poll would queue a dozen
	// copies of the same sentence in front of a session that is about to be
	// interrupted anyway.
	var told []string
	for _, b := range busy {
		if err := tellToPark(board, b.ID, why); err != nil {
			// Not fatal. A session that cannot be told is a session that gets
			// interrupted, which is the situation without any of this, and the
			// wait below still gives it a chance to finish on its own.
			continue
		}
		told = append(told, b.Title)
	}

	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		time.Sleep(parkPoll)
		busy, err = busyAgents(board)
		if err != nil {
			// The board went away while waiting, which means the thing being
			// waited for is already over.
			return nil, told, nil
		}
		if len(busy) == 0 {
			return nil, told, nil
		}
	}
	return busy, told, nil
}

// describe is the list of who is busy, as one readable line per session.
func describe(busy []busyCard) string {
	parts := make([]string, 0, len(busy))
	for _, b := range busy {
		s := b.Title
		if b.Doing != "" {
			s += " (" + b.Doing + ")"
		} else {
			s += " (" + b.Status + ")"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ", ")
}
