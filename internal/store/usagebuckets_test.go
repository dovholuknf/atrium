package store

import (
	"testing"
	"time"
)

// r-012: a bucket's replies, in the total and per card and per cause, are the
// sum of the rows it came from. The usage tab reads them as "calls".
func TestUsageBucketsSumReplies(t *testing.T) {
	s := openTestStore(t)
	a, _, err := s.Register(Observed{WireName: "ra", Worktree: "/ra"})
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := s.Register(Observed{WireName: "rb", Worktree: "/rb"})
	if err != nil {
		t.Fatal(err)
	}
	until := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	since := until.Add(-time.Hour)
	rows := []*SessionUsage{
		{TaskID: a.ID, Ended: since.Add(5 * time.Second), Cause: UsageOperator, Replies: 3, Input: 1},
		{TaskID: a.ID, Ended: since.Add(20 * time.Second), Cause: UsageKeepalive, Replies: 1, Input: 1},
		{TaskID: b.ID, Ended: since.Add(40 * time.Second), Cause: UsageOperator, Replies: 7, Input: 1},
	}
	for _, r := range rows {
		if err := s.AddSessionUsage(r); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.UsageBuckets(since, until, 60, "")
	if err != nil || len(got.Buckets) != 1 {
		t.Fatalf("buckets %+v (%v)", got, err)
	}
	bk := got.Buckets[0]
	if bk.Total.Replies != 11 {
		t.Fatalf("total replies %d, want 11", bk.Total.Replies)
	}
	if bk.Cards[a.ID].Replies != 4 || bk.Cards[b.ID].Replies != 7 {
		t.Fatalf("per-card replies %d and %d, want 4 and 7", bk.Cards[a.ID].Replies, bk.Cards[b.ID].Replies)
	}
	if bk.Causes[UsageOperator].Replies != 10 || bk.Causes[UsageKeepalive].Replies != 1 {
		t.Fatalf("per-cause replies %+v", bk.Causes)
	}
}

// Bucket sums match the row sums, per card and per cause, and the bounds hold.
func TestUsageBucketsSumRows(t *testing.T) {
	s := openTestStore(t)
	a, _, err := s.Register(Observed{WireName: "a", Worktree: "/a"})
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := s.Register(Observed{WireName: "b", Worktree: "/b"})
	if err != nil {
		t.Fatal(err)
	}
	until := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	since := until.Add(-time.Hour)
	rows := []*SessionUsage{
		{TaskID: a.ID, Ended: since.Add(10 * time.Second), Cause: UsageOperator, Input: 1, Output: 10, CacheRead: 100, Cost: 0.5},
		{TaskID: a.ID, Ended: since.Add(50 * time.Second), Cause: UsageKeepalive, Input: 2, Output: 20, CacheWrite1h: 7, Cost: 0.25},
		{TaskID: b.ID, Ended: since.Add(70 * time.Second), Cause: UsageOperator, Input: 4, Output: 40, CacheWrite5m: 3, Cost: 1},
		{TaskID: b.ID, Ended: since.Add(-time.Second), Cause: UsageOperator, Input: 99, Cost: 99},
	}
	var sumIn int64
	var sumCost float64
	for _, r := range rows[:3] {
		sumIn += r.Input
		sumCost += r.Cost
	}
	for _, r := range rows {
		if err := s.AddSessionUsage(r); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.UsageBuckets(since, until, 60, "")
	if err != nil {
		t.Fatal(err)
	}
	only, err := s.UsageBuckets(since, until, 60, b.ID)
	if err != nil || len(only.Buckets) != 1 || only.Buckets[0].Total.Input != 4 || len(only.Buckets[0].Cards) != 1 {
		t.Fatalf("one card's read %+v (%v)", only.Buckets, err)
	}
	if len(got.Buckets) != 2 {
		t.Fatalf("buckets %d, want 2", len(got.Buckets))
	}
	first, second := got.Buckets[0], got.Buckets[1]
	if !first.Start.Equal(since) || !second.Start.Equal(since.Add(time.Minute)) {
		t.Fatalf("starts %v %v", first.Start, second.Start)
	}
	if first.Total.Rows != 2 || first.Total.Input != 3 || first.Total.Cost != 0.75 || first.Total.CacheWrite1h != 7 {
		t.Fatalf("first total %+v", first.Total)
	}
	if first.Cards[a.ID].Output != 30 || first.Causes[UsageKeepalive].Output != 20 {
		t.Fatalf("first split %+v %+v", first.Cards[a.ID], first.Causes[UsageKeepalive])
	}
	if second.Cards[b.ID].CacheWrite5m != 3 || second.Cards[a.ID] != nil {
		t.Fatalf("second cards %+v", second.Cards)
	}
	if first.Total.Input+second.Total.Input != sumIn || first.Total.Cost+second.Total.Cost != sumCost {
		t.Fatalf("buckets do not sum to the rows")
	}
}

// A range past 30 days is cut, and a width too narrow for 500 buckets is widened.
func TestUsageBucketsBounds(t *testing.T) {
	s := openTestStore(t)
	until := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	got, err := s.UsageBuckets(until.Add(-90*24*time.Hour), until, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Since.Equal(until.Add(-UsageMaxBack)) {
		t.Fatalf("since %v not held to 30 days", got.Since)
	}
	if want := int(UsageMaxBack.Seconds())/UsageMaxBuckets + 1; got.Bucket < want-1 {
		t.Fatalf("bucket %d not widened, want at least %d", got.Bucket, want-1)
	}
	if n := int(until.Sub(got.Since).Seconds()) / got.Bucket; n > UsageMaxBuckets {
		t.Fatalf("%d buckets over the cap", n)
	}
	if got.Buckets == nil {
		t.Fatal("buckets must be an empty list, not null")
	}
}
