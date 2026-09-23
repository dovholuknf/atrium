package daemon

import (
	"log"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// Whether the operator has seen a card's latest turn, and answered what it
// asked. See docs/seen-design.md.
//
// The room decides every way of seeing it can observe for itself: a keystroke
// over the attach socket, a prompt submitted, a message sent from the board.
// The one it cannot observe is a window showing the terminal in front of
// somebody, and the board reports that on `POST /v1/tasks/{id}/seen`.
//
// BEST EFFORT, LIKE EVERY HOOK PATH. A failure to record is logged and the
// turn, the prompt and the keystroke go on exactly as before.

// peerPromptWindow is how long after atrium submitted a peer's message a
// prompt is read as that message rather than the operator.
//
// A typed peer message is submitted, and Claude Code fires UserPromptSubmit for
// it as it does for a person. The hook that reports it arrives well inside a
// second. Thirty is generous on a loaded machine and still short of the time it
// takes somebody to read a reply and type their own.
const peerPromptWindow = 30 * time.Second

// loadUnseen fills the in-memory set from the store, so a keystroke after a
// restart still sees a turn that ended before it.
func (d *Daemon) loadUnseen() {
	ids, err := d.st.UnseenCards()
	if err != nil {
		log.Printf("[atrium] could not read which turns are unseen: %v", err)
		return
	}
	for _, id := range ids {
		d.unseen.Store(id, true)
	}
}

// noteTurnForSeen records a turn that ended, and the questions it ended on.
//
// Called only on the path where the Stop hook lets the turn end. A Stop that
// delivers a message sends the model back to work, so that turn is not over.
func (d *Daemon) noteTurnForSeen(taskID string, q store.TurnQuestions) {
	if err := d.st.NoteTurnEnded(taskID, q); err != nil {
		log.Printf("[atrium] could not record a turn ending on %s: %v", taskID, err)
		return
	}
	d.unseen.Store(taskID, true)
	d.publishTask(taskID)
}

// seenPrompted is a prompt submitted to this card. The operator's answers the
// card's questions and sees its turn. A peer's does neither.
func (d *Daemon) seenPrompted(taskID string) {
	if run := d.sup.get(taskID); run != nil && run.promptWasPeer(time.Now()) {
		return
	}
	d.seenAnswered(taskID, store.SeenPrompt)
}

// seenAnswered is the operator replying through any channel.
func (d *Daemon) seenAnswered(taskID, via string) {
	d.unseen.Delete(taskID)
	changed, err := d.st.MarkAnswered(taskID, via)
	if err != nil {
		log.Printf("[atrium] could not record %s answering %s: %v", via, taskID, err)
		return
	}
	if changed {
		d.publishTask(taskID)
	}
}

// seenTyped is an operator keystroke into this card's terminal.
//
// ON THE KEYSTROKE PATH, so it costs one map lookup when there is nothing to
// see, which is almost every keystroke. The write happens off the path.
func (d *Daemon) seenTyped(taskID string) {
	if _, ok := d.unseen.LoadAndDelete(taskID); !ok {
		return
	}
	go func() {
		changed, err := d.st.MarkSeen(taskID, store.SeenTyped, nil)
		if err != nil {
			log.Printf("[atrium] could not record a keystroke seeing %s: %v", taskID, err)
			return
		}
		if changed {
			d.publishTask(taskID)
		}
	}()
}
