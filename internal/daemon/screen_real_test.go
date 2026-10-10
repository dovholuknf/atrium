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

// THE LOST-LINES FIXTURE, item 74 in docs/backlog-2.md.
//
// `lostlines` is a capture of a reply scripted as `L0001 ...` to `L0300`, each
// with a fixed tail, streamed through height resizes and followed by a repaint.
// What was lost is COUNTED, not sampled, rather than held to a floor measured
// from today's renderer.
