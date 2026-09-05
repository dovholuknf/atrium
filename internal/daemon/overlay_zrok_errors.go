package daemon

import (
	"fmt"
	"strings"
)

// Turning what zrok said into what somebody can act on.
//
// THE LIMITS ARE THE COMMON FAILURE and they are the least readable. A zrok
// account has a cap on environments, on shares, on how much traffic it will
// carry, and the free tier's caps are low enough to hit on an ordinary
// evening. What comes back through the generated client is a typed error whose
// message is often the HTTP status and nothing else, so the board showed
// `unexpected response 429` and left somebody to guess.
//
// Every one of these is a sentence about what to do next. The original is kept
// on the end, always: a guess that reads well and points the wrong way is worse
// than a status code, and this is a guess. It matches on text because the
// generated client returns a distinct type per status per operation, so a type
// switch would name a dozen and still miss whichever one the next zrok adds.

// zrokSays wraps an error from any zrok call with an explanation when it can
// recognise one, and returns it unchanged when it cannot.
func zrokSays(what string, err error) error {
	if err == nil {
		return nil
	}
	s := strings.ToLower(err.Error())

	switch {
	// The account's ceiling. zrok returns 429 for every kind of limit, so the
	// message has to cover all of them rather than name one.
	case has(s, "429", "too many requests", "limit", "quota", "exceeded"):
		return fmt.Errorf("%s: your zrok account is at a limit. that is one of: too many "+
			"shares open at once, too many environments enabled, or the account's "+
			"transfer allowance for this period. stop a share you are not using, or "+
			"check the account at your zrok instance. (%w)", what, err)

	// The token is wrong, or the environment was disabled on the other end.
	case has(s, "401", "403", "unauthorized", "forbidden", "invalid token"):
		return fmt.Errorf("%s: zrok did not accept this environment. the account token may "+
			"have been revoked, or this environment disabled from the console. enabling "+
			"this machine again under settings, expose the board, is the fix. (%w)", what, err)

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
