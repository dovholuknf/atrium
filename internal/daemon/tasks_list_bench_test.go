//go:build integration

package daemon

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// BenchmarkListTasks is one GET /v1/tasks on a board of a hundred cards, half of them with a keep-alive switch, which
// is what every board tab and every hub poll pays.
func BenchmarkListTasks(b *testing.B) {
	d := testDaemon(b)
	for i := 0; i < 100; i++ {
		task, _, err := d.st.Register(store.Observed{
			WireName: fmt.Sprintf("bench-%d", i), Worktree: "/tmp/atrium-bench", Runner: "claude",
		})
		if err != nil {
			b.Fatal(err)
		}
		if i%2 == 0 {
			if _, err := d.st.SetKeepaliveState(task.ID, store.KeepaliveStates[0]); err != nil {
				b.Fatal(err)
			}
		}
	}
	h := d.ap.Handler()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/tasks", nil))
		if rec.Code != http.StatusOK {
			b.Fatalf("answered %d", rec.Code)
		}
	}
}
