package daemon

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/dovholuknf/atrium/internal/daemon/hublink"
)

// THE ROOM'S EVENT LOG, SENT. A hub write held while the hub was away is a row in room_event (store/roomevent.go).
// This sends them oldest first through the link this room holds, and stops at the first answer that says the hub is not
// there. The hub applies a row once by (room, seq), so a row it took and this room did not record is sent again safely.
// See docs/design/hub-offline-tolerance.md, stage 1.

type roomEventPush struct{ mu sync.Mutex }

func (d *Daemon) flushRoomEvents() {
	d.roomEvents.mu.Lock()
	defer d.roomEvents.mu.Unlock()
	max, err := d.st.RoomEventMax()
	if err != nil || max == 0 {
		return
	}
	ha, _ := d.Relay().(hublink.HubAsker)
	if ha == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), hublink.RelayWait)
	res, err := ha.Hub(ctx, "", http.MethodGet, "/_hub/room-events/apply", nil)
	cancel()
	if err != nil || !res.OK {
		return
	}
	var held struct {
		High int64 `json:"high"`
	}
	if json.Unmarshal(res.Data, &held) != nil || held.High >= max {
		return
	}
	rows, err := d.st.RoomEventsAfter(held.High, 200)
	if err != nil {
		log.Printf("[atrium] could not read the events owed to the hub: %v", err)
		return
	}
	for _, e := range rows {
		body, _ := json.Marshal(map[string]any{"seq": e.Seq, "kind": e.Kind, "key": e.Key, "method": e.Method,
			"path": e.Path, "body": json.RawMessage(e.Body), "replay": e.SentAt != ""})
		ctx, cancel := context.WithTimeout(context.Background(), hublink.RelayWait)
		res, err := ha.Hub(ctx, e.Sender, http.MethodPost, "/_hub/room-events/apply", body)
		cancel()
		switch {
		case err != nil:
			_ = d.st.RoomEventFailed(e.Seq, err.Error())
			return
		case !res.OK && res.Code >= 400 && res.Code < 500:
			_ = d.st.RoomEventFailed(e.Seq, res.Error)
			continue
		case !res.OK:
			_ = d.st.RoomEventFailed(e.Seq, res.Error)
			return
		}
		if e.SentAt != "" {
			continue
		}
		var got struct {
			Applied bool   `json:"applied"`
			Error   string `json:"error"`
		}
		_ = json.Unmarshal(res.Data, &got)
		note := ""
		if !got.Applied && got.Error != "" {
			note = "the hub refused it: " + got.Error
		}
		if err := d.st.RoomEventSent(e.Seq, note); err != nil {
			log.Printf("[atrium] the hub took event %d but it could not be marked: %v", e.Seq, err)
		}
	}
}
