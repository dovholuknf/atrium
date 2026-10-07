package store

import (
	"database/sql"
	"strings"
	"time"
)

// WHAT ATRIUM SAID TO A SESSION, AND WHAT THE TURN AFTER IT COST. Atrium is the only party that knows why a turn
// happened, and Claude's transcript is the only one that knows what it cost. This file keeps the join. See 0092 and
// internal/daemon/turncost.go, which reads the transcript.

// The cause kinds of a delivery. A kind is what the text was, not how it got there.
const (
	// DeliveryOperator is the operator typing, or a message they sent from the board.
	DeliveryOperator = "operator"
	// DeliverySay is another session's atrium_say.
	DeliverySay = "say"
	// DeliveryReport is a worker's report, relayed to its launcher.
	DeliveryReport = "report"
	// DeliveryNotice is atrium's own words that have no kind of their own.
	DeliveryNotice = "notice"
	// DeliveryNudge is "ended without a report", "idle, nudged".
	DeliveryNudge = "nudge"
	// DeliveryDeploy is a deploy hold or its wake.
	DeliveryDeploy = "deploy"
	// DeliveryCycle is a step of a context cycle.
	DeliveryCycle = "context-cycle"
	// DeliveryRestartWake is the after-restart or after-exit wake.
	DeliveryRestartWake = "restart-wake"
)

// The replies a costed turn made.
const (
	// ReplyWork used a tool.
	ReplyWork = "work"
	// ReplyText said something that is not an ack.
	ReplyText = "text"
	// ReplyAck said only that it got the message.
	ReplyAck = "ack"
	// ReplyNone said nothing and used no tool.
	ReplyNone = "none"
)

// Delivery is one text atrium put in front of a session.
type Delivery struct {
	ID        string    `json:"id"`
	TaskID    string    `json:"task_id"`
	Name      string    `json:"name,omitempty"`
	From      string    `json:"from,omitempty"`
	Kind      string    `json:"kind"`
	At        time.Time `json:"at"`
	MessageID string    `json:"message_id,omitempty"`
	Costed    bool      `json:"costed"`
	// Shared is a delivery that landed in a turn another delivery already carries the cost of.
	Shared       bool    `json:"shared,omitempty"`
	Model        string  `json:"model,omitempty"`
	Replies      int     `json:"replies"`
	Input        int64   `json:"input"`
	Output       int64   `json:"output"`
	CacheWrite5m int64   `json:"cache_write_5m"`
	CacheWrite1h int64   `json:"cache_write_1h"`
	CacheRead    int64   `json:"cache_read"`
	Cost         float64 `json:"cost"`
	Reply        string  `json:"reply,omitempty"`
}

// TurnCost is what one turn spent, to land on the deliveries that started it.
type TurnCost struct {
	Model        string
	Replies      int
	Input        int64
	Output       int64
	CacheWrite5m int64
	CacheWrite1h int64
	CacheRead    int64
	Cost         float64
	Reply        string
}

// DeliveryKind is the kind of a queued message: the one it was queued with, else what its sender says it is.
func DeliveryKind(from, cause string) string {
	switch {
	case cause != "":
		return cause
	case from == "":
		return DeliveryOperator
	case from == "atrium":
		return DeliveryNotice
	}
	return DeliverySay
}

// RecordDelivery writes a delivery that has not been costed yet.
func (s *Store) RecordDelivery(d *Delivery) error {
	if d.ID == "" {
		d.ID = newID()
	}
	if d.At.IsZero() {
		d.At = now()
	}
	return s.guard(func() error { return insertDelivery(s.db, d) })
}

func insertDelivery(db *sql.DB, d *Delivery) error {
	_, err := db.Exec(`INSERT INTO delivery (id, task_id, from_name, kind, at, message_id) VALUES (?,?,?,?,?,?)`,
		d.ID, d.TaskID, d.From, d.Kind, ts(d.At), d.MessageID)
	return err
}

// recordMessageDeliveries is MarkDelivered's half: every message that reached a session is a delivery. An operator
// message typed in at the terminal is left to the prompt hook, which sees it as it is submitted.
func recordMessageDeliveries(db *sql.DB, taskID, via string, ids []string) error {
	for _, id := range ids {
		var from, cause string
		if err := db.QueryRow(`SELECT from_peer, cause FROM message WHERE id = ?`, id).Scan(&from, &cause); err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return err
		}
		kind := DeliveryKind(from, cause)
		if kind == DeliveryOperator && via == "terminal" {
			continue
		}
		if err := insertDelivery(db, &Delivery{
			ID: newID(), TaskID: taskID, From: from, Kind: kind, At: now(), MessageID: id}); err != nil {
			return err
		}
	}
	return nil
}

// SetMessageCause tags a queued message with the kind it is.
func (s *Store) SetMessageCause(id, cause string) error {
	if id == "" || cause == "" {
		return nil
	}
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE message SET cause = ? WHERE id = ?`, cause, id)
		return err
	})
}

// CostDeliveries lands a turn's cost on the deliveries of a card made up to the Stop that ended it. The first carries
// the cost and the rest are marked as sharing it, so a sum never counts a turn twice. It returns how many it costed.
func (s *Store) CostDeliveries(taskID string, upTo time.Time, c TurnCost) (int, error) {
	n := 0
	err := s.guard(func() error {
		n = 0
		rows, err := s.db.Query(`SELECT id FROM delivery WHERE task_id = ? AND costed_at = '' AND at <= ?
			ORDER BY at, id`, taskID, ts(upTo))
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil || len(ids) == 0 {
			return err
		}
		at := ts(now())
		for i, id := range ids {
			if i == 0 {
				_, err = s.db.Exec(`UPDATE delivery SET costed_at = ?, model = ?, replies = ?, input = ?, output = ?,
					cache_write_5m = ?, cache_write_1h = ?, cache_read = ?, cost = ?, reply = ? WHERE id = ?`,
					at, c.Model, c.Replies, c.Input, c.Output, c.CacheWrite5m, c.CacheWrite1h, c.CacheRead, c.Cost,
					c.Reply, id)
			} else {
				_, err = s.db.Exec(`UPDATE delivery SET costed_at = ?, group_id = ?, reply = ? WHERE id = ?`,
					at, ids[0], c.Reply, id)
			}
			if err != nil {
				return err
			}
		}
		n = len(ids)
		return nil
	})
	return n, err
}

// CostRow is what one kind spent on one card on one day.
type CostRow struct {
	Day          string  `json:"day"`
	TaskID       string  `json:"task_id"`
	Name         string  `json:"name,omitempty"`
	Kind         string  `json:"kind"`
	Turns        int     `json:"turns"`
	Input        int64   `json:"input"`
	Output       int64   `json:"output"`
	CacheWrite   int64   `json:"cache_write"`
	CacheRead    int64   `json:"cache_read"`
	Cost         float64 `json:"cost"`
	NoiseTurns   int     `json:"noise_turns"`
	NoiseTokens  int64   `json:"noise_tokens"`
	NoiseCost    float64 `json:"noise_cost"`
	NoneTurns    int     `json:"none_turns"`
	AckTurns     int     `json:"ack_turns"`
	DeliveryRows int     `json:"deliveries"`
}

// CostReport is the room's turn cost by cause.
type CostReport struct {
	Since time.Time `json:"since"`
	Until time.Time `json:"until"`
	// Rows is by day, card and kind. Whoever draws it sums what it wants.
	Rows []CostRow `json:"rows"`
	// Top is the costliest turns an atrium text started. The operator's are not atrium's.
	Top []Delivery `json:"top"`
	// Pending is deliveries still waiting for their turn to end.
	Pending int `json:"pending"`
}

const deliveryCols = `d.id, d.task_id, COALESCE(NULLIF(t.alias,''), t.wire_name, ''), d.from_name, d.kind, d.at,
	d.message_id, d.costed_at, d.group_id, d.model, d.replies, d.input, d.output, d.cache_write_5m, d.cache_write_1h,
	d.cache_read, d.cost, d.reply`

// DeliveryCosts reads the report for a span. Tokens are summed in SQL and the day is the UTC date.
func (s *Store) DeliveryCosts(since, until time.Time, topN int) (*CostReport, error) {
	if topN <= 0 || topN > 100 {
		topN = 10
	}
	rep := &CostReport{Since: since.UTC(), Until: until.UTC(), Rows: []CostRow{}, Top: []Delivery{}}
	err := s.guard(func() error {
		rep.Rows, rep.Top = []CostRow{}, []Delivery{}
		rows, err := s.db.Query(`SELECT substr(d.at, 1, 10), d.task_id, COALESCE(NULLIF(t.alias,''), t.wire_name, ''),
				d.kind, COUNT(*), SUM(CASE WHEN d.group_id = '' THEN 1 ELSE 0 END), COALESCE(SUM(d.input),0), COALESCE(SUM(d.output),0),
				COALESCE(SUM(d.cache_write_5m + d.cache_write_1h),0), COALESCE(SUM(d.cache_read),0),
				COALESCE(SUM(d.cost),0),
				SUM(CASE WHEN d.reply IN ('none','ack') AND d.group_id = '' THEN 1 ELSE 0 END),
				COALESCE(SUM(CASE WHEN d.reply IN ('none','ack') AND d.group_id = ''
					THEN d.input + d.output + d.cache_write_5m + d.cache_write_1h + d.cache_read ELSE 0 END),0),
				COALESCE(SUM(CASE WHEN d.reply IN ('none','ack') AND d.group_id = '' THEN d.cost ELSE 0 END),0),
				SUM(CASE WHEN d.reply = 'none' AND d.group_id = '' THEN 1 ELSE 0 END),
				SUM(CASE WHEN d.reply = 'ack' AND d.group_id = '' THEN 1 ELSE 0 END)
			FROM delivery d LEFT JOIN task t ON t.id = d.task_id
			WHERE d.at >= ? AND d.at <= ? AND d.costed_at != ''
			GROUP BY 1, 2, 4 ORDER BY 1 DESC, 2, 4`, ts(since), ts(until))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r CostRow
			var noise, none, ack, turns sql.NullInt64
			if err := rows.Scan(&r.Day, &r.TaskID, &r.Name, &r.Kind, &r.DeliveryRows, &turns, &r.Input, &r.Output,
				&r.CacheWrite, &r.CacheRead, &r.Cost, &noise, &r.NoiseTokens, &r.NoiseCost, &none, &ack); err != nil {
				return err
			}
			r.NoiseTurns, r.NoneTurns, r.AckTurns = int(noise.Int64), int(none.Int64), int(ack.Int64)
			// A turn is a delivery that carries a cost: the rest only shared it.
			r.Turns = int(turns.Int64)
			rep.Rows = append(rep.Rows, r)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		top, err := s.db.Query(`SELECT `+deliveryCols+` FROM delivery d LEFT JOIN task t ON t.id = d.task_id
			WHERE d.at >= ? AND d.at <= ? AND d.costed_at != '' AND d.group_id = '' AND d.kind != ?
			ORDER BY d.input + d.output + d.cache_write_5m + d.cache_write_1h + d.cache_read DESC LIMIT ?`,
			ts(since), ts(until), DeliveryOperator, topN)
		if err != nil {
			return err
		}
		defer top.Close()
		for top.Next() {
			d, err := scanDelivery(top)
			if err != nil {
				return err
			}
			rep.Top = append(rep.Top, *d)
		}
		if err := top.Err(); err != nil {
			return err
		}
		return s.db.QueryRow(`SELECT COUNT(*) FROM delivery WHERE costed_at = '' AND at >= ?`, ts(since)).Scan(&rep.Pending)
	})
	return rep, err
}

func scanDelivery(r rowScanner) (*Delivery, error) {
	var (
		d           Delivery
		at, costed  string
		group, name string
	)
	if err := r.Scan(&d.ID, &d.TaskID, &name, &d.From, &d.Kind, &at, &d.MessageID, &costed, &group, &d.Model,
		&d.Replies, &d.Input, &d.Output, &d.CacheWrite5m, &d.CacheWrite1h, &d.CacheRead, &d.Cost, &d.Reply); err != nil {
		return nil, err
	}
	d.Name = name
	d.Costed, d.Shared = costed != "", group != ""
	var err error
	if d.At, err = parseTS(at); err != nil {
		return nil, err
	}
	return &d, nil
}

// IsNoiseReply is a turn whose reply was only an ack, or nothing.
func IsNoiseReply(reply string) bool {
	return reply == ReplyAck || reply == ReplyNone
}

// ClassifyReply says what a turn's replies were. work is any tool use, none is no tool and no words, ack is words
// that only acknowledge.
func ClassifyReply(text string, usedTool bool) string {
	if usedTool {
		return ReplyWork
	}
	t := strings.TrimSpace(text)
	if t == "" {
		return ReplyNone
	}
	if isAck(t) {
		return ReplyAck
	}
	return ReplyText
}

// ackStarts are what an acknowledgement opens with, lower case and without punctuation.
var ackStarts = []string{
	"ok", "okay", "got it", "noted", "thanks", "thank you", "ack", "acknowledged", "understood", "will do",
	"sounds good", "roger", "no response", "nothing to add", "nothing more", "standing by", "waiting", "received",
	"on it", "done", "yes", "great", "perfect", "cool", "sure",
}

// isAck is a short reply that opens like an acknowledgement. Long ones say something.
func isAck(text string) bool {
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '\n' || r == '\t' || r == '.' || r == ',' || r == '!' || r == '-' || r == '\'':
			b.WriteByte(' ')
		}
	}
	words := strings.Fields(b.String())
	if len(words) == 0 {
		// Only symbols or emoji.
		return len([]rune(text)) <= 8
	}
	if len(words) > 12 {
		return false
	}
	joined := strings.Join(words, " ")
	for _, a := range ackStarts {
		if joined == a || strings.HasPrefix(joined, a+" ") {
			return true
		}
	}
	return false
}
