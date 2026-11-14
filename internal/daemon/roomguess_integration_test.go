//go:build integration

package daemon

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func guessNotices(t *testing.T, d *Daemon, id string) int {
	t.Helper()
	evs, err := d.st.Events(id, 50)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range evs {
		if e.Kind == store.EventNotified && strings.Contains(string(e.Payload), NoticeRoomGuess) {
			n++
		}
	}
	return n
}

func TestAHookThatGuessedItsRoomRaisesOneNoticePerCard(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	task, _, err := d.st.Register(store.Observed{WireName: "guesser", Worktree: "/tmp/guesser", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := d.st.Register(store.Observed{WireName: "sure", Worktree: "/tmp/sure", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	d.onActivity(ActivityEvent{Agent: "guesser", TaskID: task.ID, Event: "tool-start", Tool: "Bash", RoomSource: "daemon.json"})
	if got := guessNotices(t, d, task.ID); got != 1 {
		t.Fatalf("notices after the first guess = %d, want 1", got)
	}
	d.onActivity(ActivityEvent{Agent: "guesser", TaskID: task.ID, Event: "tool-start", Tool: "Bash", RoomSource: "daemon.json"})
	if got := guessNotices(t, d, task.ID); got != 1 {
		t.Errorf("notices after the second guess = %d, want still 1", got)
	}
	d.onActivity(ActivityEvent{Agent: "sure", TaskID: other.ID, Event: "tool-start", Tool: "Bash"})
	if got := guessNotices(t, d, other.ID); got != 0 {
		t.Errorf("a hook that named its room raised %d notices, want 0", got)
	}
}
