package daemon

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// THE SIZE A NEW TERMINAL OPENS AT, chosen so the first viewer has nothing to
// resize.
//
// A card with no size of its own used to open at `launchCols` by `launchRows`,
// and the board attached a moment later at whatever its pane really was. Each
// resize ends in the terminal host's bare `ESC [ H` repaint, and that repaint's
// first row is a different line every time, because the host reflows on a width
// change and pulls history back when the height grows. The replay applied the
// repaint in place, so lines already filed were painted again and filed twice:
// half-drawn copies of the banner, and the first prompt once per width.
//
// So the terminal opens at the size it will be watched at. In order: what this
// card was last at, then what the ROOM last agreed on for any runner, then the
// launch default for a room that has never had a viewer. See `noteRoomSize`.

// roomSize is the last size a viewer agreed on, read from memory once loaded.
// The zero value means unknown.
func (d *Daemon) roomSize() viewport {
	d.roomMu.Lock()
	defer d.roomMu.Unlock()
	if !d.roomLoaded {
		d.roomLoaded = true
		if v, err := d.st.Setting(store.SettingRoomViewport); err == nil {
			d.roomView = parseViewport(v)
		}
	}
	return d.roomView
}

// noteRoomSize remembers the size a viewer just agreed on, and writes it down
// only when it changed, so a restart keeps it without a drag hitting the database.
func (d *Daemon) noteRoomSize(cols, rows int) {
	if cols <= 0 || rows <= 0 {
		return
	}
	want := viewport{cols, rows}
	if d.roomSize() == want {
		return
	}
	d.roomMu.Lock()
	d.roomView = want
	d.roomMu.Unlock()
	if err := d.st.SetSetting(store.SettingRoomViewport, fmt.Sprintf("%dx%d", cols, rows)); err != nil {
		log.Printf("[atrium] could not record the room viewport: %v", err)
	}
}

// parseViewport reads `COLSxROWS`. Anything unusable reads as unknown.
func parseViewport(s string) viewport {
	c, r, ok := strings.Cut(strings.TrimSpace(s), "x")
	if !ok {
		return viewport{}
	}
	cols, err1 := strconv.Atoi(c)
	rows, err2 := strconv.Atoi(r)
	if err1 != nil || err2 != nil || cols <= 0 || rows <= 0 {
		return viewport{}
	}
	return viewport{cols, rows}
}

// launchSizeFor is the width and height to open THIS CARD's terminal at.
//
// A half-known card takes its own half and the room's other half, so a card
// saved before heights were recorded still opens at the height being watched.
func (d *Daemon) launchSizeFor(taskID string) (cols, rows int) {
	if taskID != "" {
		if t, err := d.st.Get(taskID); err == nil {
			cols, rows = t.LastCols, t.LastRows
		}
	}
	if cols > 0 && rows > 0 {
		return cols, rows
	}
	room := d.roomSize()
	if cols <= 0 {
		cols = room.cols
	}
	if rows <= 0 {
		rows = room.rows
	}
	if cols <= 0 {
		cols = launchCols
	}
	if rows <= 0 {
		rows = launchRows
	}
	return cols, rows
}
