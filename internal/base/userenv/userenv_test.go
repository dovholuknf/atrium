package userenv

import (
	"os"
	"strings"
	"testing"
)

func TestRefreshKeepsBaseWhenThereIsNothingToRead(t *testing.T) {
	base := []string{"A=1", "PATH=/x"}
	if got := refresh(base, nil); strings.Join(got, "|") != "A=1|PATH=/x" {
		t.Fatalf("got %v", got)
	}
}

func TestRefreshPutsTheUsersPathFirstAndKeepsTheProcesssOwn(t *testing.T) {
	sep := string(os.PathListSeparator)
	base := []string{"Path=/launcher" + sep + "/sys", "KEEP=mine", "B=1"}
	got := refresh(base, map[string]string{"PATH": "/sys" + sep + "/new", "KEEP": "theirs", "DOTAGENTS": "/d"})
	want := map[string]bool{"Path=/sys" + sep + "/new" + sep + "/launcher": true, "KEEP=mine": true, "B=1": true, "DOTAGENTS=/d": true}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for _, kv := range got {
		if !want[kv] {
			t.Fatalf("unexpected %q in %v", kv, got)
		}
	}
}
