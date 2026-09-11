package daemon

import "testing"

// What goes in the share name box, and what zrok is handed.
//
// THE SEPARATOR IS A COLON. `zroksdk.ParseNameSelection` documents its input as
// `<namespaceToken>[:<name>]`, and atrium wrote and split a SLASH, so the whole
// of `public/atrium-4pcddxxx9aez` went into the namespace and the name came
// back empty. `ReserveZrokName` then refused with "a reservation needs a name",
// and every public share failed on a configuration that looked right in the
// box.

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
