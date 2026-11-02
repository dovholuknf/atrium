package ctl

// Card is the part of a task these tools report. The board's own shape is
// much larger and most of it is for drawing.
type Card struct {
	ID string `json:"id"`
	// LaunchReplay: the room answered with the card an earlier attempt filed under the same launch key.
	LaunchReplay bool     `json:"launch_replay"`
	Title        string   `json:"display_title"`
	Wire         string   `json:"wire_name"`
	Alias        string   `json:"alias"`
	Created      string   `json:"created_at"`
	Status       string   `json:"status"`
	Worktree     string   `json:"worktree"`
	Runner       string   `json:"runner"`
	Why          string   `json:"why"`
	Idle         int      `json:"idle_seconds"`
	Wait         int      `json:"wait_seconds"`
	Superv       bool     `json:"supervised"`
	Tags         []string `json:"tags"`
	// What the card was launched with. See docs/runtime/launch-options-design.md.
	Model         string   `json:"model"`
	Effort        string   `json:"effort"`
	LaunchArgs    []string `json:"launch_args"`
	LaunchEnvKeys []string `json:"launch_env_keys"`
	Activity      struct {
		What string `json:"what"`
	} `json:"activity"`
	Seen *Seen `json:"seen,omitempty"`
}

// Seen is whether the operator has seen a card's latest turn and answered
// its Open Questions. The room's `store.SeenView`, mirrored so internal/link
// learns nothing of the store. See docs/runtime/seen-design.md.
type Seen struct {
	TurnEndedAt string `json:"turn_ended_at,omitempty"`
	SeenAt      string `json:"seen_at,omitempty"`
	SeenVia     string `json:"seen_via,omitempty"`
	// Unseen is the latest turn having ended with nobody looking at it since.
	Unseen        bool     `json:"unseen"`
	OpenQuestions []string `json:"open_questions,omitempty"`
	// QuestionsUnparsed is a turn that had an Open Questions heading whose
	// items could not be read. They exist, and their text is not known.
	QuestionsUnparsed bool   `json:"questions_unparsed,omitempty"`
	QuestionsAt       string `json:"questions_at,omitempty"`
	AnsweredAt        string `json:"answered_at,omitempty"`
	AnsweredVia       string `json:"answered_via,omitempty"`
	// Answered is absent when no turn ever asked anything.
	Answered *bool `json:"answered,omitempty"`
}

// OpenCount is how many questions are owed, counting an unreadable block as
// one so it is never reported as none.
func (s *Seen) OpenCount() int {
	if s == nil || s.Answered == nil || *s.Answered {
		return 0
	}
	if len(s.OpenQuestions) == 0 && s.QuestionsUnparsed {
		return 1
	}
	return len(s.OpenQuestions)
}

// PickCard is the card a wire name belongs to. A wire name can be reused by a card that has
// since died, so a live one is preferred, then the newest, which is how resolvePeer orders an
// alias. Nil when no card answers to it.
func PickCard(tasks []Card, agent string) *Card {
	var best *Card
	for i := range tasks {
		t := &tasks[i]
		if t.Wire != agent {
			continue
		}
		switch {
		case best == nil, best.Status == "dead" && t.Status != "dead":
			best = t
		case (best.Status == "dead") == (t.Status == "dead") && t.Created > best.Created:
			best = t
		}
	}
	return best
}
