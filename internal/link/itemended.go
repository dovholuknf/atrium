package link

import (
	"context"
	"encoding/json"
	"log"
	"net/url"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

// A backlog item that goes done, dropped or held tells each card attached to it, and the card's room decides whether
// to close it or exit its runner (internal/api/itemended.go). The hub only passes the word on, off the request that
// moved the item, and every failure is logged and nothing more: the status is already recorded.

func (p *Proxy) backlogEnded(id, status, by string) {
	switch status {
	case hubstore.ItemDone, hubstore.ItemDropped, hubstore.ItemHeld:
	default:
		return
	}
	st := p.itemStore()
	if st == nil {
		return
	}
	p.work.Add(1)
	go func() {
		defer p.work.Done()
		cards, err := st.WorkCards(id)
		if err != nil {
			log.Printf("[hub] item %s %s: could not list its cards: %v", id, status, err)
			return
		}
		body, _ := json.Marshal(map[string]string{"item": id, "status": status, "by": by})
		for _, c := range cards {
			code, resp, err := p.callItemRoom(context.Background(), c.Room, "/v1/tasks/"+url.PathEscape(c.Card)+"/item-ended", body)
			if err != nil || code != 200 {
				log.Printf("[hub] item %s %s: %s on %s did not answer: %d %v %.200s", id, status, c.Card, c.Room, code, err, resp)
				continue
			}
			log.Printf("[hub] item %s %s: told %s on %s: %.200s", id, status, c.Card, c.Room, resp)
			p.RecordAudit(c.Room, "item-ended", id+" "+status+" "+c.Card)
		}
	}()
}
