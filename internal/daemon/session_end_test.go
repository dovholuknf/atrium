package daemon

import "testing"

// What counts as a session ending.
//
// `clear` and `resume` are both followed immediately by a SessionStart in the
// same directory. Treating either as a death makes the card go to finished and
// come straight back, which on the board is a card flickering out of the
// column you were reading.

func TestClearAndResumeAreNotEndings(t *testing.T) {
	for _, reason := range []string{"clear", "resume", "Clear", " RESUME "} {
		if EndsTheSession(reason) {
			t.Fatalf("%q was treated as the session ending", reason)
		}
	}
}

// Everything else IS an ending, including nothing at all. A runner that says
// nothing has not claimed the session is continuing, and a card left in
// running forever is the worse of the two mistakes.
func TestAnythingElseIsAnEnding(t *testing.T) {
	for _, reason := range []string{"", "exit", "logout", "prompt_input_exit", "other"} {
		if !EndsTheSession(reason) {
			t.Fatalf("%q was not treated as the session ending", reason)
		}
	}
}
