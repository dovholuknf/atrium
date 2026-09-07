package daemon

import (
	"fmt"
	"strings"
)

// Turning what zrok said into what somebody can act on.
//
// THE LIMITS ARE THE COMMON FAILURE and they are the least readable. A zrok
// account has a cap on environments, on shares, on reserved names and on how
// much traffic it will carry, and the free tier's caps are low enough to hit on
// an ordinary evening. What comes back through the generated client is a typed
// error whose message is often the HTTP status and nothing else, so the board
// showed a bare status code and left somebody to guess.
//
// Every one of these is a sentence about what to do next. The original is kept
// on the end, always: a guess that reads well and points the wrong way is worse
// than a status code, and this is a guess. It matches on text because the
// generated client returns a distinct type per status per operation, so a type
// switch would name a dozen and still miss whichever one the next zrok adds.
//
// ZROK V2 DOES NOT RETURN 429 FOR A LIMIT. This file used to say it returned
// 429 for all of them, which is what every branch below was written against,
// and it is wrong. Read from the v2.0.4 controller, a refusal for a limit comes
// back as one of three things:
//
//   - `POST /share` answers 401 with no body. `controller/share.go` calls
//     `checkLimits` and returns `NewShareUnauthorized()`, the SAME response a
//     revoked token gets, so the two are not distinguishable from out here.
//   - `POST /enable` answers 401 with no body, `controller/enable.go`, same
//     shape.
//   - `POST /share/name` and its update answer 409 carrying `names limit
//     reached; cannot reserve additional names`.
//
// The last one is why the limit case is tested BEFORE the conflict case below,
// and why `alreadyThere` in `overlay_reserve.go` refuses to treat it as a name
// that was already there. A 409 is otherwise exactly what reserving an existing
// name looks like, so an account at its name ceiling was told its name was
// taken and to try a longer one, which it would have done forever.
//
// The two 401s cannot be told apart, so that branch names both causes rather
// than picking one. The zrok panel's account block is the thing that answers
// which, because it counts what the account is already holding.

// zrokSays wraps an error from any zrok call with an explanation when it can
// recognise one, and returns it unchanged when it cannot.
func zrokSays(what string, err error) error {
	if err == nil {
		return nil
	}
	s := strings.ToLower(err.Error())

	switch {
	// A NAME CEILING, said in words by the controller. First, because it
	// arrives as a 409 and the conflict branch below would otherwise tell
	// somebody to pick a different name, which cannot help.
	//
	// Matched on the words and NOT on a status. `limit reached` is the only
	// thing zrok says that names which ceiling was hit, so it is the only thing
	// that earns a message naming one.
	case has(s, "limit reached"):
		return fmt.Errorf("%s: your zrok account is holding as many reserved names as it is "+
			"allowed. release one you are not using. the zrok panel under settings, expose "+
			"the board, counts them and says how many are atrium's. (%w)", what, err)

	// A limit with nothing saying which. zrok v2 does not send these, so this
	// is here for an instance that does and must not claim to know more than
	// the status does.
	case has(s, "429", "too many requests"):
		return fmt.Errorf("%s: the zrok instance says this account is at a limit and does not "+
			"say which. the zrok panel under settings, expose the board, counts what the "+
			"account is holding. (%w)", what, err)

	// The token is wrong, the environment was disabled on the other end, OR
	// the account is at a share or environment ceiling. All three are 401 with
	// no body, so all three are named. See the header.
	case has(s, "401", "403", "unauthorized", "forbidden", "invalid token"):
		return fmt.Errorf("%s: zrok refused this environment, and it answers the same way for "+
			"two different reasons. either the account is at a limit, on shares open at once, "+
			"on environments enabled, or on the transfer allowance for this period, or the "+
			"account token was revoked and this environment needs enabling again. the zrok "+
			"panel under settings, expose the board, counts what the account is using, which "+
			"is what tells the two apart. (%w)", what, err)

	// The name is taken. Only reachable on a reserved share.
	case has(s, "409", "conflict", "already exists", "not available"):
		return fmt.Errorf("%s: that name is taken. names are unique across the whole zrok "+
			"instance, not just your account, so a short one is usually gone. try a "+
			"longer one. (%w)", what, err)

	// Nothing answered. The instance, the network, or a proxy in between.
	case has(s, "connection refused", "no such host", "timeout", "timed out",
		"i/o timeout", "eof", "dial tcp"):
		return fmt.Errorf("%s: could not reach the zrok instance. check that you are online "+
			"and that the api endpoint under settings is the instance you enabled "+
			"against. (%w)", what, err)

	case has(s, "500", "502", "503", "504", "internal server error", "bad gateway"):
		return fmt.Errorf("%s: the zrok instance answered with an error of its own. nothing "+
			"here is misconfigured, so this is worth trying again in a minute. (%w)",
			what, err)
	}
	return fmt.Errorf("%s: %w", what, err)
}

func has(s string, any ...string) bool {
	for _, a := range any {
		if strings.Contains(s, a) {
			return true
		}
	}
	return false
}
