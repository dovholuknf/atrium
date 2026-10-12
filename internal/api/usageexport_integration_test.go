//go:build integration

package api

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

func TestUsageExportIsOneRowPerLine(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	base := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Second)
	for i, in := range []int64{1, 2, 3} {
		end := base.Add(time.Duration(i) * 24 * time.Hour)
		if err := st.AddSessionUsage(&store.SessionUsage{TaskID: task.ID, Started: end, Ended: end,
			Cause: store.UsageOperator, Model: "m", Input: in}); err != nil {
			t.Fatal(err)
		}
	}

	since := base.Add(12 * time.Hour).Format(time.RFC3339)
	w := httptest.NewRecorder()
	s.usageExport(w, httptest.NewRequest("GET", "/v1/usage/export?since="+since, nil))
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/x-ndjson" {
		t.Fatalf("answered %d, %q", w.Code, w.Header().Get("Content-Type"))
	}
	var got []store.SessionUsage
	sc := bufio.NewScanner(w.Body)
	for sc.Scan() {
		var u store.SessionUsage
		if err := json.Unmarshal(sc.Bytes(), &u); err != nil {
			t.Fatalf("line %q: %v", sc.Text(), err)
		}
		got = append(got, u)
	}
	if len(got) != 2 || got[0].Input != 2 || got[1].Input != 3 {
		t.Fatalf("rows %+v", got)
	}

	// No since is the last seven days, which holds all three, and nothing in range is an empty body.
	w = httptest.NewRecorder()
	s.usageExport(w, httptest.NewRequest("GET", "/v1/usage/export", nil))
	if n := len(bytesLines(w.Body.Bytes())); n != 3 {
		t.Fatalf("default window gave %d lines", n)
	}
	w = httptest.NewRecorder()
	s.usageExport(w, httptest.NewRequest("GET", "/v1/usage/export?since="+time.Now().Add(time.Hour).UTC().Format(time.RFC3339), nil))
	if w.Code != http.StatusOK || w.Body.Len() != 0 {
		t.Fatalf("answered %d with %q", w.Code, w.Body.String())
	}
}

func bytesLines(b []byte) [][]byte {
	var out [][]byte
	for _, l := range splitLines(b) {
		if len(l) > 0 {
			out = append(out, l)
		}
	}
	return out
}

func splitLines(b []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, c := range b {
		if c == '\n' {
			out = append(out, b[start:i])
			start = i + 1
		}
	}
	return append(out, b[start:])
}

func TestUsageExportBadSinceIs400(t *testing.T) {
	t.Parallel()
	s, _, _ := fileServer(t)
	w := httptest.NewRecorder()
	s.usageExport(w, httptest.NewRequest("GET", "/v1/usage/export?since=yesterday", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("answered %d", w.Code)
	}
}
