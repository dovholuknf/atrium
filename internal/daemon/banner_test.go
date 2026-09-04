package daemon

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// Who a message is from, said in the envelope.
//
// This matters more than it looks and it is why the column exists before the
// peer bus does. A model that reads another session's request as an
// instruction from the operator acts on it with an authority that session does
// not have, and the only thing standing between those two readings is this
// sentence.

func TestAMessageFromYouReadsAsFromYou(t *testing.T) {
	got := messageBanner([]*store.Message{{Text: "run the tests"}}, false)
	if !strings.Contains(got, "from the human") {
		t.Fatalf("the banner does not say who: %q", got)
	}
	if strings.Contains(got, "another session") {
		t.Fatal("your own message was attributed to a session")
	}
}

func TestAMessageFromAPeerSaysWhichAndSaysItWasNotYou(t *testing.T) {
	got := messageBanner([]*store.Message{
		{Text: "have you got the lock", FromPeer: "sg4/ziti-acme"},
	}, false)

	if !strings.Contains(got, "sg4/ziti-acme") {
		t.Fatalf("the banner does not say which session: %q", got)
	}
	// The load-bearing half. Without it a peer's request reads as yours.
	if !strings.Contains(got, "not as something the human typed") {
		t.Fatalf("the banner does not disclaim the human: %q", got)
	}
}

// A mixed batch is described as mixed rather than picking one, since claiming
// it is all from you would be wrong about part of it.
func TestAMixedBatchIsDescribedAsMixed(t *testing.T) {
	got := messageBanner([]*store.Message{
		{Text: "mine"},
		{Text: "theirs", FromPeer: "sg4/other"},
	}, false)

	if !strings.Contains(got, "some from the human") {
		t.Fatalf("a mixed batch claimed one author: %q", got)
	}
	// And each is labeled, or "some of these are from a peer" is unusable.
	if !strings.Contains(got, "From sg4/other:") {
		t.Fatalf("a mixed batch does not say which is which: %q", got)
	}
}

// Two different peers in one batch is also mixed, even with no message of
// yours in it.
func TestTwoPeersInOneBatchIsMixed(t *testing.T) {
	got := messageBanner([]*store.Message{
		{Text: "a", FromPeer: "sg4/one"},
		{Text: "b", FromPeer: "sg4/two"},
	}, false)
	if !strings.Contains(got, "other sessions") {
		t.Fatalf("two peers were reported as one: %q", got)
	}
}

// The blocked wording still explains why a tool call was interrupted, whoever
// sent the message, or the model reads a refusal on its merits.
func TestTheBlockedExplanationSurvivesAPeerMessage(t *testing.T) {
	got := messageBanner([]*store.Message{
		{Text: "x", FromPeer: "sg4/other"},
	}, true)
	if !strings.Contains(got, "not refused on its merits") {
		t.Fatalf("a blocked peer message reads as a policy refusal: %q", got)
	}
}

// A peer message has to say which session sent it. A single queue function
// with an optional sender is one defaulted argument away from a peer message
// that claims to be from you.
func TestAPeerMessageMustNameItsSender(t *testing.T) {
	d := testDaemon(t)
	task := noteCard(t, d, "peer-envelope")

	if _, err := d.st.QueueFromPeer(task.ID, "hello", ""); err == nil {
		t.Fatal("a peer message with no sender was accepted")
	}
	if _, err := d.st.QueueFromPeer(task.ID, "hello", "   "); err == nil {
		t.Fatal("a whitespace sender was accepted")
	}

	m, err := d.st.QueueFromPeer(task.ID, "hello", "sg4/other")
	if err != nil {
		t.Fatal(err)
	}
	if m.FromHuman() {
		t.Fatal("a peer message reports itself as from the human")
	}

	// And it survives the round trip, which is what the banner reads.
	pending, err := d.st.PendingMessages(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].FromPeer != "sg4/other" {
		t.Fatalf("the sender was lost: %+v", pending)
	}
}

// Everything written before the column existed is from the operator, which is
// what an empty value has to mean.
func TestAnEmptySenderIsTheOperator(t *testing.T) {
	d := testDaemon(t)
	task := noteCard(t, d, "peer-default")

	if _, err := d.st.QueueMessage(task.ID, "ordinary"); err != nil {
		t.Fatal(err)
	}
	pending, err := d.st.PendingMessages(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || !pending[0].FromHuman() {
		t.Fatalf("an ordinary message is not attributed to the human: %+v", pending)
	}
}
