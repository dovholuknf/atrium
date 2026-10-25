//go:build integration

package sshtarget

import (
	"os"
	"strings"
	"testing"
)

func TestSSHTargetAddLoadRemove(t *testing.T) {
	dir := t.TempDir()
	if err := Add(dir, Target{Name: "wsl", Addr: "127.0.0.1:22", From: []string{"m1mini"}, User: "clint"}, false); err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir)
	if err != nil || len(s.Targets) != 1 {
		t.Fatalf("load: %+v %v", s, err)
	}
	if got, ok := Find(s, "wsl"); !ok || got.Addr != "127.0.0.1:22" || got.User != "clint" {
		t.Fatalf("find: %+v %v", got, ok)
	}
	if err := Remove(dir, "wsl"); err != nil {
		t.Fatal(err)
	}
	if s, _ := Load(dir); len(s.Targets) != 0 {
		t.Fatalf("not removed: %+v", s)
	}
	if err := Remove(dir, "wsl"); err == nil {
		t.Fatal("removing a missing name did not fail")
	}
}

func TestSSHTargetRejectsBadNameAddrFromUser(t *testing.T) {
	ok := Target{Name: "wsl", Addr: "127.0.0.1:22"}
	cases := map[string]func(*Target){
		"upper name":   func(x *Target) { x.Name = "WSL" },
		"dash name":    func(x *Target) { x.Name = "-x" },
		"long name":    func(x *Target) { x.Name = strings.Repeat("a", 33) },
		"no port":      func(x *Target) { x.Addr = "host" },
		"port zero":    func(x *Target) { x.Addr = "host:0" },
		"port too big": func(x *Target) { x.Addr = "host:70000" },
		"no host":      func(x *Target) { x.Addr = ":22" },
		"from space":   func(x *Target) { x.From = []string{"a b"} },
		"user semi":    func(x *Target) { x.User = "a;b" },
		"note newline": func(x *Target) { x.Note = "a\nb" },
	}
	if err := Validate(ok); err != nil {
		t.Fatalf("good target refused: %v", err)
	}
	for name, mod := range cases {
		x := ok
		mod(&x)
		if err := Validate(x); err == nil {
			t.Errorf("%s: accepted", name)
		}
		if err := Add(t.TempDir(), x, false); err == nil {
			t.Errorf("%s: Add accepted", name)
		}
	}
}

func TestSSHTargetMissingFileIsEmpty(t *testing.T) {
	s, err := Load(t.TempDir())
	if err != nil || len(s.Targets) != 0 {
		t.Fatalf("%+v %v", s, err)
	}
}

func TestSSHTargetWrongVersionIsAnError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(Path(dir), []byte(`{"version":2,"targets":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("err = %v", err)
	}
}

func TestSSHTargetAllowsStarAndCase(t *testing.T) {
	if Allows(Target{}, "m1mini") {
		t.Fatal("an empty from list allowed a room")
	}
	if !Allows(Target{From: []string{"*"}}, "anyone") {
		t.Fatal("* refused")
	}
	g := Target{From: []string{"M1Mini"}}
	if !Allows(g, "m1mini") || Allows(g, "sg3") {
		t.Fatal("case or exclusion wrong")
	}
}

func TestSSHTargetReplaceNeedsTheFlag(t *testing.T) {
	dir := t.TempDir()
	if err := Add(dir, Target{Name: "wsl", Addr: "127.0.0.1:22"}, false); err != nil {
		t.Fatal(err)
	}
	if err := Add(dir, Target{Name: "wsl", Addr: "127.0.0.1:2222"}, false); err == nil {
		t.Fatal("second add without replace succeeded")
	}
	if err := Add(dir, Target{Name: "wsl", Addr: "127.0.0.1:2222"}, true); err != nil {
		t.Fatal(err)
	}
	s, _ := Load(dir)
	if len(s.Targets) != 1 || s.Targets[0].Addr != "127.0.0.1:2222" {
		t.Fatalf("%+v", s)
	}
}
