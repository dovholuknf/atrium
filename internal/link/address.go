package link

// otherRoom is the room part when it names a room other than `own`, and empty
// when it names none or names `own`, which is the same as naming none.
func otherRoom(room, own string) string {
	if room == "" || equalFold(room, own) {
		return ""
	}
	return room
}
