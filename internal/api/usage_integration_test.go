//go:build integration

package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// The room usage read answers buckets, holds the range, and refuses nonsense.
func TestUsageRoomEndpointShapeAndBounds(t *testing.T) {
	s, st, dir := fileServer(t)
	task := cardIn(t, st, dir)
	end := time.Now().UTC().Add(-30 * time.Second)
	if err := st.AddSessionUsage(&store.SessionUsage{TaskID: task.ID, Ended: end, Cause: store.UsageOperator,
		Input: 5, Output: 50, CacheRead: 500, Cost: 0.25}); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	s.roomUsage(w, httptest.NewRequest("GET", "/v1/usage?bucket=60", nil))
	if w.Code != 200 {
		t.Fatalf("answered %d: %s", w.Code, w.Body.String())
	}
	var got store.UsageSeries
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Buckets) != 1 || got.Buckets[0].Cards[task.ID].Output != 50 {
		t.Fatalf("buckets %+v", got.Buckets)
	}
	// A stored cost (older rows have one) is never sent.
	if body := strings.ToLower(w.Body.String()); strings.Contains(body, "cost") || strings.Contains(body, "$") {
		t.Fatalf("money in the usage response: %s", w.Body.String())
	}

	since := time.Now().UTC().Add(-365 * 24 * time.Hour).Format(time.RFC3339)
	w = httptest.NewRecorder()
	s.roomUsage(w, httptest.NewRequest("GET", "/v1/usage?bucket=1&since="+since, nil))
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Until.Sub(got.Since) > store.UsageMaxBack+time.Second {
		t.Fatalf("range %v not held to 30 days", got.Until.Sub(got.Since))
	}
	if int(got.Until.Sub(got.Since).Seconds())/got.Bucket > store.UsageMaxBuckets {
		t.Fatalf("bucket %d leaves more than %d buckets", got.Bucket, store.UsageMaxBuckets)
	}

	for _, bad := range []string{"?since=yesterday", "?bucket=0", "?bucket=x"} {
		w = httptest.NewRecorder()
		s.roomUsage(w, httptest.NewRequest("GET", "/v1/usage"+bad, nil))
		if w.Code != 400 {
			t.Fatalf("%s answered %d, want 400", bad, w.Code)
		}
	}
}
