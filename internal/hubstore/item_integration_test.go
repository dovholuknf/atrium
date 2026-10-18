//go:build integration

package hubstore

import "testing"

func anItem(rev int, state string) Item {
	return Item{ID: "it-1", Room: "sg4", Card: "c1", Generation: 1, State: state, Revision: rev,
		OwnerKind: OwnerSession, Owner: "orch@sg4", CreatedBy: "orch@sg4"}
}

// A REPLAYED PUSH IS A NO-OP: the same (item_id, revision, state) changes nothing and says so.
func TestPutItemReplayOfTheSameRevisionAndStateIsANoOp(t *testing.T) {
	s := open(t)
	got, applied, err := s.PutItem(anItem(1, ItemStateRunning))
	if err != nil || !applied || got.Revision != 1 {
		t.Fatalf("first: %+v %v %v", got, applied, err)
	}
	blocked := anItem(2, ItemStateBlocked)
	blocked.Note = "need a key"
	if _, applied, err = s.PutItem(blocked); err != nil || !applied {
		t.Fatalf("blocked: %v %v", applied, err)
	}
	// the same key again, with different words: nothing changes
	again := blocked
	again.Note = "different words"
	got, applied, err = s.PutItem(again)
	if err != nil || applied {
		t.Fatalf("replay applied=%v err=%v", applied, err)
	}
	if got.Note != "need a key" || got.Revision != 2 {
		t.Fatalf("replay changed the row: %+v", got)
	}
	all, _ := s.Items(ItemQuery{})
	if len(all) != 1 {
		t.Fatalf("rows: %+v", all)
	}
}

// A STALE PUSH NEVER MOVES AN ITEM BACK. Pushes can arrive out of order after an outage.
func TestPutItemKeepsTheHighestRevision(t *testing.T) {
	s := open(t)
	s.PutItem(anItem(3, ItemStateFinished))
	got, applied, err := s.PutItem(anItem(2, ItemStateBlocked))
	if err != nil || applied {
		t.Fatalf("stale: applied=%v err=%v", applied, err)
	}
	if got.State != ItemStateFinished || got.Revision != 3 {
		t.Fatalf("stale push regressed the item: %+v", got)
	}
}

func TestPutItemRefusesWhatIsNotAState(t *testing.T) {
	s := open(t)
	if _, _, err := s.PutItem(anItem(1, "delivered")); !IsRefusal(err) {
		t.Fatalf("state: %v", err)
	}
	bad := anItem(0, ItemStateRunning)
	if _, _, err := s.PutItem(bad); !IsRefusal(err) {
		t.Fatalf("revision: %v", err)
	}
	bad = anItem(1, ItemStateRunning)
	bad.ID = ""
	if _, _, err := s.PutItem(bad); !IsRefusal(err) {
		t.Fatalf("id: %v", err)
	}
}

func TestItemsFilterByOwnerAndState(t *testing.T) {
	s := open(t)
	a := anItem(1, ItemStateBlocked)
	b := anItem(1, ItemStateFinished)
	b.ID, b.OwnerKind, b.Owner = "it-2", OwnerHuman, ""
	s.PutItem(a)
	s.PutItem(b)
	if got, _ := s.Items(ItemQuery{OwnerKind: OwnerHuman}); len(got) != 1 || got[0].ID != "it-2" {
		t.Fatalf("human: %+v", got)
	}
	if got, _ := s.Items(ItemQuery{States: []string{ItemStateBlocked}}); len(got) != 1 || got[0].ID != "it-1" {
		t.Fatalf("blocked: %+v", got)
	}
	if _, err := s.ItemByID("it-9"); err != ErrNoItem {
		t.Fatalf("missing: %v", err)
	}
}

// OWNER RESOLUTION: the creator by default, a single-holder role on request, the human, and never a tester.
func TestResolveOwner(t *testing.T) {
	for _, c := range []struct {
		spec, creator, kind, owner string
		refused                    bool
	}{
		{"", "orch@sg4", OwnerSession, "orch@sg4", false},
		{"human", "orch@sg4", OwnerHuman, "", false},
		{"merger", "orch@sg4", OwnerRole, RoleMerger, false},
		{"role:director", "orch@sg4", OwnerRole, RoleDirector, false},
		{"orchestrator", "x", OwnerRole, RoleOrchestrator, false},
		{"tester", "orch@sg4", "", "", true},
		{"role:tester", "orch@sg4", "", "", true},
		{"boss", "orch@sg4", "", "", true},
		{"", "", "", "", true},
	} {
		k, o, err := ResolveOwner(c.spec, c.creator)
		if c.refused {
			if !IsRefusal(err) {
				t.Errorf("%q: want a refusal, got %v", c.spec, err)
			}
			continue
		}
		if err != nil || k != c.kind || o != c.owner {
			t.Errorf("%q: %q %q %v", c.spec, k, o, err)
		}
	}
}

// A ROLE IS RESOLVED TO ITS CURRENT HOLDER AT DELIVERY, not at creation.
func TestItemHolderResolvesARoleAtTheTime(t *testing.T) {
	s := open(t)
	it := anItem(1, ItemStateBlocked)
	it.OwnerKind, it.Owner = OwnerRole, RoleMerger
	if _, _, ok, _ := s.ItemHolder(it); ok {
		t.Fatal("nobody holds merger yet")
	}
	s.RoleSet(RoleMerger, "sg4", "c7", "clint")
	room, card, ok, err := s.ItemHolder(it)
	if err != nil || !ok || room != "sg4" || card != "c7" {
		t.Fatalf("holder: %q %q %v %v", room, card, ok, err)
	}
	s.RoleSet(RoleMerger, "sg3", "c8", "clint")
	if room, card, _, _ = s.ItemHolder(it); room != "sg3" || card != "c8" {
		t.Fatalf("the holder changed: %q %q", room, card)
	}
}

// THE MIGRATION IS A NEW ONE AT THE END, and reopening a store with it recorded changes nothing.
func TestItemMigrationIsLastAndIdempotent(t *testing.T) {
	last := migrations[len(migrations)-10]
	if last.name != "0022_work_backfill" || migrations[len(migrations)-13].name != "0019_room_ssh" {
		t.Fatalf("the item migration is not where it was: %q", last.name)
	}
	s := open(t)
	s.PutItem(anItem(1, ItemStateRunning))
	if err := s.migrate(); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Items(ItemQuery{}); len(got) != 1 {
		t.Fatalf("rows after a second migrate: %+v", got)
	}
}
