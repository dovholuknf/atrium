package daemon

// Replaying a captured byte stream, for anybody outside this package.
//
// The renderers live here because `attach` is here, and until now the only way
// to run one was to attach to a live card. That made every change to how
// scrollback renders a build, a restart, and a reading by eye of a terminal
// pane, which is a loop slow enough that the rendering was twice declared
// fixed on the strength of tests written beside it and twice reverted.
//
// `atrium replay` is the way in. See `internal/cli/replay.go`.

// ReplayModes are the renderings, in the order they interpret the stream least
// to most.
var ReplayModes = []string{"raw", "flat", "screen"}

// Replay renders captured terminal bytes the way an attach would.
//
// `cols` and `rows` are the size the bytes were COMPOSED at, not the size
// something will display them at. A terminal user interface writes hard line
// breaks and absolute cursor moves for a specific grid, so replaying into a
// different one is the difference between reading the output and reading its
// wreckage. The ring records both; a capture taken any other way has to be
// told.
//
// Zero for either takes the default, which is 80 columns and `screenRows`.
func Replay(b []byte, mode string, cols, rows int) []byte {
	switch mode {
	case "raw":
		// Nothing at all. What a terminal receiving this stream would be given.
		return b
	case "flat":
		// COLLAPSE FIRST, and only here. Without a grid the flattener turns a
		// spinner that redrew four hundred times into four hundred lines, and
		// `collapseRedraws` is what stops that. It also deletes real text, so
		// nothing that has a grid should ever be handed its output. See the
		// note on `collapseRedraws`.
		return flatten(collapseRedraws(b))
	default:
		sc := newScreenSized(cols, rows)
		sc.apply(b)
		return []byte(sc.text())
	}
}

// ReplayStats are the numbers worth knowing about a rendering without reading
// it. Enough to answer "did that change help" in one line, and never enough to
// answer "is it right", which needs eyes.
type ReplayStats struct {
	Bytes  int
	Lines  int
	Blank  int
	Padded int
}

// StatsFor counts what a rendering came out as.
func StatsFor(out []byte) ReplayStats {
	st := ReplayStats{Bytes: len(out)}
	for _, line := range splitKeepingEndings(out) {
		st.Lines++
		trimmed := stripSGR(line)
		body := string(trimmed)
		for len(body) > 0 && (body[len(body)-1] == '\n' || body[len(body)-1] == '\r') {
			body = body[:len(body)-1]
		}
		if body == "" {
			st.Blank++
			continue
		}
		if body[len(body)-1] == ' ' {
			st.Padded++
		}
	}
	return st
}
