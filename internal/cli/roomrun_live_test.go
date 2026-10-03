package cli

import (
	"reflect"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

func TestSelectLiveServesRunningAndWaitingAndParkedCardsAndNoOthers(t *testing.T) {
	now := time.Now()
	card := func(status, branch string) *store.Task {
		return &store.Task{Status: status, Repo: "r", Branch: branch}
	}
	parked := card(store.StatusNeedsInput, "fix/parked")
	parked.ParkedAt = &now
	tasks := []*store.Task{
		card(store.StatusRunning, "fix/running"),
		card(store.StatusNeedsInput, "fix/asking"),
		card(store.StatusNeedsPermission, "fix/perm"),
		// A parked card has no process but keeps its status, and a review it owes keeps being served.
		parked,
		// Not live.
		card(store.StatusDone, "fix/done"),
		card(store.StatusShelved, "fix/shelved"),
		card(store.StatusDead, "fix/dead"),
		card(store.StatusBacklog, "fix/backlog"),
		// No branch, and another repository.
		card(store.StatusRunning, ""),
		{Status: store.StatusRunning, Repo: "elsewhere", Branch: "fix/other"},
		// The same folder name but another owner, when the card recorded one.
		{Status: store.StatusRunning, Repo: "r", Org: "someone-else", Branch: "fix/theirs"},
		{Status: store.StatusRunning, Repo: "r", Org: "O", Host: "github.com", Branch: "fix/mine"},
	}
	got := selectLive("github/o/r", tasks)
	want := []string{"fix/running", "fix/asking", "fix/perm", "fix/parked", "fix/mine"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}
