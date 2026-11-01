package daemon

import (
	"github.com/dovholuknf/atrium/internal/daemon/publish"
	"testing"
)

// A login in front of the PUBLISHED board only.
//
// The tests that matter here are the ones about where it does not apply.
// Getting that wrong breaks every hook on the machine, and it breaks them
// quietly, because a hook that fails is designed never to fail a session.

// Who may in is matched on either the subject or the email, because a provider
// may send either and an operator adding their own address should not have to
// know which.
func TestEitherTheSubjectOrTheEmailLetsSomebodyIn(t *testing.T) {
	t.Parallel()
	c := publish.AuthConfig{Allow: []string{"Someone@Example.com", "sub-123"}}
	for _, tc := range []struct {
		sub, mail string
		want      bool
	}{
		{"whoever", "someone@example.com", true},
		{"sub-123", "", true},
		{"SUB-123", "", true},
		{"nobody", "else@example.com", false},
		{"", "", false},
	} {
		if got := c.Allows(tc.sub, tc.mail); got != tc.want {
			t.Fatalf("allows(%q,%q) = %v", tc.sub, tc.mail, got)
		}
	}
}

// A state is single use, or a callback can be replayed.
func TestALoginStateCannotBeReplayed(t *testing.T) {
	t.Parallel()
	var logins publish.LoginStates
	s, _, err := logins.StartLogin("/", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := logins.TakeLogin(s); !ok {
		t.Fatal("a fresh state was not accepted")
	}
	if _, ok := logins.TakeLogin(s); ok {
		t.Fatal("a state was accepted twice, so a callback can be replayed")
	}
	if _, ok := logins.TakeLogin("never issued"); ok {
		t.Fatal("a state nobody issued was accepted")
	}
}

// Every login carries its own verifier, or one stolen code opens every session
// that board ever starts.
func TestEveryLoginGetsItsOwnVerifier(t *testing.T) {
	t.Parallel()
	var logins publish.LoginStates
	a, first, err := logins.StartLogin("/", false)
	if err != nil {
		t.Fatal(err)
	}
	b, second, err := logins.StartLogin("/", false)
	if err != nil {
		t.Fatal(err)
	}
	if first.Verifier == "" || first.Verifier == second.Verifier {
		t.Fatalf("two logins shared a verifier (%q)", first.Verifier)
	}
	if a == b {
		t.Fatal("two logins shared a state")
	}
	// The challenge is a hash and not the verifier itself. `plain` would send
	// the secret in the same redirect an attacker would be watching anyway.
	if publish.PkceChallenge(first.Verifier) == first.Verifier {
		t.Fatal("the challenge is the verifier, so pkce buys nothing")
	}
}
