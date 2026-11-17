package capability

import (
	"os"
	"testing"
)

func TestDefaultListParses(t *testing.T) {
	l := Default()
	if len(l.Caps) < 20 || l.Version < 1 {
		t.Fatalf("default list is %d caps at version %d", len(l.Caps), l.Version)
	}
}

func TestParseRefusesBadLists(t *testing.T) {
	for name, y := range map[string]string{
		"dupe":    "caps: [{id: a, kind: reach, probe: [x]}, {id: a, kind: reach, probe: [x]}]",
		"kind":    "caps: [{id: a, kind: weird, probe: [x]}]",
		"need":    "caps: [{id: a, kind: reach, probe: [x], needs: [b]}]",
		"timeout": "caps: [{id: a, kind: reach, probe: [x], timeout: 5m}]",
		"noprobe": "caps: [{id: a, kind: reach}]",
		"rule":    "caps: [{id: a, kind: reach, probe: [x], pass: shell}]",
	} {
		if _, err := Parse([]byte(y)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}

func TestLoadLocalAdditionsReplaceByID(t *testing.T) {
	f := t.TempDir() + "/c.yaml"
	os.WriteFile(f, []byte("version: 9\ncaps: [{id: go, kind: toolchain, probe: [mygo], optional: true}, {id: mine, kind: reach, probe: [x]}]"), 0o600)
	l, err := Load(f)
	if err != nil || l.Version != 9 {
		t.Fatalf("load: %v %d", err, l.Version)
	}
	var got, mine bool
	for _, c := range l.Caps {
		got = got || (c.ID == "go" && c.Probe[0] == "mygo")
		mine = mine || c.ID == "mine"
	}
	if !got || !mine {
		t.Fatal("local list did not replace and add")
	}
}

func TestWithHubLaysOverAndNeverLoosens(t *testing.T) {
	base, err := Parse([]byte("version: 1\ncaps: [{id: a, kind: toolchain, probe: [x]}, {id: b, kind: toolchain, probe: [x]}]"))
	if err != nil {
		t.Fatal(err)
	}
	hub := "version: 4\ncaps: [{id: b, kind: toolchain, probe: [y], optional: false}, {id: c, kind: reach, probe: [z], needs: [a]}]"
	got, err := WithHub(base, []byte(hub))
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 4 || len(got.Caps) != 3 || got.Caps[1].Probe[0] != "y" || got.Caps[2].ID != "c" {
		t.Fatalf("%+v", got)
	}
	if len(base.Caps) != 2 || base.Caps[1].Probe[0] != "x" {
		t.Fatalf("the base list was changed: %+v", base)
	}
	if _, err := WithHub(base, []byte("caps: [{id: c, kind: reach, probe: [z], needs: [nope]}]")); err == nil {
		t.Fatal("a hub entry with an unresolved need was taken")
	}
}
