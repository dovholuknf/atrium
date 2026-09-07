package daemon

import (
	"errors"
	"strings"
	"testing"
)

// The way an account at a limit gets told the wrong thing.
//
// zrok v2 does not answer a limit with 429, which is what this code was
// written against. It answers 401 with no body for a share or an environment,
// and 409 carrying `names limit reached` for a name. Both of those statuses
// already meant something else here, so both of them lied. Each test below
// names the sentence somebody read and why it sent them nowhere.
//
// The error strings are the ones the generated client produces. It formats the
// operation, the status and the JSON payload into one line, so the payload is
// what there is to match on. See `rest_client_zrok/share/
// create_share_name_responses.go`, `CreateShareNameConflict.Error`.

const nameLimitErr = `[POST /share/name][409] createShareNameConflict ` +
	`"names limit reached; cannot reserve additional names"`

const nameTakenErr = `[POST /share/name][409] createShareNameConflict ""`

const shareRefusedErr = `[POST /share][401] shareUnauthorized`

// An account at its reserved name ceiling must not be told to pick a longer
// name. There is no longer name that works, and somebody following that
// instruction tries forever.
func TestNameLimitIsNotReportedAsATakenName(t *testing.T) {
	got := zrokSays("could not create the name", errors.New(nameLimitErr)).Error()

	if strings.Contains(got, "that name is taken") || strings.Contains(got, "longer one") {
		t.Fatalf("a name limit was reported as a name collision, so the instruction is to "+
			"try a longer name, which cannot work:\n%s", got)
	}
	if !strings.Contains(got, "release") {
		t.Errorf("nothing in the message says to release a name, which is the only thing "+
			"that helps:\n%s", got)
	}
}

// A limit that does not say which limit must not be given one. Only the words
// `limit reached` name a ceiling, and a bare 429 naming the reserved names
// would send somebody to release names over something else entirely.
func TestAnUnnamedLimitDoesNotClaimToBeTheNameLimit(t *testing.T) {
	got := zrokSays("could not open the share",
		errors.New(`[POST /share][429] shareTooManyRequests`)).Error()

	if strings.Contains(got, "reserved names") || strings.Contains(got, "release one") {
		t.Fatalf("a limit with no name was reported as the reserved-name ceiling:\n%s", got)
	}
	if !strings.Contains(got, "limit") {
		t.Errorf("a 429 no longer reads as a limit at all:\n%s", got)
	}
}

// The plain collision still has to read as one. A name really taken by
// somebody else IS fixed by choosing another, and folding both cases into one
// message would lose that.
func TestATakenNameStillReadsAsATakenName(t *testing.T) {
	got := zrokSays("could not create the name", errors.New(nameTakenErr)).Error()

	if !strings.Contains(got, "taken") {
		t.Fatalf("a name collision no longer says the name is taken:\n%s", got)
	}
}

// The refusal that cannot be told apart from a revoked token has to name both.
// zrok returns exactly the same 401 with no body for a share limit and for a
// dead token, so a message that picks one is wrong half the time, and the half
// it is wrong about sends somebody to re-enable an environment that is fine.
func TestAShareRefusalNamesBothCauses(t *testing.T) {
	got := strings.ToLower(zrokSays("could not open the share", errors.New(shareRefusedErr)).Error())

	if !strings.Contains(got, "limit") {
		t.Errorf("a 401 from zrok does not mention a limit, and a share limit is one of the "+
			"two things that produces it:\n%s", got)
	}
	if !strings.Contains(got, "revoked") {
		t.Errorf("a 401 from zrok no longer mentions the token, which is the other:\n%s", got)
	}
}

// Reserving is two calls and the first one swallows conflicts on purpose,
// because a name that already exists is the ordinary case on a second press.
// A name limit arrives as the same status and must NOT be swallowed: carried
// into the second call it comes back as "the name exists but could not be
// reserved", which describes a state the account is not in.
func TestReservingDoesNotSwallowANameLimit(t *testing.T) {
	if alreadyThere(errors.New(nameLimitErr)) {
		t.Fatal("a name-limit refusal is being read as a name that already exists, so " +
			"reserving carries on to the update, which refuses for the same reason and " +
			"reports it as a name collision")
	}
	if !alreadyThere(errors.New(nameTakenErr)) {
		t.Fatal("a real conflict is no longer recognised, so pressing reserve twice now " +
			"fails the second time")
	}
}

// Every name atrium invents carries the prefix the account panel counts, so
// "how many of these reservations are mine" has an answer. Losing the prefix
// silently zeroes that number rather than breaking anything.
func TestAtriumNamesAreAttributable(t *testing.T) {
	n, err := newShareNameOf(boardShareNameLen)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(n, atriumNamePrefix) {
		t.Fatalf("%q does not start with %q, so the zrok panel cannot say which reserved "+
			"names are atrium's", n, atriumNamePrefix)
	}
}
