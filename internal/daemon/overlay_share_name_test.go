package daemon

import (
	"strings"
	"testing"
)

// The address is the whole credential, so its shape is not cosmetic.
//
// There is no login on a lent session: anyone holding the link drives the
// terminal. That makes the generated name the only thing standing between a
// stranger and somebody's shell, and every property asserted here is load
// bearing rather than tidy.

func TestShareNameIsUnguessable(t *testing.T) {
	// Sixty bits, spelled as twelve symbols from an alphabet of thirty two.
	// A shorter name or a smaller alphabet is a name somebody can walk.
	if got := len(shareNameAlphabet); got != 32 {
		t.Fatalf("the alphabet is %d symbols, not 32. the comment on shareNameLen "+
			"computes sixty bits from thirty two, so one of the two moved", got)
	}
	if shareNameLen != 12 {
		t.Fatalf("shareNameLen is %d, not 12", shareNameLen)
	}

	n, err := newShareName()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(n, "atrium-") {
		t.Fatalf("%q has no atrium- prefix. it is what makes a leftover share "+
			"identifiable on an account holding shares from other tools", n)
	}
	body := strings.TrimPrefix(n, "atrium-")
	if len(body) != shareNameLen {
		t.Fatalf("%q has %d random characters, want %d", n, len(body), shareNameLen)
	}

	// No character anybody can transcribe wrongly. A name gets read aloud and
	// typed by hand, and `l` against `1` turns an unguessable link into a
	// support question.
	for _, bad := range []string{"l", "o", "0", "1"} {
		if strings.Contains(shareNameAlphabet, bad) {
			t.Errorf("the alphabet contains %q, which is confusable when read aloud", bad)
		}
	}
	for _, c := range body {
		if !strings.ContainsRune(shareNameAlphabet, c) {
			t.Errorf("%q contains %q, which is not in the alphabet", n, c)
		}
	}
}

// A generated name must never repeat. Two cards sharing one address would mean
// the link you sent one person reaches the other person's terminal.
func TestShareNamesDoNotRepeat(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 500; i++ {
		n, err := newShareName()
		if err != nil {
			t.Fatal(err)
		}
		if seen[n] {
			t.Fatalf("%q came up twice in 500. the generator is not random", n)
		}
		seen[n] = true
	}
}
