package screen

import "time"

// EscParser is a small state machine over a runner's output bytes. It does not interpret anything, it only knows
// whether the stream is between sequences (ground) or in the middle of one, so the clear mark is never written
// into the middle of a CSI, an OSC or DCS string, or a UTF-8 character. See `runner.markClear`.
type EscParser struct {
	state escState
	// need is the UTF-8 continuation bytes still owed.
	need int
}

type escState uint8

const (
	escGround escState = iota
	escEsc             // after ESC
	escCSI             // ESC [ ... until a final byte 0x40-0x7e
	escStr             // OSC, DCS, SOS, PM or APC string until BEL or ST
	escStrEsc          // ESC seen inside a string, ST if a backslash follows
	escUTF8            // inside a multi-byte character
)

// Bounds on a held mark. A runner that stops mid-sequence, or sends a string that never ends, must not keep the
// divider from drawing for good, so a mark is forced out once this many bytes have passed it or this long has
// gone by, whichever is first. A real sequence is far shorter than either.
const (
	MarkHoldBytes = 4096
	MarkHoldTime  = 2 * time.Second
)

func (p *EscParser) Ground() bool { return p.state == escGround }

// feed consumes one byte.
func (p *EscParser) Feed(b byte) {
	switch p.state {
	case escGround:
		switch {
		case b == 0x1b:
			p.state = escEsc
		case b >= 0xf0 && b <= 0xf7:
			p.state, p.need = escUTF8, 3
		case b >= 0xe0 && b <= 0xef:
			p.state, p.need = escUTF8, 2
		case b >= 0xc0 && b <= 0xdf:
			p.state, p.need = escUTF8, 1
		}
	case escEsc:
		switch {
		case b == '[':
			p.state = escCSI
		case b == ']' || b == 'P' || b == 'X' || b == '^' || b == '_':
			p.state = escStr
		case b >= 0x20 && b <= 0x2f:
			// an intermediate byte: the sequence goes on
		case b == 0x1b:
			// a fresh ESC
		default:
			p.state = escGround
		}
	case escCSI:
		switch {
		case b >= 0x40 && b <= 0x7e:
			p.state = escGround
		case b == 0x1b:
			p.state = escEsc
		case b == 0x18 || b == 0x1a:
			p.state = escGround
		}
	case escStr:
		switch b {
		case 0x07, 0x18, 0x1a:
			p.state = escGround
		case 0x1b:
			p.state = escStrEsc
		}
	case escStrEsc:
		if b == '\\' {
			p.state = escGround
		} else {
			p.state = escEsc
			p.Feed(b)
		}
	case escUTF8:
		if b >= 0x80 && b <= 0xbf {
			if p.need--; p.need == 0 {
				p.state = escGround
			}
			return
		}
		// not a continuation: the character was cut short, this byte starts afresh
		p.state = escGround
		p.Feed(b)
	}
}

// feedUntilGround consumes b and returns the index just past the first byte after which the parser is in ground
// state, or -1 if it never is. It stops there, so the caller can put something in at that point.
func (p *EscParser) FeedUntilGround(b []byte) int {
	for i, c := range b {
		p.Feed(c)
		if p.Ground() {
			return i + 1
		}
	}
	return -1
}
