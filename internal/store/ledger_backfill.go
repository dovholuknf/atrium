package store

import (
	"encoding/json"
	"strings"
	"time"
)

// Putting the last two weeks of launched cards on the ledger, once.
//
// The evidence is thin and every item this makes says so. It does NOT use
// LatestAgentReports, which reads only seven days, skips progress, blocked and
// question, and does not decode a report's status. It reads each card's own
// `submitted` events. The hot window may already have dropped some of them,
// and `recap` and `report_sha` may describe different reports, so each item is
// marked `inferred`. It is a starting list to rule on, not a reconstruction.

// SettingLedgerBackfill records that the backfill finished, so it runs once.
const SettingLedgerBackfill = "work_ledger_backfilled_at"

// LedgerBackfillWindow is how far back the backfill looks. Older launched
// cards stay out: flagging a year of history would bury the ones that matter.
const LedgerBackfillWindow = 14 * 24 * time.Hour

// BackfillResult is what the backfill made.
type BackfillResult struct {
	Skipped  bool `json:"skipped"` // it had already run
	Items    int  `json:"items"`
	Reported int  `json:"reported"`
	Ended    int  `json:"ended"`
	Open     int  `json:"open"`
	Unknown  int  `json:"unknown"`
}

// BackfillWorkLedger gives every card launched by another session in the last
// fourteen days an item, marked inferred. `liveness` says whether each card's
// session is there, which only the daemon can answer.
//
// Restart safe: an item already there is left alone, and the setting is
// written only at the end, so a crash part way runs it again without doubling
// anything.
func (s *Store) BackfillWorkLedger(liveness func(*Task) Liveness) (*BackfillResult, error) {
	res := &BackfillResult{}
	done, err := s.Setting(SettingLedgerBackfill)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(done) != "" {
		res.Skipped = true
		return res, nil
	}
	var cards []*Task
	err = s.guard(func() error {
		cards = nil
		rows, err := s.db.Query(`SELECT `+taskColumns+` FROM task
			WHERE created_at >= ? AND spawned_by != '' AND spawned_by != ?
			ORDER BY created_at ASC`, ts(now().Add(-LedgerBackfillWindow)), HumanLauncher)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			t, err := scanTask(rows)
			if err != nil {
				return err
			}
			cards = append(cards, t)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	for _, t := range cards {
		if !t.Launched() || !hasOriginAgent(t.Tags) {
			continue
		}
		live := liveness(t)
		made, state, err := s.backfillOne(t, live)
		if err != nil {
			return res, err
		}
		if !made {
			continue
		}
		res.Items++
		switch state {
		case WorkReported:
			res.Reported++
		case WorkEnded:
			res.Ended++
		default:
			res.Open++
			if live == LiveUnknown {
				res.Unknown++
			}
		}
	}
	if err := s.SetSetting(SettingLedgerBackfill, ts(now())); err != nil {
		return res, err
	}
	return res, nil
}

// hasOriginAgent is the `origin:agent` tag. The daemon's OriginAgentTag is the
// same string, and a test pins the two together.
func hasOriginAgent(tags []string) bool {
	for _, t := range tags {
		if strings.EqualFold(strings.TrimSpace(t), "origin:agent") {
			return true
		}
	}
	return false
}

// parsedReport is one agent report read back out of a `submitted` event.
type parsedReport struct {
	at     time.Time
	status string
	sha    string
}

// newestReport parses a card's `submitted` events, newest first, for the
// newest one an agent made. Bounded, since this runs over every card.
func newestReport(q querier, taskID string) (*parsedReport, error) {
	rows, err := q.Query(`SELECT at, payload FROM event WHERE task_id = ? AND kind = ?
		ORDER BY at DESC, id DESC LIMIT 50`, taskID, EventSubmitted)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var at, payload string
		if err := rows.Scan(&at, &payload); err != nil {
			return nil, err
		}
		var p struct {
			By     string `json:"by"`
			Kind   string `json:"kind"`
			Report string `json:"report"`
			Status string `json:"status"`
			SHA    string `json:"sha"`
		}
		if json.Unmarshal([]byte(payload), &p) != nil || p.By != "agent" {
			continue
		}
		r := &parsedReport{sha: strings.TrimSpace(p.SHA)}
		r.at, _ = parseTS(at)
		switch {
		case p.Kind == "report" && p.Report != "":
			r.status = p.Report
		case p.Kind == "finished" && p.Status == StatusDone:
			r.status = "done"
		case p.Kind == "finished" && p.Status != "":
			r.status = p.Status
		default:
			continue
		}
		return r, nil
	}
	return nil, rows.Err()
}

// backfillOne makes one card's item, in one transaction.
func (s *Store) backfillOne(t *Task, live Liveness) (bool, string, error) {
	made, state := false, ""
	err := s.inTx(func(tx *Tx) error {
		made, state = false, WorkOpen
		rep, err := newestReport(tx, t.ID)
		if err != nil {
			return err
		}
		ended := 0
		switch {
		case rep != nil && rep.status == "done":
			state = WorkReported
		case live == Gone:
			state = WorkEnded
		}
		if live == Gone {
			ended = 1
		}
		n := ts(now())
		res, err := tx.Exec(`INSERT INTO work_item (task_id, handle, title, worktree, launcher_id,
				launcher_handle, arbiter_id, arbiter_handle, state, state_at, state_by, ended_generation,
				inferred, created_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,1,?)
			ON CONFLICT (task_id) DO NOTHING`,
			t.ID, t.WireName, t.DisplayTitle(), t.Worktree, t.SpawnedByID, t.SpawnedBy,
			t.SpawnedByID, t.SpawnedBy, state, n, ByAtrium, ended, ts(t.CreatedAt))
		if err != nil {
			return err
		}
		if got, err := res.RowsAffected(); err != nil || got == 0 {
			return err
		}
		made = true
		if _, err := s.insertLog(tx, t.ID, logRow{kind: LogAtrium, by: ByAtrium,
			text: "put on the ledger by the backfill, from the card's own records: liveness " + string(live)}); err != nil {
			return err
		}
		if rep != nil {
			var outputs *WorkOutputs
			// report_sha names the latest `done` that carried one. Taken as
			// this report's output only when this report named that sha.
			if rep.status == "done" && rep.sha != "" && rep.sha == strings.TrimSpace(t.ReportSHA) {
				outputs = &WorkOutputs{Commits: []string{rep.sha}}
				if t.ReportUnverified {
					outputs.Unverified = []string{rep.sha}
				}
			}
			// The event holds the status and not the words, so the row has no
			// text and says where it came from.
			id, err := s.insertLog(tx, t.ID, logRow{kind: LogReport, by: ByWorker, status: rep.status,
				note: "inferred from a submitted event", outputs: outputs, at: rep.at})
			if err != nil {
				return err
			}
			if state == WorkReported {
				o := "{}"
				if outputs != nil {
					o = outputs.json()
				}
				if _, err := tx.Exec(`UPDATE work_item SET latest_report_id = ?, outputs = ? WHERE task_id = ?`,
					id, o, t.ID); err != nil {
					return err
				}
			}
		}
		if strings.TrimSpace(t.Recap) != "" {
			// Nothing ties the recap to one report, so it is not one.
			if _, err := s.insertLog(tx, t.ID, logRow{kind: LogAtrium, by: ByAtrium, text: t.Recap,
				note: "recap at backfill"}); err != nil {
				return err
			}
		}
		s.ledgerChanged(tx, t.ID)
		return nil
	})
	return made, state, err
}
