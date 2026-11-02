//go:build integration

package daemon

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/base/testwait"
	"github.com/dovholuknf/atrium/internal/store"
)

func TestPublishTaskSendsTheDecoratedRow(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	card, _, err := d.st.Register(store.Observed{WireName: "pub", Worktree: "/tmp/pub", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.st.AddAsk(card.ID, "one?", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := d.st.AddAsk(card.ID, "two?", ""); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(d.ap.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	stop := make(chan struct{})
	defer close(stop)
	// The stream subscribes after it answers, so publish until the subscriber hears one.
	go func() {
		for {
			d.Publish(card.ID)
			select {
			case <-stop:
				return
			case <-time.After(testwait.HTTP):
			}
		}
	}()

	sc := bufio.NewScanner(resp.Body)
	kind := ""
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "event: ") {
			kind = strings.TrimPrefix(line, "event: ")
		}
		if strings.HasPrefix(line, "data: ") && kind == "task" {
			if !strings.Contains(line, `"asks_open":2`) || !strings.Contains(line, `"display_title"`) {
				t.Fatalf("event is the raw task, not the list row: %s", line)
			}
			return
		}
	}
	t.Fatal("no task event")
}
