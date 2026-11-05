package store

import "regexp"

var noNULRE = regexp.MustCompile(`^[^\x00]*$`)

// noNUL is p, or a lone NUL when p holds one, so a call that takes it fails the way it did with the NUL in.
func noNUL(p string) string {
	if !noNULRE.MatchString(p) {
		return "\x00"
	}
	return p
}
