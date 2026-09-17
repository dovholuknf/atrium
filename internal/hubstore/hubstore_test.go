package hubstore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// What the hub's store has to be right about.
//
// The tests are named for the ways this gets broken rather than for the
// behaviour, because every one of them is a mistake that would compile, pass
// the rest of the suite, and be found by somebody who cannot work out why a
// room they made is not on the list.

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatalf("could not open a fresh store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func added(t *testing.T, s *Store, name string) *Room {
	t.Helper()
	r, err := s.Add(name, TransportDirect)
	if err != nil {
		t.Fatalf("could not add %q: %v", name, err)
	}
	return r
}

// A ROOM EXISTS BEFORE IT HAS EVER CONNECTED. That is the entire reason the
// list is durable rather than a picture of what is dialled in: "have I already
// made that room" has to be answerable, and it cannot be answered by looking at
// sockets.
func TestARoomIsOnTheListBeforeItHasEverConnected(t *testing.T) {
	s := open(t)
	r := added(t, s, "sparta")

	if r.EverConnected() {
		t.Fatal("a room that was just added claims to have connected")
	}
	rooms, err := s.Rooms()
	if err != nil {
		t.Fatal(err)
	}
	if len(rooms) != 1 || rooms[0].Name != "sparta" {
		t.Fatalf("the room is not on the list: %+v", rooms)
	}
	if rooms[0].State != StateActive {
		t.Fatalf("a new room is in state %q", rooms[0].State)
	}
}

// ONE NAME, ONE ROOM, and capitals are not a second room.
//
// A name is what routes, and internal/link folds it the same way to decide
// where a request goes. Two rows differing only in case would both be reachable
// and only one would ever be reached.
func TestTwoRoomsCannotShareANameHoweverItIsTyped(t *testing.T) {
	s := open(t)
	added(t, s, "sparta")

	if _, err := s.Add("Sparta", TransportDirect); err == nil {
		t.Fatal("a second room took a name that was already taken")
	} else if !strings.Contains(err.Error(), "already a room") {
		t.Fatalf("the refusal does not say what is wrong: %v", err)
	}

	// AND IT MUST NOT HALT. A name somebody typed twice is the store working,
	// not the store breaking, and halting on it would take the hub down over a
	// typo.
	if halted, cause := s.Halted(); halted {
		t.Fatalf("a duplicate name halted the store: %v", cause)
	}
}

// A name that cannot survive being a room is refused where somebody can read
// the sentence, rather than at enrolment where it is a certificate error.
func TestANameThatCannotBeRoutedIsRefused(t *testing.T) {
	s := open(t)
	for _, bad := range []string{"", "  ", "two words", "room/one", "café", "a~b"} {
		if _, err := s.Add(bad, TransportDirect); err == nil {
			t.Fatalf("%q was accepted as a room name", bad)
		}
	}
	if _, err := s.Add(strings.Repeat("x", 65), TransportDirect); err == nil {
		t.Fatal("a 65 character name was accepted")
	}
}

// A JOIN SECRET AUTHORISES ONE NAME, which is the hole it exists to close.
//
// What came before authorised A join and let the room name itself, so anybody
// holding a string could enrol as any room they liked. Spend has to answer WHO
// the secret was for, not merely whether it was valid.
func TestASecretSaysWhichRoomItIsFor(t *testing.T) {
	s := open(t)
	sparta := added(t, s, "sparta")
	added(t, s, "athens")

	secret, err := s.Mint(sparta.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Spend(secret)
	if err != nil {
		t.Fatalf("a freshly minted join string was refused: %v", err)
	}
	if got.ID != sparta.ID || got.Name != "sparta" {
		t.Fatalf("the join string enrolled %q, not sparta", got.Name)
	}
}

// GOOD ONCE. Pasting it twice failing is how you find out it went somewhere it
// should not have.
func TestAJoinStringWorksExactlyOnce(t *testing.T) {
	s := open(t)
	r := added(t, s, "sparta")
	secret, err := s.Mint(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Spend(secret); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Spend(secret); !errors.Is(err, ErrBadSecret) {
		t.Fatalf("the same join string worked twice: %v", err)
	}
}

// MINTING AGAIN REVOKES WHAT WAS THERE. The string is copy-once, so a lost one
// is replaced rather than revealed, and the replaced one has to stop working or
// "replaced" means "there are now two".
func TestMintingAgainRetiresTheOldString(t *testing.T) {
	s := open(t)
	r := added(t, s, "sparta")
	first, err := s.Mint(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Mint(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("minting twice produced the same string")
	}
	if _, err := s.Spend(first); !errors.Is(err, ErrBadSecret) {
		t.Fatalf("the retired join string still works: %v", err)
	}
	if _, err := s.Spend(second); err != nil {
		t.Fatalf("the current join string was refused: %v", err)
	}
}

// An hour is long enough to walk to another machine. A string left in a chat
// log is not a key the next day.
func TestAJoinStringExpires(t *testing.T) {
	s := open(t)
	r := added(t, s, "sparta")
	secret, err := s.Mint(r.ID)
	if err != nil {
		t.Fatal(err)
	}

	real := now
	defer func() { now = real }()
	now = func() time.Time { return real().Add(secretLife + time.Minute) }

	if ok, _, err := s.Outstanding(r.ID); err != nil || ok {
		t.Fatalf("an expired join string still reads as outstanding (%v, %v)", ok, err)
	}
	if _, err := s.Spend(secret); !errors.Is(err, ErrBadSecret) {
		t.Fatalf("an expired join string was accepted: %v", err)
	}
}

// WHAT A MACHINE REPORTS NEVER OVERWRITES WHAT A HUMAN TYPED.
//
// This is the one place breaking that rule would be easiest, because the room's
// own name arrives on the same frame as everything else about it. The hub's
// name is the name; the room's is observed and sits beside it.
func TestWhatARoomCallsItselfNeverBecomesItsName(t *testing.T) {
	s := open(t)
	r := added(t, s, "sparta")

	if err := s.Seen(r.ID, "DESKTOP-9F2K1", "v2.1"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "sparta" {
		t.Fatalf("the room renamed itself to %q", got.Name)
	}
	if got.SelfName != "DESKTOP-9F2K1" {
		t.Fatalf("what the room calls itself was not kept: %q", got.SelfName)
	}
	if got.Version != "v2.1" {
		t.Fatalf("the room's version was not kept: %q", got.Version)
	}
}

// FIRST SEEN IS SET ONCE. It is what separates a room that is offline from one
// that has never been anywhere, and the second is the one that draws nowhere
// but the rooms tab. Moving it on every attach would make every room look new.
func TestTheFirstTimeARoomConnectedDoesNotMove(t *testing.T) {
	s := open(t)
	r := added(t, s, "sparta")

	real := now
	defer func() { now = real }()
	first := real()
	now = func() time.Time { return first }
	if err := s.Seen(r.ID, "box", "v1"); err != nil {
		t.Fatal(err)
	}

	later := first.Add(3 * time.Hour)
	now = func() time.Time { return later }
	if err := s.Seen(r.ID, "box", "v1"); err != nil {
		t.Fatal(err)
	}

	got, err := s.Get(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.FirstSeen == nil || !got.FirstSeen.Equal(first) {
		t.Fatalf("first seen moved to %v, wanted %v", got.FirstSeen, first)
	}
	if got.LastSeen == nil || !got.LastSeen.Equal(later) {
		t.Fatalf("last seen is %v, wanted %v", got.LastSeen, later)
	}
	if !got.EverConnected() {
		t.Fatal("a room that has connected reads as never connected")
	}
}

// THE HUB'S OWN ROOM IS A ROW LIKE ANY OTHER, and turning it off and on again
// must not make a second one.
func TestTheHubsOwnRoomIsWrittenDownOnce(t *testing.T) {
	s := open(t)

	first, err := s.EnsureLocal("sg4")
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.EnsureLocal("sg4")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != again.ID {
		t.Fatal("turning the hub's own room on twice made two rooms")
	}
	if first.Transport != TransportLocal {
		t.Fatalf("the hub's own room reaches itself over %q", first.Transport)
	}
}

// AND IT MUST NOT ADOPT SOMEBODY ELSE'S ROW.
//
// The hub's own room is named after the machine by default, and a real room
// dialling in from elsewhere could already hold that name. Taking the row over
// would point an in-process room at another machine's identity and break both.
// Found by review.
func TestTheHubsOwnRoomWillNotTakeOverARealOne(t *testing.T) {
	s := open(t)
	added(t, s, "sg4")

	_, err := s.EnsureLocal("sg4")
	if err == nil {
		t.Fatal("the hub's own room took over a room that dials in over the network")
	}
	if !strings.Contains(err.Error(), "--room-name") {
		t.Fatalf("the refusal does not say how to fix it: %v", err)
	}
}

// WHETHER A ROOM IS STILL THERE, AS AN INFERENCE AND NOT A CLAIM.
//
// Nothing outside the hub's process can know whether a socket is open. What the
// store can say is how recently the hub wrote down hearing from it, which is
// enough for a command line to refuse to force out a room that is plainly still
// running and not enough for anything that has to be right.
func TestARoomHeardFromRecentlyReadsAsStillThere(t *testing.T) {
	s := open(t)
	r := added(t, s, "sparta")

	if got, _ := s.Get(r.ID); got.LikelyAttached() {
		t.Fatal("a room that has never connected reads as attached")
	}
	if err := s.Seen(r.ID, "box", "v1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(r.ID); !got.LikelyAttached() {
		t.Fatal("a room heard from a moment ago does not read as attached")
	}

	real := now
	defer func() { now = real }()
	now = func() time.Time { return real().Add(Lively + time.Second) }
	if got, _ := s.Get(r.ID); got.LikelyAttached() {
		t.Fatal("a room nobody has heard from still reads as attached")
	}
}

// MARKING IS REVERSIBLE, which is the whole reason it is safe to press.
func TestMarkingForDeletionCanBeTakenBack(t *testing.T) {
	s := open(t)
	r := added(t, s, "sparta")

	if err := s.Mark(r.ID, true); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(r.ID)
	if !got.Marked() {
		t.Fatal("marking a room did not mark it")
	}
	if err := s.Mark(r.ID, false); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(r.ID)
	if got.Marked() || got.State != StateActive {
		t.Fatalf("unmarking left the room in %q", got.State)
	}
}

// FORCING A ROOM OUT REMOVES THE HUB'S RECORD AND NOTHING ELSE, and the record
// of having done it has to survive, because that is the thing somebody will
// come looking for when the machine turns up again still holding cards.
func TestTheLogOutlivesTheRoomItIsAbout(t *testing.T) {
	s := open(t)
	r := added(t, s, "sparta")
	if _, err := s.Announce(r.ID, []Card{{ID: "c1", Status: "running"}}); err != nil {
		t.Fatal(err)
	}

	if err := s.Force(r.ID, "the laptop was wiped"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(r.ID); !errors.Is(err, ErrNoSuchRoom) {
		t.Fatalf("the room is still there: %v", err)
	}
	if n, err := s.CardCount(r.ID); err != nil || n != 0 {
		t.Fatalf("its cached cards outlived it: %d, %v", n, err)
	}

	log, err := s.Audit(50)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range log {
		if e.Kind == "forced-out" && e.RoomName == "sparta" &&
			strings.Contains(e.Detail, "wiped") {
			found = true
		}
	}
	if !found {
		t.Fatalf("forcing a room out left no record of it: %+v", log)
	}

	// AND IT CAN STILL BE LOOKED UP BY NAME, which is the only handle anybody
	// has left. Asking by id needs a room row, and the room row is the thing
	// that just went: the question "what happened to that laptop" arrives after
	// there is nothing to resolve. Found by review.
	byName, err := s.AuditByName("SPARTA", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(byName) == 0 {
		t.Fatal("a removed room's log cannot be found by the name it had")
	}
	for _, e := range byName {
		if !strings.EqualFold(e.RoomName, "sparta") {
			t.Fatalf("the lookup returned another room's line: %+v", e)
		}
	}
}

// ANYTHING NOT IN THE ANNOUNCEMENT IS DISCARDED. No merging, nothing kept on
// the chance it still exists. A room is the only source of truth, so what it
// sends IS the state.
func TestComingBackReplacesEverythingRatherThanMerging(t *testing.T) {
	s := open(t)
	r := added(t, s, "sparta")

	if _, err := s.Announce(r.ID, []Card{
		{ID: "a", Status: "running", Payload: json.RawMessage(`{"title":"one"}`)},
		{ID: "b", Status: "done", Payload: json.RawMessage(`{"title":"two"}`)},
		{ID: "c", Status: "running", Payload: json.RawMessage(`{"title":"three"}`)},
	}); err != nil {
		t.Fatal(err)
	}

	ch, err := s.Announce(r.ID, []Card{
		{ID: "a", Status: "running", Payload: json.RawMessage(`{"title":"one"}`)},
		{ID: "b", Status: "shelved", Payload: json.RawMessage(`{"title":"two"}`)},
		{ID: "d", Status: "running", Payload: json.RawMessage(`{"title":"four"}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ch.Gone != 1 || ch.New != 1 || ch.Changed != 1 || ch.Same != 1 {
		t.Fatalf("the announcement was counted as %+v, wanted one of each", ch)
	}

	cards, err := s.Cards(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 3 {
		t.Fatalf("the cache holds %d cards, wanted 3", len(cards))
	}
	for _, c := range cards {
		if c.ID == "c" {
			t.Fatal("a card the room stopped mentioning is still cached")
		}
		if c.ID == "b" && c.Status != "shelved" {
			t.Fatalf("card b was not replaced: %q", c.Status)
		}
	}
}

// A ROOM WITH NOTHING ON IT SAYS SO, and that is not the same as a room that
// has not spoken. An empty announcement has to empty the cache, or a room
// somebody cleared shows the old board forever.
func TestARoomThatAnnouncesNothingEmptiesItsCache(t *testing.T) {
	s := open(t)
	r := added(t, s, "sparta")
	if _, err := s.Announce(r.ID, []Card{{ID: "a"}, {ID: "b"}}); err != nil {
		t.Fatal(err)
	}
	ch, err := s.Announce(r.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ch.Gone != 2 {
		t.Fatalf("clearing a room discarded %d cards, wanted 2", ch.Gone)
	}
	if n, _ := s.CardCount(r.ID); n != 0 {
		t.Fatalf("%d cards survived an empty announcement", n)
	}
}

// The discard is not silent. Wholesale replacement is the right rule and it is
// also the one that can quietly lose something a person remembers seeing.
func TestADiscardIsWrittenDown(t *testing.T) {
	s := open(t)
	r := added(t, s, "sparta")
	if _, err := s.Announce(r.ID, []Card{{ID: "a"}, {ID: "b"}, {ID: "c"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Announce(r.ID, []Card{{ID: "a"}}); err != nil {
		t.Fatal(err)
	}

	log, err := s.AuditFor(r.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	var said string
	for _, e := range log {
		if e.Kind == "announced" {
			said = e.Detail
			break
		}
	}
	if !strings.Contains(said, "2 card(s) were discarded") {
		t.Fatalf("the newest announcement reads %q", said)
	}
}

// AND AN ANNOUNCEMENT THAT LOST NOTHING IS NOT WRITTEN DOWN.
//
// A room announces on every change, which on a busy machine is every couple of
// seconds. A line each time saying nothing was lost is a log nobody can read,
// and a log nobody can read answers nothing when somebody comes looking for the
// card they are sure was there.
func TestAnOrdinaryUpdateDoesNotFillTheLog(t *testing.T) {
	s := open(t)
	r := added(t, s, "sparta")

	if _, err := s.Announce(r.ID, []Card{{ID: "a", Status: "running"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Announce(r.ID, []Card{
		{ID: "a", Status: "done"}, {ID: "b", Status: "running"},
	}); err != nil {
		t.Fatal(err)
	}

	log, err := s.AuditFor(r.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range log {
		if e.Kind == "announced" {
			t.Fatalf("an announcement that discarded nothing was written down: %s", e.Detail)
		}
	}
}

// ONE ROOM'S CACHE IS NOT ANOTHER'S. Two rooms can mint the same card id, which
// is the whole reason internal/link tags them, and a cache keyed on the card
// alone would have one room's announcement wipe the other's board.
func TestOneRoomsAnnouncementLeavesAnothersAlone(t *testing.T) {
	s := open(t)
	sparta := added(t, s, "sparta")
	athens := added(t, s, "athens")

	if _, err := s.Announce(sparta.ID, []Card{{ID: "same-id", Status: "running"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Announce(athens.ID, []Card{{ID: "same-id", Status: "done"}}); err != nil {
		t.Fatal(err)
	}

	got, err := s.Cards(sparta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Status != "running" {
		t.Fatalf("athens overwrote sparta's card: %+v", got)
	}
}

// IT DOES NOT START ON A DATABASE IT CANNOT READ.
//
// `sql.Open` is lazy and the pragmas pass on a file full of rubbish, so without
// the check this would come up, serve a board, tell a room it may attach, and
// only fall over at the first query. Refusing here is the one moment the answer
// can still be "do not start".
func TestItRefusesToStartOnADamagedDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	if err := os.WriteFile(path, []byte("this is not a database, it is a note"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err == nil {
		s.Close()
		t.Fatal("the hub opened a store that is not a database")
	}
}

// AND IT HALTS RATHER THAN CARRYING ON.
//
// A hub that keeps serving while it cannot remember which rooms exist will mint
// a second room under a name it has forgotten, or fail to refuse a deletion it
// has no record of. That the work is safe on the rooms is not a reason to stay
// up, it is the reason the halt costs little.
func TestAStoreThatBreaksUnderneathUsHalts(t *testing.T) {
	s := open(t)
	added(t, s, "sparta")

	var told error
	s.OnHalt = func(cause error) { told = cause }

	// The database going away under a running hub: a disk unmounted, a file
	// deleted. Closing it is the reachable version of the same thing.
	s.db.Close()

	if _, err := s.Add("athens", TransportDirect); err == nil {
		t.Fatal("the store accepted work after its database had gone")
	} else if !errors.Is(err, ErrHalted) {
		t.Fatalf("the failure did not come back as a halt: %v", err)
	}
	if halted, _ := s.Halted(); !halted {
		t.Fatal("the store did not halt")
	}
	if told == nil {
		t.Fatal("nothing was told the store had halted")
	}
	// EVERY CALL, not only the one that noticed.
	if _, err := s.Rooms(); !errors.Is(err, ErrHalted) {
		t.Fatalf("reading still works on a halted store: %v", err)
	}
}

// Opening a path that was never a hub is not an error, and the caller has to be
// able to tell. A hub pointed at the wrong directory looks exactly like every
// room having vanished.
func TestAFreshStoreSaysSo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hub.db")

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Fresh() {
		t.Fatal("a store that was just created does not read as fresh")
	}
	s.Close()

	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if again.Fresh() {
		t.Fatal("reopening the same store reads as fresh")
	}
}

// A MACHINE THAT HAS NEVER RUN A HUB HAS NO DIRECTORY TO PUT THIS IN.
//
// The store is the first thing the hub opens, before the certificate authority
// is set up, so on a clean machine nothing has made the state directory yet.
// SQLite will not make one, so without this the very first `atrium2 hub` dies
// at startup over a file it was about to create anyway. Found by review, and it
// is the one failure that only ever happens to somebody's first run.
func TestItMakesItsOwnDirectoryOnAMachineThatHasNeverRunAHub(t *testing.T) {
	path := filepath.Join(t.TempDir(), "never", "been", "here", "hub.db")

	s, err := Open(path)
	if err != nil {
		t.Fatalf("a hub could not start on a clean machine: %v", err)
	}
	defer s.Close()
	if !s.Fresh() {
		t.Fatal("a store made from nothing does not read as fresh")
	}
	if _, err := s.Add("sparta", TransportDirect); err != nil {
		t.Fatalf("the store it made does not work: %v", err)
	}
}

// A room that was never added cannot be looked up, and that is an answer rather
// than a failure. Halting on it would mean an unknown room name takes the hub
// down, and an unknown room name is exactly what an attacker sends.
func TestAnUnknownRoomIsAnAnswerNotAFailure(t *testing.T) {
	s := open(t)
	if _, err := s.ByName("nowhere"); !errors.Is(err, ErrNoSuchRoom) {
		t.Fatalf("looking up an unknown room answered %v", err)
	}
	if halted, cause := s.Halted(); halted {
		t.Fatalf("an unknown room halted the store: %v", cause)
	}
}
