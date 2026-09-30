package store

import "testing"

// The automatic new context settings read the safe value for anything unusable, and the
// checks refuse what is out of range.
func TestAutoNewContextSettingReads(t *testing.T) {
	s := openTestStore(t)
	if s.AutoNewContextMode() != AutoNewContextOff || s.AutoNewContextK() != DefaultAutoNewContextK ||
		s.AutoNewContextIdleS() != DefaultAutoNewContextIdleS {
		t.Fatalf("unset reads %s %d %d", s.AutoNewContextMode(), s.AutoNewContextK(), s.AutoNewContextIdleS())
	}
	for v, want := range map[string]string{
		"tagged": AutoNewContextTagged, " Agents ": AutoNewContextAgents, "off": AutoNewContextOff, "on": AutoNewContextOff,
	} {
		if err := s.SetSetting(SettingAutoNewContext, v); err != nil {
			t.Fatal(err)
		}
		if got := s.AutoNewContextMode(); got != want {
			t.Fatalf("mode %q reads %q, want %q", v, got, want)
		}
	}
	for v, want := range map[string]int{"120": 120, " 400 ": 400, "49": DefaultAutoNewContextK, "2001": DefaultAutoNewContextK, "x": DefaultAutoNewContextK} {
		if err := s.SetSetting(SettingAutoNewContextK, v); err != nil {
			t.Fatal(err)
		}
		if got := s.AutoNewContextK(); got != want {
			t.Fatalf("k %q reads %d, want %d", v, got, want)
		}
	}
	for v, want := range map[string]int{"30": 30, "9": DefaultAutoNewContextIdleS, "3601": DefaultAutoNewContextIdleS} {
		if err := s.SetSetting(SettingAutoNewContextIdleS, v); err != nil {
			t.Fatal(err)
		}
		if got := s.AutoNewContextIdleS(); got != want {
			t.Fatalf("idle %q reads %d, want %d", v, got, want)
		}
	}
}

func TestAutoNewContextSettingChecks(t *testing.T) {
	for v, ok := range map[string]bool{"": true, "off": true, "Tagged": true, "agents": true, "all": false} {
		if _, err := CheckAutoNewContext(v); (err == nil) != ok {
			t.Fatalf("mode %q: err %v, want ok=%v", v, err, ok)
		}
	}
	for v, ok := range map[string]bool{"": true, "50": true, "2000": true, "49": false, "2001": false, "1.5": false, "abc": false} {
		if _, err := CheckAutoNewContextK(v); (err == nil) != ok {
			t.Fatalf("k %q: err %v, want ok=%v", v, err, ok)
		}
	}
	for v, ok := range map[string]bool{"": true, "10": true, "3600": true, "9": false, "3601": false} {
		if _, err := CheckAutoNewContextIdleS(v); (err == nil) != ok {
			t.Fatalf("idle %q: err %v, want ok=%v", v, err, ok)
		}
	}
	if got, _ := CheckAutoNewContextK(" 120 "); got != "120" {
		t.Fatalf("a typed 120 is stored as %q", got)
	}
}
