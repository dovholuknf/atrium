package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Actions on a card, written by the operator.
//
// A card can be terminated, shelved, attached to and messaged. All of those
// are things ATRIUM does. None of them is the thing a person does repeatedly,
// which is send the same instruction to whichever agent is in front of them.
//
// The delivery path already exists and is the message queue: typed into the
// terminal when atrium owns one, queued for the next hook when it does not.
// What is new is storing them and putting them on the card.

// AfterKeep leaves the session up for whatever comes next. AfterExit sends the
// prompt and then the harness's own exit keys.
//
// The second one is why this is not a saved snippet. "Write it up and go away"
// is one gesture in somebody's head and two things to a runner.
const (
	AfterKeep = "keep"
	AfterExit = "exit"
)

// MaxActionPrompt bounds what one action may say.
//
// Generous, because a good action prompt is a paragraph rather than a
// sentence, and small enough that this cannot become a place to keep a
// document.
const MaxActionPrompt = 4000

// CardAction is one named thing to say to an agent.
type CardAction struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Prompt is what gets said. Delivered as text, never interpreted.
	Prompt string `json:"prompt"`
	// After is AfterKeep or AfterExit.
	After string `json:"after"`
	// Tag limits this action to cards carrying it. Empty offers it everywhere.
	//
	// "Run the tests" means something different in a Go repo and a docs repo,
	// and a tag is the cut the operator already makes. A runner condition is
	// the other obvious one and both are here, because they answer different
	// questions and neither replaces the other.
	Tag string `json:"tag"`
	// Runner limits this action to cards running one harness. Empty is any.
	Runner    string    `json:"runner"`
	Enabled   bool      `json:"enabled"`
	Sort      int       `json:"sort"`
	CreatedAt time.Time `json:"created_at"`
}

// Offers reports whether this action belongs on a given card.
//
// Both conditions have to pass, and an empty one passes everything. A card
// with no runner is not matched by an action that names one: the action is a
// claim about what the session can do, and a card with no session cannot.
func (a *CardAction) Offers(t *Task) bool {
	if !a.Enabled || t == nil {
		return false
	}
	if a.Runner != "" && !strings.EqualFold(a.Runner, t.Runner) {
		return false
	}
	if a.Tag != "" {
		want := strings.ToLower(strings.TrimSpace(a.Tag))
		found := false
		for _, tag := range t.Tags {
			if strings.EqualFold(tag, want) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

const actionColumns = `id, label, prompt, after, tag, runner, enabled, sort, created_at`

func scanAction(sc interface{ Scan(...any) error }) (*CardAction, error) {
	var (
		a       CardAction
		enabled int
		created string
	)
	if err := sc.Scan(&a.ID, &a.Label, &a.Prompt, &a.After, &a.Tag, &a.Runner,
		&enabled, &a.Sort, &created); err != nil {
		return nil, err
	}
	a.Enabled = enabled != 0
	var err error
	if a.CreatedAt, err = parseTS(created); err != nil {
		return nil, err
	}
	return &a, nil
}

// CardActions lists every configured action, in the order they should read.
func (s *Store) CardActions() ([]*CardAction, error) {
	var out []*CardAction
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT ` + actionColumns +
			` FROM card_action ORDER BY sort ASC, label ASC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			a, err := scanAction(rows)
			if err != nil {
				return err
			}
			out = append(out, a)
		}
		return rows.Err()
	})
	return out, err
}

// CardActionByID returns one action.
func (s *Store) CardActionByID(id string) (*CardAction, error) {
	var a *CardAction
	err := s.guard(func() error {
		row := s.db.QueryRow(`SELECT `+actionColumns+` FROM card_action WHERE id = ?`, id)
		got, err := scanAction(row)
		if err != nil {
			return err
		}
		a = got
		return nil
	})
	return a, err
}

// SaveCardAction creates or replaces one.
func (s *Store) SaveCardAction(a CardAction) (*CardAction, error) {
	a.ID = strings.TrimSpace(a.ID)
	if a.ID == "" {
		a.ID = newID()
	}
	a.Label = strings.TrimSpace(a.Label)
	a.Prompt = strings.TrimSpace(a.Prompt)
	if a.Label == "" {
		return nil, errors.New("an action needs a name, since that is what goes on the button")
	}
	if a.Prompt == "" {
		return nil, errors.New("an action with nothing to say is a button that does nothing")
	}
	if len(a.Prompt) > MaxActionPrompt {
		return nil, errors.New("that prompt is longer than an instruction. " +
			"an action is something you say often, not a document")
	}
	if a.After != AfterExit {
		a.After = AfterKeep
	}
	a.Tag = strings.ToLower(strings.TrimSpace(a.Tag))
	a.Runner = strings.TrimSpace(a.Runner)

	err := s.guard(func() error {
		created := ts(now())
		var existing string
		if err := s.db.QueryRow(
			`SELECT created_at FROM card_action WHERE id = ?`, a.ID).Scan(&existing); err == nil {
			created = existing
		}
		enabled := 0
		if a.Enabled {
			enabled = 1
		}
		_, err := s.db.Exec(`INSERT INTO card_action (`+actionColumns+`)
			VALUES (?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET
				label = excluded.label, prompt = excluded.prompt, after = excluded.after,
				tag = excluded.tag, runner = excluded.runner, enabled = excluded.enabled,
				sort = excluded.sort`,
			a.ID, a.Label, a.Prompt, a.After, a.Tag, a.Runner, enabled, a.Sort, created)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.CardActionByID(a.ID)
}

// DeleteCardAction removes one.
func (s *Store) DeleteCardAction(id string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`DELETE FROM card_action WHERE id = ?`, id)
		return err
	})
}

// SeedCardActions puts the obvious ones there on first run, and never again.
//
// Seeded rather than left empty because a feature whose value is "you write
// your own" starts as an empty list nobody fills in. These three are the ones
// worth having on day one, and the write-yourself-up one is what makes
// `atrium finish` reachable at all: nothing else tells a session that command
// exists.
//
// Recorded by a marker rather than by checking whether the table is empty, so
// deleting all three does not bring them back.
func (s *Store) SeedCardActions() error {
	seeded, err := s.Setting("card_actions_seeded")
	if err != nil || seeded == "yes" {
		return err
	}
	for _, a := range DefaultCardActions() {
		if _, err := s.SaveCardAction(a); err != nil {
			return err
		}
	}
	return s.SetSetting("card_actions_seeded", "yes")
}

// DefaultCardActions are the three worth having before you have written any.
func DefaultCardActions() []CardAction {
	return []CardAction{
		{
			ID: "write-up", Label: "write it up and finish", Enabled: true, Sort: 10,
			After: AfterKeep,
			Prompt: "Summarise what you have done in this session in two or three sentences: " +
				"what changed, and what is worth knowing about it. Then run " +
				"`atrium finish \"<that summary>\"` to record it and mark this work finished. " +
				"Keep the summary short. It is not a transcript.",
		},
		{
			ID: "run-tests", Label: "run the tests", Enabled: true, Sort: 20,
			After:  AfterKeep,
			Prompt: "Run the test suite for this project and tell me what failed, if anything.",
		},
		{
			ID: "wrap-up", Label: "commit and go", Enabled: false, Sort: 30,
			After: AfterExit,
			Prompt: "Commit what you have with a short message describing it, then stop. " +
				"Do not push.",
		},
	}
}

// ActionsFor returns the actions that belong on one card.
func (s *Store) ActionsFor(t *Task) ([]*CardAction, error) {
	all, err := s.CardActions()
	if err != nil {
		return nil, err
	}
	var out []*CardAction
	for _, a := range all {
		if a.Offers(t) {
			out = append(out, a)
		}
	}
	return out, nil
}

// ErrNoAction is returned when an action id names nothing.
var ErrNoAction = sql.ErrNoRows
