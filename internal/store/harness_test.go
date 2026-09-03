package store

import (
	"bytes"
	"testing"
)

// How a runner is asked to exit is per runner, and the tokens exist so the
// field can be written without knowing that control-d is 0x04.
func TestExitBytesDecodesControlKeys(t *testing.T) {
	cases := []struct {
		name string
		keys []string
		want [][]byte
	}{
		{
			// Two presses, not one write of two bytes. A program reading a
			// terminal does not see those as the same thing.
			name: "claude wants control-d twice",
			keys: []string{"ctrl-d", "ctrl-d"},
			want: [][]byte{{0x04}, {0x04}},
		},
		{
			name: "ollama and codex want it once",
			keys: []string{"ctrl-d"},
			want: [][]byte{{0x04}},
		},
		{
			// A newline is appended, because expecting the operator to add
			// "enter" as a second token is a rule they find out by it failing.
			name: "a shell wants exit and a newline",
			keys: []string{"exit"},
			want: [][]byte{[]byte("exit\r")},
		},
		{
			name: "names are case and spelling tolerant",
			keys: []string{"CTRL-C", "^d", "Enter", "esc"},
			want: [][]byte{{0x03}, {0x04}, []byte("\r"), {0x1b}},
		},
		{
			name: "blank tokens are dropped rather than sent",
			keys: []string{"", "  ", "ctrl-d"},
			want: [][]byte{{0x04}},
		},
		{
			name: "nothing configured is nothing to send",
			keys: nil,
			want: nil,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := Harness{ExitKeys: c.keys}
			got := h.ExitBytes()
			if len(got) != len(c.want) {
				t.Fatalf("got %d writes, wanted %d: %q", len(got), len(c.want), got)
			}
			for i := range got {
				if !bytes.Equal(got[i], c.want[i]) {
					t.Errorf("write %d is %q, wanted %q", i, got[i], c.want[i])
				}
			}
		})
	}
}

// The column has to survive a round trip, or the exit button silently falls
// back to the shell default for every runner.
func TestExitKeysSurviveSaving(t *testing.T) {
	s := open(t)

	if _, err := s.SaveHarness(Harness{
		ID: "probe", Cmd: "probe", LaunchMode: LaunchPTY,
		ExitKeys: []string{"ctrl-d", "ctrl-d"},
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := s.Harness("probe")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ExitKeys) != 2 || got.ExitKeys[0] != "ctrl-d" {
		t.Fatalf("exit keys came back as %q", got.ExitKeys)
	}
}

// The seeded runners each carry the sequence their tool actually wants.
func TestSeededHarnessesKnowHowToExit(t *testing.T) {
	s := open(t)
	want := map[string]int{"claude": 2, "codex": 1, "ollama": 1}

	for id, n := range want {
		h, err := s.Harness(id)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if len(h.ExitBytes()) != n {
			t.Errorf("%s sends %d keys to exit, wanted %d", id, len(h.ExitBytes()), n)
		}
	}
}
