//go:build integration

package link

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/store"
)

func seededRoom(t *testing.T, n int) (*api.Server, string) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "atrium.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	prompt := strings.Repeat("x", 7500)
	var first string
	for i := 0; i < n; i++ {
		c, _, err := st.Register(store.Observed{WireName: "w" + strconv.Itoa(i), Worktree: "/tmp/w" + strconv.Itoa(i), Runner: "claude"})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.SetLaunchCommand(c.ID, store.LaunchCmd{Exe: "claude", Args: []string{"--append-system-prompt", prompt}}); err != nil {
			t.Fatal(err)
		}
		if first == "" {
			first = c.ID
		}
	}
	return api.New(st), first
}

// b-hub-tasks-4mb-slow: launch_cmd was 74% of a 4.1 MB hub list, so the list drops it and GET one keeps it.
func TestHubTaskListLeavesOutTheLaunchCommand(t *testing.T) {
	const per = 290
	alpha, id := seededRoom(t, per)
	beta, _ := seededRoom(t, per)
	front, _, done := two(t, alpha.Handler(), beta.Handler())
	defer done()

	start := time.Now()
	res, err := http.Get(front.URL + "/v1/tasks")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	took := time.Since(start)
	var body struct {
		Tasks []map[string]any `json:"tasks"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	t.Logf("hub GET /v1/tasks, %d cards: %d bytes in %v", len(body.Tasks), len(raw), took)
	if len(body.Tasks) != 2*per {
		t.Fatalf("%d cards, want %d", len(body.Tasks), 2*per)
	}
	if _, has := body.Tasks[0]["launch_cmd"]; has {
		t.Fatal("the list carries launch_cmd")
	}
	if max := 2 * per * 2048; len(raw) > max {
		t.Fatalf("the list is %d bytes, want at most %d", len(raw), max)
	}

	res, err = http.Get(front.URL + "/v1/tasks/" + tagFor("alpha", id))
	if err != nil {
		t.Fatal(err)
	}
	var one map[string]any
	_ = json.NewDecoder(res.Body).Decode(&one)
	res.Body.Close()
	if _, has := one["launch_cmd"]; !has {
		t.Fatalf("the one card lost launch_cmd: %v", one["id"])
	}
}
