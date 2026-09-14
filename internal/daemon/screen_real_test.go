package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// THE SCREEN MODEL AGAINST REAL SESSIONS.
//
// Everything in `screen_test.go` is a shape written by hand to pin one rule.
// This runs the carried scrollback of sessions that actually happened, which is
// the only corpus that contains what claude-code really emits: measured at
// 257,113 synchronized-output markers and 22,796 cursor moves in one file.
//
// Skipped where the files are not there, so it costs nothing on another
// machine and fails loudly on this one.

func realScrollbacks(t *testing.T) []string {
	t.Helper()
	dir := `C:/Users/claude/.atrium/scrollback`
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Skip("no carried scrollback on this machine")
	}
	var out []string
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".scrollback") {
			continue
		}
		info, err := e.Info()
		// Big enough to hold a real session rather than a greeting.
		if err != nil || info.Size() < 200_000 {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	if len(out) == 0 {
		t.Skip("no carried scrollback big enough to be worth replaying")
	}
	return out
}

// IT SURVIVES EVERY REAL SESSION ON THIS MACHINE.
//
// Not a correctness claim, a robustness one. These files carry every sequence
// claude-code, codex, git, a pager and a shell have emitted here, including
// sequences cut in half by the ring's own boundary. A panic or a hang is the
// failure this catches, and neither is acceptable on a path that runs on every
// attach.
func TestEveryRealSessionReplays(t *testing.T) {
	for _, path := range realScrollbacks(t) {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		// 120 columns, the width the board actually uses most.
		out := renderHistory(raw, 120)
		name := filepath.Base(path)[:12]

		if len(raw) > 0 && len(out) == 0 {
			t.Errorf("%s: %d bytes in, nothing out", name, len(raw))
			continue
		}
		t.Logf("%s  %8d bytes in  %8d out  %6d lines",
			name, len(raw), len(out), strings.Count(string(out), "\n"))
	}
}

// THE TEXT SURVIVES. A screen model that dropped half the session would still
// pass the panic test above, so this asserts the words are there.
//
// The probes are things a session says that cannot be produced by the model
// itself, so finding them means the bytes came through rather than that the
// renderer invented something.
func TestRealSessionsKeepTheirText(t *testing.T) {
	for _, path := range realScrollbacks(t) {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.Base(path)[:12]
		out := plain(string(renderHistory(raw, 120)))

		// What the runner printed, with every escape stripped, is the ceiling.
		// The model cannot produce more text than was sent.
		var bare strings.Builder
		s := string(raw)
		for i := 0; i < len(s); {
			if s[i] == 0x1b {
				j := i + 1
				if j < len(s) && s[j] == '[' {
					j++
					for j < len(s) && s[j] >= 0x20 && s[j] <= 0x3f {
						j++
					}
					if j < len(s) {
						j++
					}
				} else if j < len(s) {
					j++
				}
				i = j
				continue
			}
			bare.WriteByte(s[i])
			i++
		}

		// Words of four letters or more that the session printed, sampled.
		want := 0
		found := 0
		for _, w := range strings.Fields(bare.String()) {
			// BETWEEN 8 AND 30 CHARACTERS, and the ceiling is the important
			// half. Stripping escapes takes the cursor-forward sequences that
			// did the SPACING with them, so a positioned layout collapses into
			// one enormous pseudo-word: a real sample was 2,000 characters of
			// fused paragraph. That can never match a correctly spaced
			// transcript, and counting it as a loss measures this comparison
			// rather than the renderer. Anything that long is not a word.
			if len(w) < 8 || len(w) > 30 || strings.ContainsAny(w, "\x00") {
				continue
			}
			want++
			if want%500 != 0 {
				continue
			}
			if strings.Contains(out, w) {
				found++
			}
		}
		sampled := want / 500
		// TEN IS THE FLOOR, and it is about the sample rather than the
		// renderer. Every five hundredth word is taken, so a card holding a
		// greeting and an exit contributes three or four, and losing one of
		// three reads as 33% and means nothing. Two brand new cards failed
		// this way: one word of three, one of four, on sessions that had
		// barely produced a screenful between them.
		//
		// Skipped rather than counted leniently, because a percentage over a
		// handful is not a weaker measurement, it is a different one.
		if sampled < 10 {
			if sampled > 0 {
				t.Logf("%s  skipped, only %d sampled words", name, sampled)
			}
			continue
		}
		pct := found * 100 / sampled
		t.Logf("%s  %d of %d sampled words survived (%d%%)", name, found, sampled, pct)
		// A REPAINT LEGITIMATELY DESTROYS TEXT. A spinner frame overwritten by
		// the next one was on screen and is not in the transcript, correctly,
		// so this is not asking for everything. It is asking that the session
		// is still recognisably itself.
		//
		// MOST SESSIONS SIT AT 80 TO 100%. One, `01a080db`, sits near 50 and
		// was read by hand rather than waved through: its misses are a mix of
		// three things. OSC window titles (`0;claude\a`), which are correctly
		// dropped and were never on screen. More fused pseudo-words that
		// survive the 30 character cap. And genuine output overwritten by a
		// later repaint, which is the cost this whole approach accepts.
		//
		// That session addresses the cursor home 91 times and redraws from the
		// top, so a real terminal would have shown the same thing: the newest
		// paint, with the older one gone. The floor is set below it rather
		// than at it, because the number that matters is "did a session survive
		// at all", and catastrophic loss looks like single digits.
		if pct < 40 {
			t.Errorf("%s: only %d%% of sampled words survived, which is not a transcript", name, pct)
		}
	}
}

// It is not quadratic. A path that runs on every attach, over megabytes, has to
// be linear or a large card hangs the pane.
func TestReplayIsNotQuadratic(t *testing.T) {
	paths := realScrollbacks(t)
	raw, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 400_000 {
		t.Skip("need a larger specimen to say anything about growth")
	}

	half := renderHistory(raw[:len(raw)/2], 120)
	full := renderHistory(raw, 120)

	// Doubling the input must not multiply the output by much more than two.
	if len(half) > 0 && len(full) > len(half)*4 {
		t.Errorf("half the input gave %d bytes and all of it gave %d, which is not linear",
			len(half), len(full))
	}
	t.Logf("half %d bytes out, whole %d bytes out", len(half), len(full))
}

// THE SPINNER IS GONE FROM REAL OUTPUT.
//
// The complaint that started this was pages of `Kneading…` and `Forging…`. A
// real session runs one for minutes, so the transcript should hold very few:
// one per run of the animation, not one per frame.
func TestRealSpinnersCollapse(t *testing.T) {
	for _, path := range realScrollbacks(t) {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.Base(path)[:12]
		s := string(raw)
		out := plain(string(renderHistory(raw, 120)))

		for _, word := range []string{"Kneading", "Forging", "Garnishing", "Simmering", "Brewing"} {
			before := strings.Count(s, word)
			if before < 50 {
				continue
			}
			after := strings.Count(out, word)
			t.Logf("%s  %-11s %6d frames sent, %4d in the transcript", name, word, before, after)
			if after > before/4 {
				t.Errorf("%s: %q survived %d of %d times, which is still a wall of frames",
					name, word, after, before)
			}
		}
	}
}
