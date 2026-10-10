//go:build integration

package daemon

import "testing"

func TestAShareNameIsParsedTheWayZrokSpellsIt(t *testing.T) {
	cases := []struct {
		in   string
		ns   string
		name string
	}{
		// Zrok's own spelling.
		{"public:atrium-abc", "public", "atrium-abc"},
		// What atrium wrote into this box for months. It has to keep working
		// or the fix reads as the share name being lost.
		{"public/atrium-abc", "public", "atrium-abc"},
		// What anybody types. This is the one that also came out empty, since
		// a bare token parses as a NAMESPACE with no name.
		{"atrium-abc", "public", "atrium-abc"},
		{"  atrium-abc  ", "public", "atrium-abc"},
		// A namespace that is not the default is still honoured.
		{"mine:atrium-abc", "mine", "atrium-abc"},
	}
	for _, c := range cases {
		sel, err := parseShareName(c.in)
		if err != nil {
			t.Fatalf("%q was refused: %v", c.in, err)
		}
		if sel.NamespaceToken != c.ns || sel.Name != c.name {
			t.Fatalf("%q gave namespace %q name %q, wanted %q and %q",
				c.in, sel.NamespaceToken, sel.Name, c.ns, c.name)
		}
	}
}

// A NAME THAT IS NOT A NAME IS REFUSED HERE, where the message can say what
// was wrong with it, rather than three calls later as "a reservation needs a
// name" with nothing pointing at the box it came from.
func TestAShareNameWithNoShareInItIsRefused(t *testing.T) {
	for _, in := range []string{"", "   ", "public:", "public/", ":", "/"} {
		if sel, err := parseShareName(in); err == nil {
			t.Fatalf("%q was accepted as namespace %q name %q", in, sel.NamespaceToken, sel.Name)
		}
	}
}
