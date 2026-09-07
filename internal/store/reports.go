package store

import (
	"encoding/json"
	"time"
)

// What a session last said about itself, read back off the timeline.
//
// `atrium finish` and `atrium ask` both already write an event: `finish.go`
// records `{"by":"agent","kind":"finished"}` and `help.go` records
// `{"by":"agent","kind":"asked","blocked":true|false}`. Nothing ever read them
// back, which is why sixteen agents could run for hours and the only way to
// learn which ones had finished was to ask them.
//
// A READ OF THE EVENT LOG, deliberately, and the opposite call from migration
// `0032_task_waiting_reason`. That one argued for a column because the waiting
// list is polled every five seconds and a per-card query per poll is a query
// per card per poll. This is not that: it is one query for the whole board,
// run when a person types a command, and it answers a question the columns
// cannot answer at all. A session that asks a question WHILE STILL WORKING
// does not move its card on purpose, so no column changes and there is nothing
// for a column to have recorded.
//
// The other reason not to add a column: `blocked` and `still working` are two
// facts about one moment, and the moment is already written down. A column
// would be a second copy of it that has to be kept in step.

// reportWindow is how far back a report is read.
//
// A session that said something a week ago is not part of what is running now,
// and a dispatcher asking "which of these want me" is asking about today. The
// bound also keeps the scan off the whole history of the machine.
const reportWindow = 7 * 24 * time.Hour

// reportScan bounds the rows read, whatever the window holds.
//
// Belt and braces with reportWindow: a board that submitted a hundred thousand
// events in a week should still answer this command. Newest first, so what is
// dropped is the oldest, which is what a report is least likely to be.
const reportScan = 5000

// Report kinds. These are the `kind` written into the event payload.
const (
	// ReportFinished is `atrium finish`: the work is over and there may be a
	// recap on the card saying what it was.
	ReportFinished = "finished"
	// ReportAsked is `atrium ask`: a question, which the card carries in
	// `why`. Blocked says whether the session stopped to wait for the answer.
	ReportAsked = "asked"
)

// AgentReport is the last thing a session said about itself.
type AgentReport struct {
	// Kind is ReportFinished or ReportAsked.
	Kind string `json:"kind"`
	// Ask is the question, on a ReportAsked. Present on the card as `why` too,
	// and carried here so a reader can tell an agent's question from a `why`
	// the operator typed.
	Ask string `json:"ask,omitempty"`
	// Blocked is whether the session stopped. `atrium ask --continue` is a
	// question from a session that is carrying on, and that is a different
	// amount of hurry.
	Blocked bool `json:"blocked,omitempty"`
	// At is when it was said.
	At time.Time `json:"at"`
}

// eventPayload is the shape both writers use. Fields not on a given kind stay
// at their zero value.
type eventPayload struct {
	By      string `json:"by"`
	Kind    string `json:"kind"`
	Ask     string `json:"ask"`
	Blocked bool   `json:"blocked"`
}

// LatestAgentReports returns the newest report per card, for cards that made
// one.
//
// One query for the whole board rather than one per card. Rows come back
// newest first, so the first report seen for a card is its latest and every
// older one is skipped.
//
// Events that are not an agent reporting are ignored rather than refused. The
// `submitted` kind carries more than these two things, and a payload shape
// that does not parse is an event this does not know about, which is not an
// error.
func (s *Store) LatestAgentReports() (map[string]AgentReport, error) {
	out := map[string]AgentReport{}
	err := s.guard(func() error {
		rows, err := s.db.Query(
			`SELECT task_id, at, payload FROM event
			 WHERE kind = ? AND at >= ?
			 ORDER BY at DESC, id DESC LIMIT ?`,
			EventSubmitted, ts(now().Add(-reportWindow)), reportScan)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var taskID, at, pay string
			if err := rows.Scan(&taskID, &at, &pay); err != nil {
				return err
			}
			if _, seen := out[taskID]; seen {
				continue
			}
			var p eventPayload
			if err := json.Unmarshal([]byte(pay), &p); err != nil {
				continue
			}
			if p.By != "agent" {
				continue
			}
			if p.Kind != ReportFinished && p.Kind != ReportAsked {
				continue
			}
			when, err := parseTS(at)
			if err != nil {
				continue
			}
			out[taskID] = AgentReport{
				Kind: p.Kind, Ask: p.Ask, Blocked: p.Blocked, At: when,
			}
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
