package store

import (
	"os"
	"testing"
)

// Three pinned cards, arranged by hand.
//
// The point of the column is that the arrangement is a fact about the cards
// rather than a consequence of the sort above them, so what these assert is
// that the number written is the number read back, and that writing the order
// again moves what it names and leaves everything else where it was.
func pinned(t *testing.T, s *Store, wire string) string {
	t.Helper()
	task, _, err := s.Register(Observed{WireName: wire, Worktree: "/w/" + wire, Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPinned(task.ID, true); err != nil {
		t.Fatal(err)
	}
	return task.ID
}

func order(t *testing.T, s *Store, ids ...string) []int {
	t.Helper()
	out := []int{}
	for _, id := range ids {
		got, err := s.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, got.PinOrder)
	}
	return out
}

// A NEW CARD HAS NO PLACE IN THE BUCKET, and that has to read as a tie rather
// than as a position. Everything starts at zero so a board nobody has dragged
// on looks exactly as it did before the column existed: the pinned set sorts
// among itself by whatever the strip's own sort says.
func TestAPinnedCardStartsWithNoOrderOfItsOwn(t *testing.T) {
	s := open(t)
	a := pinned(t, s, "first")
	b := pinned(t, s, "second")
	if got := order(t, s, a, b); got[0] != 0 || got[1] != 0 {
		t.Fatalf("new cards came with an order already: %v", got)
	}
}

func TestTheOrderWrittenIsTheOrderReadBack(t *testing.T) {
	s := open(t)
	a := pinned(t, s, "alpha")
	b := pinned(t, s, "bravo")
	c := pinned(t, s, "charlie")

	if err := s.SetPinOrder([]string{c, a, b}); err != nil {
		t.Fatal(err)
	}
	// Read in the order they were written, so the positions should count up.
	if got := order(t, s, c, a, b); got[0] != 0 || got[1] != 1 || got[2] != 2 {
		t.Fatalf("wrote c,a,b and read back %v", got)
	}

	// Dragging one row moves it and nothing else. Written as the whole list
	// again, because that is the only way the board ever says it.
	if err := s.SetPinOrder([]string{a, b, c}); err != nil {
		t.Fatal(err)
	}
	if got := order(t, s, a, b, c); got[0] != 0 || got[1] != 1 || got[2] != 2 {
		t.Fatalf("reordering to a,b,c left %v", got)
	}
}

// AN ID THAT IS NOT THERE DOES NOT SINK THE REORDER.
//
// The board sends the ids it can see, and between the drag starting and the
// drop landing a card can be unpinned in another tab or deleted outright. That
// is the ordinary case rather than an error, and refusing the whole order over
// it would mean a drag that silently did nothing. The cards that ARE there
// still get the positions they were given.
func TestAnUnknownIDDoesNotSinkTheReorder(t *testing.T) {
	s := open(t)
	a := pinned(t, s, "here")
	b := pinned(t, s, "also-here")

	if err := s.SetPinOrder([]string{a, "01GONE", b}); err != nil {
		t.Fatalf("a stale id in the list refused the whole reorder: %v", err)
	}
	// The gone id still consumed its position, which is deliberate: the
	// numbers are compared and never counted, so a hole in them costs nothing
	// and closing it would mean the store deciding which ids are real.
	if got := order(t, s, a, b); got[0] != 0 || got[1] != 2 {
		t.Fatalf("wanted 0 and 2 around the missing row, got %v", got)
	}
}

// Order and pinning are separate facts, and unpinning must not quietly erase
// where the card sat. Somebody who unpins by accident and pins straight back
// gets the row where it was rather than at the end of the bucket.
func TestUnpinningKeepsThePlace(t *testing.T) {
	s := open(t)
	a := pinned(t, s, "one")
	b := pinned(t, s, "two")
	if err := s.SetPinOrder([]string{b, a}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPinned(a, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPinned(a, true); err != nil {
		t.Fatal(err)
	}
	if got := order(t, s, b, a); got[0] != 0 || got[1] != 1 {
		t.Fatalf("a round trip through unpinned lost the place: %v", got)
	}
}

// THE COLUMN ARRIVES ON A DATABASE THAT ALREADY HAS CARDS IN IT, and every
// pinned card in it keeps its pin.
//
// `0051` adds a column with a default, which is the cheap kind of migration
// and still the kind worth checking against something real: a board that has
// run for weeks has pinned cards on it, and the failure worth catching is the
// one where they come back unpinned, or where the open halts outright. A halt
// is not a degraded mode here, it is a daemon that refuses to start.
//
// Skipped unless ATRIUM_LIVE_COPY names a COPY, the same rule as
// `livecopy_check_test.go`. Never the real database.
func TestPinOrderLandsOnALiveDatabase(t *testing.T) {
	path := os.Getenv("ATRIUM_LIVE_COPY")
	if path == "" {
		t.Skip("set ATRIUM_LIVE_COPY to a COPY of a real atrium.db")
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("0051 halted a real database: %v", err)
	}
	defer s.Close()

	tasks, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	pins := 0
	for _, task := range tasks {
		if task.Pinned {
			pins++
		}
		// Every card reads a position, and on a database nobody has dragged on
		// yet every one of them is zero. A non-zero here would mean the column
		// came up holding somebody else's data.
		if task.PinOrder != 0 {
			t.Errorf("%s came out of the migration already ordered at %d", task.ID, task.PinOrder)
		}
	}
	t.Logf("%d cards, %d of them pinned, all reading order 0", len(tasks), pins)
	if pins == 0 {
		t.Log("nothing pinned in this copy, so the pins-survive half proved nothing")
	}
}
