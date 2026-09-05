package store

import (
	"database/sql"
	"errors"
	"fmt"
	neturl "net/url"
	"strings"
)

// The inbox: work that is real and has no session yet.
//
// Atrium owns this and does not fill it. Whoever posts an item did the reading,
// and atrium never learns what a `source` means: `github`, `zendesk` and `ci`
// are strings that become a badge. That is the whole reason intake can serve a
// system nobody has thought of yet. See docs/intake-design.md.
//
// An offered card sits in `backlog`, which has existed since the first
// migration and which nothing has ever created a card in. It is not a new
// status and does not want to be: see the note on migration 0025.

// IntakeItem is one piece of work somebody found, normalized.
//
// Everything here is a fact about the item. Nothing here is a fact about
// atrium, which is what keeps a source from having to know how a card works.
type IntakeItem struct {
	// Source is the system this came from. Required, lowercased.
	Source string `json:"source"`
	// ExternalID is that system's own identifier. Required.
	ExternalID string `json:"external_id"`
	// URL is the way back to the thing itself.
	URL string `json:"url"`
	// Title is what to call the card. Falls back to source and identifier.
	Title string `json:"title"`
	// Why is the one line a human reads later.
	//
	// For a support case this should be short and say nothing a customer said.
	// docs/intake-design.md has the rule and the reason: this lands in a
	// database with no encryption, behind a board with no login, reachable
	// from elsewhere the moment a share is up.
	Why string `json:"why"`
	// Tags are what to call this work. The link between a support case and the
	// engineering cards it spawns is a shared tag and nothing else.
	Tags []string `json:"tags"`
	// SuggestedCwd is where the work would happen. It may not exist yet, and
	// for a support case there may be no answer at all, which is precisely why
	// an offered card can exist without one.
	SuggestedCwd string `json:"suggested_cwd"`
	// Prompt is the first instruction whoever starts this should get.
	Prompt string `json:"prompt"`
	// Runner names the harness to start, if the source has an opinion.
	Runner string `json:"runner"`

	// THERE IS NO `SuggestedPriority`, and that is a decision rather than an
	// omission.
	//
	// Priority is the operator's judgement about their own attention, which is
	// the same argument that keeps the status column human. A machine that can
	// set it will raise everything it produces, and a field a source can write
	// `high` into says nothing within a week.
	//
	// A field here would have nowhere to live either. An offered item IS a task
	// in `backlog` rather than a row in an inbox table, so the only place to
	// put a suggestion is the card, which is exactly what it must not touch.
	//
	// A source that wants to say something is urgent already can: `Tags`. A tag
	// is visible in the inbox, filters like everything else, and turns into a
	// priority only when a person reads it and decides. Suggesting and setting
	// stay different acts, which is the whole rule.
}

// SafeURL keeps a link that a browser can be given, and drops one it cannot.
//
// Only `http` and `https`. Anything else comes back empty, and the card shows
// the identifier as text instead.
//
// This is here rather than only in the board because of where the data comes
// from. A source is a script reading GitHub or Zendesk, and whatever it prints
// becomes a link on a card. HTML escaping protects the ATTRIBUTE and does
// nothing about the SCHEME: `javascript:alert(1)` survives escaping intact and
// runs in the board's own context, where the settings, the grouping expression
// and every card live. The board checks too, and both checks are cheap.
//
// An allow list rather than a deny list, because the set of schemes a browser
// will act on is not a set this code gets to enumerate.
func SafeURL(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	u, err := neturl.Parse(value)
	if err != nil {
		return ""
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return value
	}
	return ""
}

// IntakeKey is the canonical deduplication key for an item.
//
// Canonicalized here and nowhere else, so that two scripts spelling it
// `github` and `GitHub` raise one card rather than two. The source folds case
// because a system name is a name; the identifier does not, because
// `openziti/ziti#4211` and `OpenZiti/Ziti#4211` are the same repository but
// `AbC` and `abc` are not the same Zendesk ticket in every tracker there has
// ever been, and quietly merging two items is worse than showing two.
//
// The unit separator joins them because it cannot occur in either half, so
// `a` + `b:c` and `a:b` + `c` cannot collide.
func IntakeKey(source, externalID string) string {
	source = strings.ToLower(strings.TrimSpace(source))
	externalID = strings.TrimSpace(externalID)
	if source == "" || externalID == "" {
		return ""
	}
	return source + "\x1f" + externalID
}

// Normalize trims an item and reports whether it can be keyed.
//
// Both halves of the key are required. An item with no source cannot be
// deduplicated against and an item with no identifier cannot be deduplicated
// at all, and a source that produces either would fill the board with the same
// work on every tick, which is the one failure mode a poller has.
func (it *IntakeItem) Normalize() error {
	it.Source = strings.ToLower(strings.TrimSpace(it.Source))
	it.ExternalID = strings.TrimSpace(it.ExternalID)
	it.URL = SafeURL(it.URL)
	it.Title = strings.TrimSpace(it.Title)
	it.Why = strings.TrimSpace(it.Why)
	it.SuggestedCwd = strings.TrimSpace(it.SuggestedCwd)
	it.Prompt = strings.TrimSpace(it.Prompt)
	it.Runner = strings.TrimSpace(it.Runner)

	if it.Source == "" {
		return errors.New("an intake item needs a source, so it can be told apart from another tracker's")
	}
	if it.ExternalID == "" {
		return errors.New("an intake item needs the identifier its own system uses, " +
			"or the same work arrives again on every tick")
	}
	if it.Title == "" {
		it.Title = it.Source + " " + it.ExternalID
	}
	it.Tags = NormalizeTags(it.Tags)
	return nil
}

// Offer records an item as a card with no runner, or returns the card that
// already carries it.
//
// The second return value is whether this call created it. A source is
// expected to post everything it can see on every tick and let atrium work out
// what is new, because the alternative is every source keeping its own memory
// of what it has already reported.
//
// An item already offered, already started, already finished or already swept
// all answer the same way: this is known, here is the card. Especially the
// swept one. A card raised, worked and archived must not come back the next
// time the source that raised it runs.
func (s *Store) Offer(item IntakeItem) (*Task, bool, error) {
	if err := item.Normalize(); err != nil {
		return nil, false, err
	}
	key := IntakeKey(item.Source, item.ExternalID)

	var (
		task    *Task
		created bool
	)
	err := s.guard(func() error {
		task, created = nil, false

		// Checked and inserted inside one guarded call. The store holds a
		// single connection, so this is serialized against every other writer
		// and two ticks arriving together cannot both find nothing.
		existing, err := s.getBy(`intake_key = ?`, key)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if existing != nil {
			task = existing
			return nil
		}

		rank, err := s.topRank(StatusBacklog)
		if err != nil {
			return err
		}
		n := now()
		t := &Task{
			ID: newID(), Title: item.Title, Why: item.Why,
			Worktree: item.SuggestedCwd, Runner: item.Runner,
			Status: StatusBacklog, CreatedAt: n, LastActivityAt: n,
			Overrides: map[string]string{}, Rank: rank, Tags: item.Tags,
			ExternalID: item.ExternalID, Source: item.Source, URL: item.URL,
			Prompt: item.Prompt, IntakeKey: key,
		}
		if err := s.insertTask(t); err != nil {
			return err
		}
		if err := s.appendEvent(t.ID, EventCreated, map[string]any{
			"offered": map[string]any{
				"source": item.Source, "external_id": item.ExternalID, "url": item.URL,
			},
		}); err != nil {
			return err
		}
		task, created = t, true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return task, created, nil
}

// Offered lists the inbox, newest first.
//
// Archived items are excluded for the same reason every other list excludes
// them: this answers what wants attention now.
func (s *Store) Offered() ([]*Task, error) {
	var out []*Task
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT ` + taskColumns + ` FROM task
			WHERE status = 'backlog' AND archived_at = ''
			ORDER BY rank ASC, created_at DESC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			t, err := scanTask(rows)
			if err != nil {
				return err
			}
			out = append(out, t)
		}
		return rows.Err()
	})
	return out, err
}

// Claim gives an existing card a wire name and the rest of what a runner
// reports about itself.
//
// This is how an offered card becomes a started one. Register cannot do it:
// the card has no wire name to match on and no pid for the fallback, so
// Register would find nothing and create a second card, leaving the item in
// the inbox and its session on a card with no link back to what it was for.
//
// Refused when the card already has a wire name. That would be renaming a live
// session, and every card the agent endpoints resolve is resolved by that
// name.
func (s *Store) Claim(taskID string, obs Observed) (*Task, error) {
	obs.WireName = s.Qualify(obs.WireName)
	if strings.TrimSpace(obs.WireName) == "" {
		return nil, errors.New("a card cannot be claimed without a name to claim it under")
	}
	var out *Task
	err := s.guard(func() error {
		t, err := s.getBy(`id = ?`, taskID)
		if err != nil {
			return err
		}
		if t.WireName != "" {
			return fmt.Errorf("card %s already answers to %s", taskID, t.WireName)
		}
		// Whatever the item suggested stands unless the runner reported
		// something, which is the same observed-versus-stored rule as
		// everywhere else. A suggested directory is a guess by a source; the
		// directory the runner actually started in is a fact.
		if obs.Worktree == "" {
			obs.Worktree = t.Worktree
		}
		if obs.Runner == "" {
			obs.Runner = t.Runner
		}
		if err := s.refreshObserved(t, obs); err != nil {
			return err
		}
		out = t
		return nil
	})
	return out, err
}

// SetPrompt replaces the instruction a card carries.
//
// Editing what an offered item says to do before starting it is the whole
// point of showing it rather than starting it, and a source's default wording
// is a guess by something that has never read the ticket.
func (s *Store) SetPrompt(id, prompt string) error {
	return s.guard(func() error {
		res, err := s.db.Exec(`UPDATE task SET prompt = ? WHERE id = ?`,
			strings.TrimSpace(prompt), id)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err == nil && n == 0 {
			return fmt.Errorf("no card %s", id)
		}
		return nil
	})
}
