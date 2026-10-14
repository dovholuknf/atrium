//go:build integration

package remoteaccess

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMissingFileIsOff(t *testing.T) {
	st := Effective(t.TempDir())
	if st.On || st.Err != nil {
		t.Fatalf("a room with no file must be off: %+v", st)
	}
	if st.Allows("hub") {
		t.Fatal("an off room allowed the hub")
	}
}

func TestSaveLoadAndAllows(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, State{On: true, Port: 2222, From: []string{"hub", "M1Mini"}, SetBy: "claude@sg3"}); err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir)
	if err != nil || !s.On || s.Port != 2222 || s.SetAt == "" || s.SetBy != "claude@sg3" {
		t.Fatalf("round trip: %+v %v", s, err)
	}
	for who, want := range map[string]bool{"hub": true, "m1mini": true, "sg4": false, "": false} {
		if got := s.Allows(who); got != want {
			t.Errorf("Allows(%q) = %v, want %v", who, got, want)
		}
	}
	if fi, err := os.Stat(filepath.Join(dir, File)); err != nil || (os.PathSeparator == '/' && fi.Mode().Perm() != 0o600) {
		t.Fatalf("file mode: %v %v", fi, err)
	}
}

func TestStarMeansEveryCaller(t *testing.T) {
	s := State{On: true, From: []string{"*"}}
	if !s.Allows("hub") || !s.Allows("anyroom") {
		t.Fatal("* did not allow every caller")
	}
	if (State{On: false, From: []string{"*"}}).Allows("hub") {
		t.Fatal("off with * still allowed")
	}
}

func TestEnvCanOnlyTurnItOff(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvForce, "on")
	if Effective(dir).On {
		t.Fatal("the environment turned it on")
	}
	_ = Save(dir, State{On: true, From: []string{"*"}})
	if !Effective(dir).On {
		t.Fatal("on in the file was not on")
	}
	t.Setenv(EnvForce, "off")
	st := Effective(dir)
	if st.On || !st.Forced || !strings.Contains(st.Describe(), "forced by ATRIUM_REMOTE_ACCESS") {
		t.Fatalf("forced off: %+v %q", st, st.Describe())
	}
	if st.Allows("hub") {
		t.Fatal("a forced off room allowed the hub")
	}
}

func TestBrokenFileIsOffWithTheReason(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, File), []byte("{not json"), 0o600)
	st := Effective(dir)
	if st.On || st.Err == nil {
		t.Fatalf("a broken file must read as off with an error: %+v", st)
	}
}

func TestParseFrom(t *testing.T) {
	if got, err := ParseFrom("hub, m1mini"); err != nil || len(got) != 2 || got[1] != "m1mini" {
		t.Fatalf("%v %v", got, err)
	}
	if got, err := ParseFrom("*"); err != nil || got[0] != "*" {
		t.Fatalf("%v %v", got, err)
	}
	if _, err := ParseFrom("a b"); err == nil {
		t.Fatal("a bad name was accepted")
	}
	if _, err := ParseFrom(""); err == nil {
		t.Fatal("an empty list was accepted")
	}
}

func TestLogAppendsRotatesAndReadsNewestFirst(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		AppendLog(dir, LogLine{Event: "open", ID: "s" + string(rune('a'+i)), Caller: "hub", Start: time.Now()})
	}
	got := RecentLog(dir, 2)
	if len(got) != 2 || got[0].ID != "sc" || got[1].ID != "sb" {
		t.Fatalf("recent: %+v", got)
	}
	big := strings.Repeat("x", 1024)
	for i := 0; i < 1200; i++ {
		AppendLog(dir, LogLine{Event: "refused", ID: "big", Caller: "hub", Error: big})
	}
	if _, err := os.Stat(filepath.Join(dir, LogFile+".1")); err != nil {
		t.Fatalf("the log did not rotate: %v", err)
	}
	if fi, _ := os.Stat(filepath.Join(dir, LogFile)); fi != nil && fi.Size() > 1<<20+4096 {
		t.Fatalf("the live log is %d bytes", fi.Size())
	}
}

func TestAddrDefaultsRoundTripsAndIsValidated(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, State{On: true, From: []string{"hub"}}); err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir)
	if err != nil || s.Host() != "127.0.0.1" || s.Addr != "" {
		t.Fatalf("a file with no addr dials 127.0.0.1: %+v %v", s, err)
	}
	for _, a := range []string{"192.168.1.147", "::1", "sg3.lan", "fe80::1"} {
		if err := Save(dir, State{On: true, Addr: a, From: []string{"hub"}}); err != nil {
			t.Fatal(err)
		}
		if s, err := Load(dir); err != nil || s.Host() != a {
			t.Fatalf("%s: %+v %v", a, s, err)
		}
	}
	for _, bad := range []string{"a b", "-x", "host:22", "[::1]", "a/b"} {
		if ValidAddr(bad) {
			t.Fatalf("%q passed as an address", bad)
		}
		if err := Save(dir, State{On: true, Addr: bad}); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(dir); err == nil {
			t.Fatalf("a file with addr %q loaded", bad)
		}
	}
}

func TestDescribeNamesANonDefaultAddr(t *testing.T) {
	if got := (Status{State: State{On: true, Port: 22, From: []string{"hub"}}}).Describe(); got != "on, port 22, from hub" {
		t.Fatal(got)
	}
	got := (Status{State: State{On: true, Addr: "::1", Port: 22, From: []string{"hub"}}}).Describe()
	if got != "on, [::1]:22, from hub" {
		t.Fatal(got)
	}
}
