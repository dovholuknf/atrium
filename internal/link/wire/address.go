package wire

// OtherRoom is the room part when it names a room other than `own`, and empty
// when it names none or names `own`, which is the same as naming none.
func OtherRoom(room, own string) string {
	if room == "" || EqualFold(room, own) {
		return ""
	}
	return room
}

func EqualFold(a, b string) bool { return LowerASCII(a) == LowerASCII(b) }

// LowerASCII rather than strings.ToLower, so a room name is folded the same way
// on every machine regardless of locale. A Turkish dotless i is a real thing
// and a room name is not prose.
func LowerASCII(s string) string {
	out := []byte(s)
	for i, c := range out {
		if c >= 'A' && c <= 'Z' {
			out[i] = c + 32
		}
	}
	return string(out)
}
