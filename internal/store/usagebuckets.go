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
	Rows int `json:"rows"`
	// Replies is how many model calls the rows made, summed from each row's
	// `replies`. The usage tab's "calls" (r-012).
	Replies      int64   `json:"replies"`
	Input        int64   `json:"input"`
	Output       int64   `json:"output"`
	CacheWrite5m int64   `json:"cache_write_5m"`
	CacheWrite1h int64   `json:"cache_write_1h"`
	CacheRead    int64   `json:"cache_read"`
	Cost         float64 `json:"-"`
}

func (a *UsageSums) add(b *UsageSums) {
	a.Rows += b.Rows
	a.Replies += b.Replies
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
	// Groups is set only when the read asked to group, by department or by
	// director. Key "" is no department or the operator, and UsageBefore holds
	// rows from before the columns existed.
	Groups map[string]*UsageSums `json:"groups,omitempty"`
}

// UsageBefore is the group of rows written before 0073_usage_grouping.
const UsageBefore = "@before"

// The groupings a usage read takes.
const (
	UsageByDept     = "dept"
	UsageByLauncher = "launcher"
)

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
	return s.UsageBucketsBy(since, until, bucketSecs, card, "")
}

// UsageBucketsBy is UsageBuckets with each bucket also summed per group, by
// UsageByDept or UsageByLauncher. Any other group adds nothing.
func (s *Store) UsageBucketsBy(since, until time.Time, bucketSecs int, card, group string) (*UsageSeries, error) {
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
		gcol := "''"
		switch group {
		case UsageByDept:
			gcol = "COALESCE(dept, '" + UsageBefore + "')"
		case UsageByLauncher:
			gcol = "COALESCE(launcher, '" + UsageBefore + "')"
		}
		rows, err := s.db.Query(`SELECT (CAST(strftime('%s', ended_at) AS INTEGER) - ?) / ? AS b, task_id, cause, `+gcol+`,
			COUNT(*), COALESCE(SUM(replies), 0), COALESCE(SUM(input), 0), COALESCE(SUM(output), 0),
			COALESCE(SUM(cache_write_5m), 0),
			COALESCE(SUM(cache_write_1h), 0), COALESCE(SUM(cache_read), 0), COALESCE(SUM(cost), 0)
			FROM session_usage WHERE ended_at >= ? AND ended_at < ? AND (? = '' OR task_id = ?)
			GROUP BY b, task_id, cause, 4 ORDER BY b`,
			since.Unix(), bucketSecs, ts(since), ts(until), card, card)
		if err != nil {
			return err
		}
		defer rows.Close()
		var cur *UsageBucket
		curIdx := int64(-1)
		for rows.Next() {
			var (
				idx               int64
				task, cause, gkey string
				g                 UsageSums
			)
			if err := rows.Scan(&idx, &task, &cause, &gkey, &g.Rows, &g.Replies, &g.Input, &g.Output, &g.CacheWrite5m,
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
			if group == UsageByDept || group == UsageByLauncher {
				if cur.Groups == nil {
					cur.Groups = map[string]*UsageSums{}
				}
				if cur.Groups[gkey] == nil {
					cur.Groups[gkey] = &UsageSums{}
				}
				cur.Groups[gkey].add(&g)
			}
		}
		return rows.Err()
	})
	return out, err
}
