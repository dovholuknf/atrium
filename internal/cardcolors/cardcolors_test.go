package cardcolors

import "testing"

func TestValid(t *testing.T) {
	ok := Colors{Default: "nord", Repos: map[string]string{"github/openziti/ziti": "teal-dusk"}}
	if err := Valid(ok); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"ziti", "github/OpenZiti/ziti", "github/openziti", "a/b/c/d"} {
		if Valid(Colors{Repos: map[string]string{k: "x"}}) == nil {
			t.Errorf("%q accepted", k)
		}
	}
	if Valid(Colors{Repos: map[string]string{"github/a/b": ""}}) == nil {
		t.Error("empty name accepted")
	}
}

func TestSeededValid(t *testing.T) {
	if len(seeded) != 17 {
		t.Fatalf("seed has %d entries", len(seeded))
	}
	if err := Valid(Colors{Repos: seeded}); err != nil {
		t.Fatal(err)
	}
}
