//go:build integration

package daemon

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// liveFloor is the old global bar, kept only where the corpus is not chosen.
const liveFloor = 40

// Every Nth eligible word is sampled. A live ring is megabytes, so 500 leaves
// plenty. A fixture is 50 to 90KB, which holds too few eligible words for 500 to
// reach the ten-sample minimum, so fixtures sample every 20th, which gives session-tools its ten.
const (
	liveStride    = 500
	fixtureStride = 20
)

// shortName is the log name: the first twelve characters of a card's file.
func shortName(path string) string {
	n := strings.TrimSuffix(strings.TrimSuffix(filepath.Base(path), ".gz"), ".scrollback")
	if len(n) > 12 {
		n = n[:12]
	}
	return n
}

// fixtureNamed returns one fixture's bytes by stem, failing when it is missing.
func fixtureNamed(t testing.TB, name string) []byte {
	t.Helper()
	for _, f := range fixtureScrollbacks(t) {
		if f.name == name {
			return f.raw
		}
	}
	t.Fatalf("fixture %q is missing from testdata/scrollback", name)
	return nil
}

// liveScrollbacks reads the corpus ATRIUM_REAL_SCROLLBACK names, and skips,
// naming the variable, when it is unset.
func liveScrollbacks(t *testing.T) []realFile {
	t.Helper()
	v := os.Getenv("ATRIUM_REAL_SCROLLBACK")
	if v == "" {
		t.Skip("set ATRIUM_REAL_SCROLLBACK to a directory, or 1 for <home>/.atrium/scrollback, to replay a live corpus")
	}
	dir := v
	if v == "1" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Skipf("ATRIUM_REAL_SCROLLBACK=1 but no home directory: %v", err)
		}
		dir = filepath.Join(home, ".atrium", "scrollback")
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("ATRIUM_REAL_SCROLLBACK: no scrollback at %s", dir)
	}
	var out []realFile
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".scrollback") {
			continue
		}
		info, err := e.Info()
		// Big enough to hold a real session rather than a greeting.
		if err != nil || info.Size() < 200_000 {
			continue
		}
		out = append(out, realFile{
			name:  shortName(e.Name()),
			raw:   readScrollback(t, filepath.Join(dir, e.Name())),
			floor: liveFloor,
		})
	}
	if len(out) == 0 {
		t.Skipf("no scrollback over 200KB in %s", dir)
	}
	return out
}

// IT SURVIVES EVERY REAL SESSION.
//
// Not a correctness claim, a robustness one. These files carry every sequence
// claude-code has emitted, including sequences cut in half by the ring's own
// boundary. A panic or a hang is the failure this catches, and neither is
// acceptable on a path that runs on every attach.
func checkReplays(t *testing.T, f realFile) {
	t.Helper()
	// 120 columns, the width the board actually uses most.
	start := time.Now()
	out := renderHistory(f.raw, 120)
	took := time.Since(start)

	if len(f.raw) > 0 && len(out) == 0 {
		t.Errorf("%s: %d bytes in, nothing out", f.name, len(f.raw))
		return
	}
	t.Logf("%s  %8d bytes in  %8d out  %6d lines  %v",
		f.name, len(f.raw), len(out), strings.Count(string(out), "\n"), took.Round(time.Millisecond))
}

func TestEveryRealSessionReplays(t *testing.T) {
	for _, f := range fixtureScrollbacks(t) {
		checkReplays(t, f)
	}
}

func TestLiveEverySessionReplays(t *testing.T) {
	for _, f := range liveScrollbacks(t) {
		checkReplays(t, f)
	}
}

// survival samples every stride-th eligible word the session printed and
// reports how many came through the renderer. sampled is the sample size.
func survival(raw []byte, stride int) (found, sampled int) {
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

	// Words of eight to thirty characters that the session printed, sampled.
	want := 0
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
		if want%stride != 0 {
			continue
		}
		if strings.Contains(out, w) {
			found++
		}
	}
	return found, want / stride
}

// THE TEXT SURVIVES. A screen model that dropped half the session would still
// pass the panic test above, so this asserts the words are there.
//
// The probes are things a session says that cannot be produced by the model
// itself, so finding them means the bytes came through rather than that the
// renderer invented something.
//
// A REPAINT LEGITIMATELY DESTROYS TEXT. A spinner frame overwritten by the next
// one was on screen and is not in the transcript, correctly, so this is not
// asking for everything. It is asking that the session is still recognisably
// itself. Live, one session (`01a080db`) sat near 50 and was read by hand: OSC
// window titles that were never on screen, fused pseudo-words that survive the
// length cap, and output a later repaint overwrote, which is the cost this
// approach accepts. Catastrophic loss looks like single digits.
func checkKeepsText(t *testing.T, f realFile, live bool) {
	t.Helper()
	stride := fixtureStride
	if live {
		stride = liveStride
	}
	found, sampled := survival(f.raw, stride)
	// TEN IS THE FLOOR, LIVE, and it is about the sample rather than the
	// renderer. Every five hundredth word is taken, so a card holding a
	// greeting and an exit contributes three or four, and losing one of
	// three reads as 33% and means nothing. Skipped rather than counted
	// leniently, because a percentage over a handful is a different
	// measurement. A fixture is chosen to be big enough, so a small one is a
	// bad capture and fails.
	if sampled < 10 {
		if !live {
			t.Errorf("%s: only %d sampled words, the fixture is too small to pin", f.name, sampled)
		} else if sampled > 0 {
			t.Logf("%s  skipped, only %d sampled words", f.name, sampled)
		}
		return
	}
	pct := found * 100 / sampled
	t.Logf("%s  %d of %d sampled words survived (%d%%), floor %d%%", f.name, found, sampled, pct, f.floor)
	if pct < f.floor {
		t.Errorf("%s: only %d%% of sampled words survived, pinned floor %d%%, which is not a transcript",
			f.name, pct, f.floor)
	}
}

func TestRealSessionsKeepTheirText(t *testing.T) {
	for _, f := range fixtureScrollbacks(t) {
		if f.floor == 0 {
			continue // the lost-lines capture has its own tests
		}
		checkKeepsText(t, f, false)
	}
}

func TestLiveSessionsKeepTheirText(t *testing.T) {
	for _, f := range liveScrollbacks(t) {
		checkKeepsText(t, f, true)
	}
}

// THE OUTPUT GROWS LINEARLY WITH THE INPUT. A path that runs on every attach,
// over megabytes, must not multiply its output. This checks SIZE only: a
// renderer that is quadratic in time and linear in output passes it, which is
// what BenchmarkReplayGrowth is for. Time is not asserted, because a timing
// ratio under a loaded suite is the flake this split removed.
//
// Doubling the input must not multiply the output by much more than two. The
// bound is 3 for a fixture, whose 4x is the same bytes twice over, and 4 live,
// where the second half of a ring is different content and can legitimately
// paint more.
func checkLinearOutput(t *testing.T, name string, small, big []byte, bound int) {
	t.Helper()
	a := renderHistory(small, 120)
	b := renderHistory(big, 120)
	if len(a) > 0 && len(b) > len(a)*bound {
		t.Errorf("%s: %d bytes in gave %d out and %d in gave %d out, which is not linear",
			name, len(small), len(a), len(big), len(b))
	}
	t.Logf("%s  %d in -> %d out, %d in -> %d out", name, len(small), len(a), len(big), len(b))
}

func TestReplayOutputGrowsLinearly(t *testing.T) {
	f := largestFixture(t)
	checkLinearOutput(t, f.name, bytes.Repeat(f.raw, 2), bytes.Repeat(f.raw, 4), 3)
}

func TestLiveReplayOutputGrowsLinearly(t *testing.T) {
	for _, f := range liveScrollbacks(t) {
		if len(f.raw) < 400_000 {
			continue
		}
		// ONE SPECIMEN, as before: the first by name. A ring's own cut point
		// makes half of an arbitrary file a poor stand-in for a smaller
		// session, and seven of forty files exceed 4x that way on healthy code.
		checkLinearOutput(t, f.name, f.raw[:len(f.raw)/2], f.raw, 4)
		return
	}
	t.Skip("no live scrollback over 400KB to say anything about growth")
}

// THE SPINNER IS GONE FROM REAL OUTPUT.
//
// The complaint that started this was pages of `Kneading…` and `Forging…`. A
// real session runs one for minutes, so the transcript should hold very few:
// one per run of the animation, not one per frame. Returns the most frames of
// any one word the file sent.
func checkSpinners(t *testing.T, f realFile) int {
	t.Helper()
	s := string(f.raw)
	out := plain(string(renderHistory(f.raw, 120)))
	most := 0
	for _, word := range []string{"Kneading", "Forging", "Garnishing", "Simmering", "Brewing"} {
		before := strings.Count(s, word)
		if before < 50 {
			continue
		}
		if before > most {
			most = before
		}
		after := strings.Count(out, word)
		t.Logf("%s  %-11s %6d frames sent, %4d in the transcript", f.name, word, before, after)
		if after > before/4 {
			t.Errorf("%s: %q survived %d of %d times, which is still a wall of frames",
				f.name, word, after, before)
		}
	}
	return most
}

func TestRealSpinnersCollapse(t *testing.T) {
	most := 0
	for _, f := range fixtureScrollbacks(t) {
		if m := checkSpinners(t, f); m > most {
			most = m
		}
	}
	// It must not pass by finding nothing to check.
	if most < 50 {
		t.Errorf("no fixture holds 50 spinner frames of one word (most: %d), so nothing was checked", most)
	}
}

func TestLiveSpinnersCollapse(t *testing.T) {
	for _, f := range liveScrollbacks(t) {
		checkSpinners(t, f)
	}
}

const (
	lostFixture = "lostlines"
	lostTail    = "lostlines-tail"
	lostBefore  = "LOSTLINES-BEGIN"
	lostAfter   = "LOSTLINES-AFTER-REPAINT"
	lostCount   = 300
)

// lostSurvivors counts the numbered lines of the REPLY in the transcript, and
// whether the ones that survived are in order.
//
// THE PROMPT NAMES THE FIRST AND LAST LINE TOO ("L0001 lostlines-tail to L0300
// lostlines-tail"), and it sits above the reply, so a first match is the prompt's
// for those two. The reply starts at the L0001 just before the first L0002, and
// every line is looked for after that point, in reply order. A line out of place
// is found by a plain search from the start but not by the ordered one.
func lostSurvivors(t testing.TB) (kept int, inOrder bool, text string) {
	t.Helper()
	text = plain(string(renderHistory(fixtureNamed(t, lostFixture), 120)))
	line := func(i int) string { return fmt.Sprintf("L%04d %s", i, lostTail) }

	start := 0
	if at2 := strings.Index(text, line(2)); at2 >= 0 {
		start = at2
		if at1 := strings.LastIndex(text[:at2], line(1)); at1 >= 0 {
			start = at1
		}
	}
	anywhere := 0
	pos := start
	for i := 1; i <= lostCount; i++ {
		if strings.Contains(text[start:], line(i)) {
			anywhere++
		}
		if at := strings.Index(text[pos:], line(i)); at >= 0 {
			kept++
			pos += at + len(line(i))
		}
	}
	return kept, kept == anywhere, text
}

// TestLostLinesFixtureRenders runs today. The lines before the long reply and
// after the repaint are what the bug does not touch, so they must be there. It
// logs how many of the 300 survived, so the number is visible without being a
// bar anyone has to clear.
func TestLostLinesFixtureRenders(t *testing.T) {
	kept, _, text := lostSurvivors(t)
	for _, marker := range []string{lostBefore, lostAfter} {
		if !strings.Contains(text, marker) {
			t.Errorf("%s: %q is missing from the transcript", lostFixture, marker)
		}
	}
	t.Logf("%s  %d of %d numbered lines survived", lostFixture, kept, lostCount)
}

// EVERY LINE OF A LONG REPLY SURVIVES A STREAM OF RESIZES.
//
// This was meant to be item 74's target, skipped until the fix. It is not: the
// capture was made on a build with the height hold (387ccd5), and 18 height
// resizes during the stream lost nothing, so the bytes already hold all 300.
// That makes it a guard on the renderer, run every time. A capture that really
// loses lines needs a build without the hold, and belongs to item 74.
func TestLongReplyThroughResizesKeepsEveryLine(t *testing.T) {
	kept, inOrder, _ := lostSurvivors(t)
	if kept != lostCount || !inOrder {
		t.Errorf("%s: %d of %d numbered lines survived, in order: %v", lostFixture, kept, lostCount, inOrder)
	}
}
