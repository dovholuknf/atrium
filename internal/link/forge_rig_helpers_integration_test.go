//go:build integration

package link

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/dovholuknf/atrium/internal/base/rules"
	forgepkg "github.com/dovholuknf/atrium/internal/link/forge"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

type ciRig struct {
	st     *hubstore.Store
	hook   *CIHook
	fake   *fakeCI
	secret string
	clock  *time.Time
	mu     sync.Mutex
	lines  []string
}

func newCIRig(t *testing.T) *ciRig {
	t.Helper()
	st, err := hubstore.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	x := &ciRig{st: st, fake: &fakeCI{}}
	start := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	x.clock = &start
	now := func() time.Time { return *x.clock }
	st.SetClock(now)
	x.hook = NewCIHook(st, x.fake, func(kind, detail string) {
		x.mu.Lock()
		x.lines = append(x.lines, kind+": "+detail)
		x.mu.Unlock()
	})
	x.hook.Now = now
	if x.secret, err = st.MintCIHookSecret(false); err != nil {
		t.Fatal(err)
	}
	return x
}

func ciBody(repo string, id int64, attempt int, workflow, branch, conclusion string) string {
	return fmt.Sprintf(`{"action":"completed","workflow_run":{"id":%d,"run_attempt":%d,"name":%q,`+
		`"path":".github/workflows/ci.yml","head_branch":%q,"head_sha":"0123456789abcdef","event":"push",`+
		`"conclusion":%q,"html_url":"https://github.com/%s/actions/runs/%d"},"repository":{"full_name":%q}}`,
		id, attempt, workflow, branch, conclusion, repo, id, repo)
}

type eventRig struct {
	st    *hubstore.Store
	hook  *EventHook
	fake  *fakeCI
	clock *time.Time
	mu    sync.Mutex
	lines []string
}

func newEventRig(t *testing.T) *eventRig {
	t.Helper()
	st, err := hubstore.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	x := &eventRig{st: st, fake: &fakeCI{}}
	start := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	x.clock = &start
	now := func() time.Time { return *x.clock }
	st.SetClock(now)
	x.hook = forgepkg.NewEventHook(st, x.fake, func(kind, detail string) {
		x.mu.Lock()
		x.lines = append(x.lines, kind+": "+detail)
		x.mu.Unlock()
	})
	x.hook.Now = now
	x.hook.From = "land@testhub"
	return x
}

func landBodyJSON(outcome, from, to string, commits int, reason string) string {
	b, _ := json.Marshal(map[string]any{"kind": "land", "repo": "o/r", "branch": "main", "outcome": outcome, "from": from,
		"to": to, "commits": commits, "reason": reason})
	return string(b)
}

const landFrom, landTo = "3f1c0de0123456789abcdef0123456789abcdef", "9a0b1c20123456789abcdef0123456789abcdef"

// seedOf is a seed that gives exactly `rows`.
func seedOf(rows ...rules.Recogniser) forgepkg.SeedFunc {
	return func() ([]rules.Recogniser, error) { return append([]rules.Recogniser(nil), rows...), nil }
}

func seedRow(id string, rank int, pattern string) rules.Recogniser {
	return rules.Recogniser{ID: id, Label: id, Enabled: true, Rank: rank, Pattern: pattern, FetchArgs: []string{}}
}

func rowIDs(rows []*rules.Recogniser) string {
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	return strings.Join(ids, ",")
}

type fakeCI struct {
	mu      sync.Mutex
	resolve map[string]func() (CITarget, CIAnswer)
	send    func(t CITarget) CIAnswer
	sent    []ciSend
}

type ciSend struct {
	Card, From, Text, Kind string
	Wake                   bool
}

func (f *fakeCI) Resolve(_ context.Context, to string) (CITarget, CIAnswer) {
	f.mu.Lock()
	r := f.resolve[to]
	f.mu.Unlock()
	if r == nil {
		return CITarget{}, CIAnswer{State: hubstore.CIPending, Detail: "no session called " + to}
	}
	return r()
}

func (f *fakeCI) Send(_ context.Context, t CITarget, from, text, kind string, wake bool) CIAnswer {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, ciSend{t.Card, from, text, kind, wake})
	if f.send != nil {
		return f.send(t)
	}
	return CIAnswer{State: hubstore.CIDelivered, Detail: "queued"}
}
