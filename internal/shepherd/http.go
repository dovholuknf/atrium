package shepherd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/dovholuknf/atrium/internal/nowindow"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

// The real edges: the hub over its loopback board, git and the atrium binary over exec, the disk over os. Each is the
// least that satisfies its interface, and the tests of the machine use fakes instead.

// HubHTTP is the Store and the Hub over the board address `atrium run` serves.
type HubHTTP struct {
	// Board is host:port.
	Board string
	// Room is this machine's room, which runs the worker, for CardStatus.
	Room   string
	Client *http.Client
}

func (h *HubHTTP) client() *http.Client {
	if h.Client != nil {
		return h.Client
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (h *HubHTTP) call(ctx context.Context, method, path string, body, out any) (int, error) {
	var rdr io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://"+h.Board+path, rdr)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := h.client().Do(req)
	if err != nil {
		return 0, fmt.Errorf("the hub is not answering at %s (is it running?): %w", h.Board, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		if e.Error == "" {
			e.Error = res.Status
		}
		return res.StatusCode, fmt.Errorf("%s", e.Error)
	}
	if out != nil {
		return res.StatusCode, json.Unmarshal(raw, out)
	}
	return res.StatusCode, nil
}

// Load reads the row. A 404 is a shepherd that has not started.
func (h *HubHTTP) Load(ctx context.Context, item string) (State, bool, error) {
	var st State
	code, err := h.call(ctx, http.MethodGet, "/_hub/shepherds/"+url.PathEscape(item), nil, &st)
	if code == http.StatusNotFound {
		return State{}, false, nil
	}
	return st, err == nil, err
}

// Save writes the row whole.
func (h *HubHTTP) Save(ctx context.Context, st State) error {
	_, err := h.call(ctx, http.MethodPost, "/_hub/shepherds/"+url.PathEscape(st.ItemID), st, nil)
	return err
}

// Backlog reads one backlog item.
func (h *HubHTTP) Backlog(ctx context.Context, id string) (BacklogItem, error) {
	var it BacklogItem
	_, err := h.call(ctx, http.MethodGet, "/_hub/backlog/"+url.PathEscape(id), nil, &it)
	return it, err
}

// ItemForCard is the newest item whose card is this one. The list is newest first.
func (h *HubHTTP) ItemForCard(ctx context.Context, card string) (WorkItem, bool, error) {
	var out struct {
		Items []WorkItem `json:"items"`
	}
	if _, err := h.call(ctx, http.MethodGet, "/_hub/items", nil, &out); err != nil {
		return WorkItem{}, false, err
	}
	for _, it := range out.Items {
		if it.Card == card {
			return it, true, nil
		}
	}
	return WorkItem{}, false, nil
}

// CardStatus reads the card from this machine's room through the hub's relay.
func (h *HubHTTP) CardStatus(ctx context.Context, card string) (string, error) {
	var t struct {
		Status string `json:"status"`
	}
	_, err := h.call(ctx, http.MethodGet, "/v1/tasks/"+url.PathEscape(card), nil, &t)
	return t.Status, err
}

// ExecRunner runs commands for real, with each stream bounded while it is read.
type ExecRunner struct{}

const outMax = 1 << 20

type capped struct {
	b bytes.Buffer
}

func (c *capped) Write(p []byte) (int, error) {
	if room := outMax - c.b.Len(); room > 0 {
		if len(p) > room {
			c.b.Write(p[:room])
		} else {
			c.b.Write(p)
		}
	}
	return len(p), nil
}

// Run implements Runner.
func (ExecRunner) Run(ctx context.Context, dir, name string, args ...string) (Result, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	nowindow.Hide(cmd)
	cmd.Dir = dir
	var so, se capped
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	r := Result{Stdout: so.b.String(), Stderr: se.b.String()}
	if err == nil {
		return r, nil
	}
	if ee, ok := err.(*exec.ExitError); ok {
		r.Code = ee.ExitCode()
		return r, nil
	}
	return r, err
}

// DiskFiles is Files over os.
type DiskFiles struct{}

// Read implements Files.
func (DiskFiles) Read(p string) ([]byte, error) { return os.ReadFile(p) }

// Write implements Files.
func (DiskFiles) Write(p string, b []byte) error { return os.WriteFile(p, b, 0o644) }

// SleepCtx waits d or until the context ends.
func SleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// DefaultRules is the block every worker's brief ends with. It is the recipe's, so the two do not drift.
var DefaultRules = strings.TrimSpace(`
RULES: commit as Clint Dovholuk (configured), one short subject line per commit, no body, no trailers. Do not edit
CLAUDE.md. Go builds go to build.claude/ (go build -o build.claude/...). gofmt -l internal/ cmd/ must be empty; go run
scripts/check-test-globals.go exit 0 (tests may not assign package vars). Unit tests stay under the 10ms budget (disk,
socket or store tests go in *_integration_test.go with //go:build integration). Add a changelog entry and
docs/changes/<id>.md. Run scripts/test-touched.sh in ONE foreground call, timeout up to 600000ms, never
background/sleep/poll (a 20ms unit-budget miss on a disk test you did not touch under load is not yours: rerun once and
say so). Known failing tests are in docs/known-failing-tests.md. Never restart the live hub or rooms, never deploy, do
not merge or push. Finish with atrium_done giving the commit sha.
`)
