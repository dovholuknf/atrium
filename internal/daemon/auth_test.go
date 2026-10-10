package daemon

import (
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
	c := AuthConfig{Allow: []string{"Someone@Example.com", "sub-123"}}
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
		if got := c.allows(tc.sub, tc.mail); got != tc.want {
			t.Fatalf("allows(%q,%q) = %v", tc.sub, tc.mail, got)
		}
	}
}

// A state is single use, or a callback can be replayed.
func TestALoginStateCannotBeReplayed(t *testing.T) {
	s, _, err := startLogin("/", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := takeLogin(s); !ok {
		t.Fatal("a fresh state was not accepted")
	}
	if _, ok := takeLogin(s); ok {
		t.Fatal("a state was accepted twice, so a callback can be replayed")
	}
	if _, ok := takeLogin("never issued"); ok {
		t.Fatal("a state nobody issued was accepted")
	}
}

// Every login carries its own verifier, or one stolen code opens every session
// that board ever starts.
func TestEveryLoginGetsItsOwnVerifier(t *testing.T) {
	a, first, err := startLogin("/", false)
	if err != nil {
		t.Fatal(err)
	}
	b, second, err := startLogin("/", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { takeLogin(a); takeLogin(b) })
	if first.verifier == "" || first.verifier == second.verifier {
		t.Fatalf("two logins shared a verifier (%q)", first.verifier)
	}
	if a == b {
		t.Fatal("two logins shared a state")
	}
	// The challenge is a hash and not the verifier itself. `plain` would send
	// the secret in the same redirect an attacker would be watching anyway.
	if pkceChallenge(first.verifier) == first.verifier {
		t.Fatal("the challenge is the verifier, so pkce buys nothing")
	}
}
