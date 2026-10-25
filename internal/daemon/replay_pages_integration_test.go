//go:build integration

package daemon

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/daemon/screen"
)

var pageLine = regexp.MustCompile(`page line (\d+) `)

// MORE THAN A PAGE OF OUTPUT, STREAMED THROUGH RESIZES, REPLAYS EVERY LINE ONCE AND IN ORDER.
func TestReplayOfMoreThanAPageKeepsEveryLineOnceInOrder(t *testing.T) {
	t.Parallel()
	sizes := [][2]int{{80, 24}, {100, 24}, {100, 40}, {60, 40}, {120, 30}, {80, 24}}
	for _, clear := range []bool{false, true} {
		t.Run(fmt.Sprintf("clear=%v", clear), func(t *testing.T) {
			r := newRingSized(1<<22, 80, 24)
			n := 0
			for round := 0; round < 12; round++ {
				var b strings.Builder
				for i := 0; i < 70; i++ {
					n++
					fmt.Fprintf(&b, "page line %d %s\r\n", n, strings.Repeat("x", 30+n%40))
				}
				r.Write([]byte(b.String()))
				if clear && round%4 == 3 {
					r.Write([]byte("\x1b[2J\x1b[3J\x1b[H"))
				}
				sz := sizes[round%len(sizes)]
				r.SetSize(sz[0], sz[1])
			}
			out, cuts, rows, _ := r.ReplayCuts()
			got := plain(string(screen.ReplayCut(out, "screen", cuts, rows)))
			prev := 0
			seen := map[int]int{}
			for _, m := range pageLine.FindAllStringSubmatch(got, -1) {
				v, _ := strconv.Atoi(m[1])
				seen[v]++
			}
			for v := 1; v <= n; v++ {
				if seen[v] != 1 {
					t.Fatalf("line %d seen %d times (prev %d)", v, seen[v], prev)
				}
				prev = v
			}
			last := 0
			for _, m := range pageLine.FindAllStringSubmatch(got, -1) {
				v, _ := strconv.Atoi(m[1])
				if v < last {
					t.Fatalf("line %d after %d", v, last)
				}
				last = v
			}
		})
	}
}
