package screen

import "strings"

// ModeTrack follows the input modes over every byte a session ever wrote, so a replay can turn them back on after
// the ring has overwritten the sequence that set them.
type ModeTrack struct {
	modes map[string]bool
	carry []byte
}

// Feed reads one chunk of output, and a sequence split across two chunks is joined by the carry.
func (m *ModeTrack) Feed(p []byte) {
	b := append(m.carry, p...)
	m.carry = nil
	for i := 0; i < len(b); i++ {
		if b[i] != 0x1b {
			continue
		}
		j := i + 1
		if j == len(b) {
			m.carry = append([]byte(nil), b[i:]...)
			return
		}
		if b[j] != '[' {
			continue
		}
		j++
		if j == len(b) {
			m.carry = append([]byte(nil), b[i:]...)
			return
		}
		if b[j] == '?' {
			j++
			k := j
			for k < len(b) && (b[k] == ';' || b[k] >= '0' && b[k] <= '9') {
				k++
			}
			if k == len(b) {
				m.carry = append([]byte(nil), b[i:]...)
				return
			}
			if b[k] == 'h' || b[k] == 'l' {
				for _, p := range strings.Split(string(b[j:k]), ";") {
					for _, im := range inputModes {
						if p == im {
							if m.modes == nil {
								m.modes = map[string]bool{}
							}
							m.modes[p] = b[k] == 'h'
						}
					}
				}
			}
			i = k
		}
	}
}

// On is the sequences that turn back on each mode the session left on.
func (m *ModeTrack) On() string {
	var b strings.Builder
	for _, im := range inputModes {
		if m.modes[im] {
			b.WriteString("\x1b[?" + im + "h")
		}
	}
	return b.String()
}
