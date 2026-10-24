package daemon

import "strings"

// otherRoom is the room part when it names a room other than this one, and
// empty when it names none or names this one, which is the same as none.
// Case does not matter, the way the hub folds room names.
func (d *Daemon) otherRoom(room string) string {
	room = strings.TrimSpace(room)
	if room == "" || strings.EqualFold(room, strings.TrimSpace(d.opts.Room)) {
		return ""
	}
	return room
}
