package store

import "time"

// The room-wide usage read behind the board's usage tab. Rows are summed in SQL
// over the (task_id, ended_at) index, so a month of them is never shipped to a
// browser.

// The limits of a usage read.
const (
	UsageMaxBuckets = 500
	UsageMaxBack    = 30 * 24 * time.Hour
)

// UsageSums is tokens per kind plus the stored cost, summed. There is no cost
// per kind: a row keeps only its last model's name, so repricing its kinds
// would misattribute money.
type UsageSums struct {
	Rows         int     `json:"rows"`
	Input        int64   `json:"input"`
	Output       int64   `json:"output"`
	CacheWrite5m int64   `json:"cache_write_5m"`
	CacheWrite1h int64   `json:"cache_write_1h"`
	CacheRead    int64   `json:"cache_read"`
	Cost         float64 `json:"cost"`
}

func (a *UsageSums) add(b *UsageSums) {
	a.Rows += b.Rows
	a.Input += b.Input
	a.Output += b.Output
	a.CacheWrite5m += b.CacheWrite5m
	a.CacheWrite1h += b.CacheWrite1h
	a.CacheRead += b.CacheRead
	a.Cost += b.Cost
}

// UsageBucket is one span of the range. Empty spans are left out.
type UsageBucket struct {
	Start  time.Time             `json:"t"`
	Total  UsageSums             `json:"total"`
	Cards  map[string]*UsageSums `json:"cards"`
	Causes map[string]*UsageSums `json:"causes"`
}

// UsageSeries is the answer to a room usage read.
type UsageSeries struct {
	Since   time.Time      `json:"since"`
	Until   time.Time      `json:"until"`
	Bucket  int            `json:"bucket"`
	Buckets []*UsageBucket `json:"buckets"`
}

// UsageBuckets sums rows ended in [since, until) into buckets of the given
// width. The range is held to UsageMaxBack, and a width too narrow to stay
// within UsageMaxBuckets is widened, the width used being reported back. A card
// id narrows it to that card, so the board can show one card's causes.
func (s *Store) UsageBuckets(since, until time.Time, bucketSecs int, card string) (*UsageSeries, error) {
	until = until.UTC()
	if until.IsZero() {
		until = now()
	}
	since = since.UTC()
	if floor := until.Add(-UsageMaxBack); since.IsZero() || since.Before(floor) {
		since = floor
	}
	if !since.Before(until) {
		since = until.Add(-time.Hour)
	}
	if bucketSecs < 1 {
		bucketSecs = 60
	}
	span := int(until.Sub(since).Seconds()) + 1
	if min := (span + UsageMaxBuckets - 1) / UsageMaxBuckets; bucketSecs < min {
		bucketSecs = min
	}
	out := &UsageSeries{Since: since, Until: until, Bucket: bucketSecs, Buckets: []*UsageBucket{}}
	err := s.guard(func() error {
		out.Buckets = []*UsageBucket{}
		rows, err := s.db.Query(`SELECT (CAST(strftime('%s', ended_at) AS INTEGER) - ?) / ? AS b, task_id, cause,
			COUNT(*), COALESCE(SUM(input), 0), COALESCE(SUM(output), 0), COALESCE(SUM(cache_write_5m), 0),
			COALESCE(SUM(cache_write_1h), 0), COALESCE(SUM(cache_read), 0), COALESCE(SUM(cost), 0)
			FROM session_usage WHERE ended_at >= ? AND ended_at < ? AND (? = '' OR task_id = ?)
			GROUP BY b, task_id, cause ORDER BY b`,
			since.Unix(), bucketSecs, ts(since), ts(until), card, card)
		if err != nil {
			return err
		}
		defer rows.Close()
		var cur *UsageBucket
		curIdx := int64(-1)
		for rows.Next() {
			var (
				idx         int64
				task, cause string
				g           UsageSums
			)
			if err := rows.Scan(&idx, &task, &cause, &g.Rows, &g.Input, &g.Output, &g.CacheWrite5m,
				&g.CacheWrite1h, &g.CacheRead, &g.Cost); err != nil {
				return err
			}
			if cur == nil || idx != curIdx {
				curIdx = idx
				cur = &UsageBucket{
					Start:  since.Add(time.Duration(idx*int64(bucketSecs)) * time.Second),
					Cards:  map[string]*UsageSums{},
					Causes: map[string]*UsageSums{},
				}
				out.Buckets = append(out.Buckets, cur)
			}
			cur.Total.add(&g)
			if cur.Cards[task] == nil {
				cur.Cards[task] = &UsageSums{}
			}
			cur.Cards[task].add(&g)
			if cur.Causes[cause] == nil {
				cur.Causes[cause] = &UsageSums{}
			}
			cur.Causes[cause].add(&g)
		}
		return rows.Err()
	})
	return out, err
}
