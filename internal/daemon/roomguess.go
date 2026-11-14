package daemon

import (
	"log"
)

// roomSourceFile is what a hook sends as room_source when it read this room from daemon.json and not from its env.
const roomSourceFile = "daemon.json"

const roomGuessText = "This session was started outside atrium, so atrium guessed which room it belongs to. " +
	"Resume it from its card on the board, or start it with atrium, so it reaches the right room."

// noteRoomGuess raises one board notice on a card whose hook guessed this room. Once per card, and it never fails its
// caller, the posture of a hook.
func (d *Daemon) noteRoomGuess(taskID string) {
	t, err := d.st.Get(taskID)
	if err != nil {
		return
	}
	fresh, err := d.st.RecordNotice(t.ID, NoticeRoomGuess, "1")
	if err != nil {
		log.Printf("[atrium] could not record a room-guess notice for %s: %v", t.DisplayTitle(), err)
		return
	}
	if !fresh {
		return
	}
	d.holdNotice(t, t, NoticeRoomGuess, roomGuessText)
}
