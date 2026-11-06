package safepath

import "strings"

// NoNUL is p, or a lone NUL when p holds one, so a call that takes it fails the way it did with the NUL in.
func NoNUL(p string) string {
	if strings.IndexByte(p, 0) >= 0 {
		return "\x00"
	}
	return p
}

// NoNULs is NoNUL for each of args.
func NoNULs(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		out = append(out, NoNUL(a))
	}
	return out
}
