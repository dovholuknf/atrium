package daemon

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// THE SCREEN MODEL AGAINST REAL SESSIONS, IN TWO CORPORA.
//
// Everything in `screen_test.go` is a shape written by hand to pin one rule.
// This runs scrollback that claude-code really emitted, which is the only corpus
// that contains what it does: synchronized output, cursor-home redraws, `\r`
// spinner frames. One live file measured 257,113 synchronized-output markers
// and 22,796 cursor moves.
//
// FROZEN, ALWAYS RUN. `testdata/scrollback/` holds a few captures made on a
// throwaway room from scripted work on atrium's own public source. Fixed bytes,
// so the numbers are deterministic and each fixture pins its own floor below.
// This is the regression suite. `testdata/scrollback/README.md` says how each
// was made.
//
// LIVE, RUN ON PURPOSE. The same bodies over the room's own ring, when
// ATRIUM_REAL_SCROLLBACK names a directory, or is `1` for
// `<home>/.atrium/scrollback`. That corpus is whatever this machine did lately,
// so it can fail with no line of code changed, and it must never gate a suite.
// Use it before a screen.go change, or when chasing item 74.
//
// An env var and not a build tag: a tag keeps the file out of `go vet` and the
// compile, and it rots unseen.

// pinnedFloors is the survival percentage each ordinary fixture must keep, keyed
// by file name without `.gz`. It is the measured figure less 5 points, and never
// below 40. A fixture that measures under 80 is read by hand before it is
// pinned. Recapturing a fixture and re-pinning it is one diff here.
//
// A fixture with no entry has no floor: the lost-lines capture is judged by its
// own two tests, since a floor measured from today's renderer would bless the
// bug it exists to catch.
var pinnedFloors = map[string]int{
	// Measured 87% (35 of 40 sampled words).
	"session-reply.scrollback": 82,
	// Measured 70% (14 of 20), under the 80 an ordinary session should reach, so
	// it was read by hand. All six misses are things the model correctly does
	// not keep: a fused pseudo-word from a positioned layout (`internal\daemon\
	// screen.go`, the status line, `PosToolUsehok`), an OSC title, a spinner
	// fragment, and the `SessionStart` hook line that a later repaint replaced.
	"session-tools.scrollback": 65,
}

// liveFloor is the old global bar, kept only where the corpus is not chosen.
const liveFloor = 40

// Every Nth eligible word is sampled. A live ring is megabytes, so 500 leaves
// plenty. A fixture is 50 to 90KB, which holds too few eligible words for 500 to
// reach the ten-sample minimum, so fixtures sample every 20th, which gives session-tools its ten.
const (
	liveStride    = 500
	fixtureStride = 20
)

// realFile is one scrollback to replay: a short name for logs, its bytes, and
// the floor it is held to (0 for none).
type realFile struct {
	name  string
	raw   []byte
	floor int
}

// readScrollback reads a `.scrollback` or `.scrollback.gz`.
func readScrollback(t testing.TB, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasSuffix(path, ".gz") {
		zr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		raw, err = io.ReadAll(zr)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
	return raw
}

// shortName is the log name: the first twelve characters of a card's file.
func shortName(path string) string {
	n := strings.TrimSuffix(strings.TrimSuffix(filepath.Base(path), ".gz"), ".scrollback")
	if len(n) > 12 {
		n = n[:12]
	}
	return n
}

// fixtureScrollbacks reads testdata, sorted by name so logs do not depend on
// directory order. It never skips: an empty directory is a failure, because a
// regression suite that finds nothing to check passes by doing nothing.
func fixtureScrollbacks(t testing.TB) []realFile {
	t.Helper()
	dir := filepath.Join("testdata", "scrollback")
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("no fixture directory: %v", err)
	}
	var out []realFile
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !(strings.HasSuffix(n, ".scrollback") || strings.HasSuffix(n, ".scrollback.gz")) {
			continue
		}
		key := strings.TrimSuffix(n, ".gz")
		out = append(out, realFile{
			name:  strings.TrimSuffix(key, ".scrollback"),
			raw:   readScrollback(t, filepath.Join(dir, n)),
			floor: pinnedFloors[key],
		})
	}
	if len(out) == 0 {
		t.Fatalf("%s holds no .scrollback fixtures", dir)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
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

// largestFixture is chosen by size, explicitly, not by position.
func largestFixture(t testing.TB) realFile {
	t.Helper()
	fs := fixtureScrollbacks(t)
	best := fs[0]
	for _, f := range fs[1:] {
		if len(f.raw) > len(best.raw) {
			best = f
		}
	}
	return best
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

// survival samples every five hundredth eligible word the session printed and
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

// BenchmarkReplayGrowth is the time half of the growth question, for manual
// comparison: ns/op should roughly double from 1x to 2x to 4x.
//
//	go test ./internal/daemon/ -run '^$' -bench ReplayGrowth
func BenchmarkReplayGrowth(b *testing.B) {
	raw := largestFixture(b).raw
	for _, n := range []int{1, 2, 4} {
		in := bytes.Repeat(raw, n)
		b.Run(fmt.Sprintf("%dx", n), func(b *testing.B) {
			b.SetBytes(int64(len(in)))
			for i := 0; i < b.N; i++ {
				renderHistory(in, 120)
			}
		})
	}
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

// THE LOST-LINES FIXTURE, item 74 in docs/backlog-2.md.
//
// `lostlines` is a capture of a reply scripted as `L0001 ...` to `L0300`, each
// with a fixed tail, followed by a repaint while the pane was scrolled. What was
// lost is COUNTED, not sampled, and it is not held to a floor measured from
// today's renderer, which would bless the bug.

const (
	lostFixture = "lostlines"
	lostTail    = "lostlines-tail"
	lostBefore  = "LOSTLINES-BEGIN"
	lostAfter   = "LOSTLINES-AFTER-REPAINT"
	lostCount   = 300
)

// lostSurvivors counts the numbered lines in the transcript, and whether the
// ones that survived are in order.
func lostSurvivors(t testing.TB) (kept int, inOrder bool, text string) {
	t.Helper()
	text = plain(string(renderHistory(fixtureNamed(t, lostFixture), 120)))
	inOrder = true
	last := -1
	for i := 1; i <= lostCount; i++ {
		at := strings.Index(text, fmt.Sprintf("L%04d %s", i, lostTail))
		if at < 0 {
			continue
		}
		kept++
		if at < last {
			inOrder = false
		}
		last = at
	}
	return
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

// TestLostLinesFixtureKeepsEveryLine is the target. The fix for item 74 removes
// the skip in the same commit, so the suite shows the fix landing.
func TestLostLinesFixtureKeepsEveryLine(t *testing.T) {
	t.Skip("item 74 open: a repaint over a scrolled pane drops lines, see docs/backlog-2.md item 74")
	kept, inOrder, _ := lostSurvivors(t)
	if kept != lostCount || !inOrder {
		t.Errorf("%s: %d of %d numbered lines survived, in order: %v", lostFixture, kept, lostCount, inOrder)
	}
}
