package store

import (
	"sort"
	"time"
)

// Tokens per accepted work item, for the usage tab (r-015, design 3.5).
//
// An item is the work ledger's `work_item`, keyed by the card it was raised on.
// The cards linked to it are that card and every card it continues, back along
// `continues`, since a relaunched worker is a new card on the same work. A card
// linked to more than one accepted item in the range has its tokens divided
// evenly between them, so nothing is counted twice.

// ItemUsage is one accepted item's tokens. Counted is input, output and both
// kinds of cache write. CacheRead is kept apart, because it is most of the
// volume and none of the effort.
type ItemUsage struct {
	Item      string `json:"item"`
	Title     string `json:"title"`
	Counted   int64  `json:"counted"`
	CacheRead int64  `json:"cache_read"`
	Cards     int    `json:"cards"`
	// Split is the most items any one of its cards was divided across. 1 when
	// no card was shared.
	Split int `json:"split"`
}

// ItemsUsage is the answer to an items read.
type ItemsUsage struct {
	Items []*ItemUsage `json:"items"`
	// Unlinked counts the accepted items with no card that has a row on record.
	Unlinked int `json:"unlinked"`
}

// AcceptedItemUsage answers the tokens of each item accepted at or after since.
func (s *Store) AcceptedItemUsage(since time.Time) (*ItemsUsage, error) {
	since = since.UTC()
	if floor := now().Add(-UsageMaxBack); since.IsZero() || since.Before(floor) {
		since = floor
	}
	out := &ItemsUsage{Items: []*ItemUsage{}}
	err := s.guard(func() error {
		out.Items, out.Unlinked = []*ItemUsage{}, 0
		continues := map[string]string{}
		crows, err := s.db.Query(`SELECT task_id, continues FROM work_item WHERE continues != ''`)
		if err != nil {
			return err
		}
		for crows.Next() {
			var id, prev string
			if err := crows.Scan(&id, &prev); err != nil {
				crows.Close()
				return err
			}
			continues[id] = prev
		}
		crows.Close()
		if err := crows.Err(); err != nil {
			return err
		}

		type accepted struct{ id, title string }
		var items []accepted
		irows, err := s.db.Query(`SELECT task_id, title FROM work_item WHERE state = ? AND state_at >= ?
			ORDER BY state_at, task_id`, WorkAccepted, ts(since))
		if err != nil {
			return err
		}
		for irows.Next() {
			var a accepted
			if err := irows.Scan(&a.id, &a.title); err != nil {
				irows.Close()
				return err
			}
			items = append(items, a)
		}
		irows.Close()
		if err := irows.Err(); err != nil {
			return err
		}

		// Each card's tokens, all its rows.
		type tokens struct{ counted, read int64 }
		spend := map[string]tokens{}
		srows, err := s.db.Query(`SELECT task_id, COALESCE(SUM(input + output + cache_write_5m + cache_write_1h), 0),
			COALESCE(SUM(cache_read), 0) FROM session_usage GROUP BY task_id`)
		if err != nil {
			return err
		}
		for srows.Next() {
			var id string
			var t tokens
			if err := srows.Scan(&id, &t.counted, &t.read); err != nil {
				srows.Close()
				return err
			}
			spend[id] = t
		}
		srows.Close()
		if err := srows.Err(); err != nil {
			return err
		}

		linked := make([][]string, len(items))
		shared := map[string]int{}
		for i, it := range items {
			seen := map[string]bool{}
			for id := it.id; id != "" && !seen[id]; id = continues[id] {
				seen[id] = true
				if _, ok := spend[id]; ok {
					linked[i] = append(linked[i], id)
					shared[id]++
				}
			}
		}
		for i, it := range items {
			if len(linked[i]) == 0 {
				out.Unlinked++
				continue
			}
			u := &ItemUsage{Item: it.id, Title: it.title, Cards: len(linked[i]), Split: 1}
			for _, id := range linked[i] {
				n := int64(shared[id])
				u.Counted += spend[id].counted / n
				u.CacheRead += spend[id].read / n
				if int(n) > u.Split {
					u.Split = int(n)
				}
			}
			out.Items = append(out.Items, u)
		}
		sort.SliceStable(out.Items, func(i, j int) bool { return out.Items[i].Counted > out.Items[j].Counted })
		return nil
	})
	return out, err
}
