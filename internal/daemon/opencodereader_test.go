package daemon

import "testing"

func TestOpencodeSessionInPicksTheNewestInTheCardsFolder(t *testing.T) {
	t.Parallel()
	out := []byte("noise\n" + `[
 {"id":"ses_old","updated":1,"directory":"V:\\cards\\a"},
 {"id":"ses_new","updated":9,"directory":"v:\\cards\\a"},
 {"id":"ses_other","updated":99,"directory":"V:\\cards\\b"}]`)
	if got := opencodeSessionIn(out, "V:/cards/a/"); got != "ses_new" {
		t.Fatalf("got %q, want ses_new", got)
	}
	if got := opencodeSessionIn(out, "V:/cards/none"); got != "" {
		t.Fatalf("a folder opencode never ran in got %q", got)
	}
}
