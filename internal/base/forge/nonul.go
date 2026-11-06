package forge

import "strings"

// noNUL is p, or a lone NUL when p holds one, so a call that takes it fails the way it did with the NUL in.
func noNUL(p string) string {
	if strings.IndexByte(p, 0) >= 0 {
		return "\x00"
	}
	return p
}
