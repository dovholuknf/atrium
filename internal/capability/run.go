package capability

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/base/nowindow"
)

// outMax bounds what is read from a probe, while it is read.
const outMax = 64 << 10

type capped struct {
	b bytes.Buffer
}

func (c *capped) Write(p []byte) (int, error) {
	if room := outMax - c.b.Len(); room > 0 {
		c.b.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

// Auditor runs the declared probes and keeps the newest result of each.
type Auditor struct {
	// Scratch is a directory the room made for probes that write, and nothing else is written to.
	Scratch string
	// Repos is where ${ATRIUM_REPOS} points when the environment does not say.
	Repos string
	// File, when set, is where results are kept across a restart.
	File string
	Now  func() time.Time

	mu      sync.Mutex
	list    List
	results map[string]Result
	running map[string]bool
	run     sync.Mutex
}

// NewAuditor starts with list and any results saved in file.
func NewAuditor(list List, scratch, file string) *Auditor {
	a := &Auditor{Scratch: scratch, File: file, Now: time.Now, list: list, results: map[string]Result{}, running: map[string]bool{}}
	if b, err := os.ReadFile(file); err == nil && file != "" {
		var rs []Result
		if json.Unmarshal(b, &rs) == nil {
			for _, r := range rs {
				a.results[r.Cap] = r
			}
		}
	}
	return a
}

// SetList swaps the declared list. Results of capabilities no longer declared are dropped.
func (a *Auditor) SetList(l List) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.list = l
	keep := map[string]bool{}
	for _, c := range l.Caps {
		keep[c.ID] = true
	}
	for id := range a.results {
		if !keep[id] {
			delete(a.results, id)
		}
	}
}

// Snapshot is the declared list and the newest result of each capability, with a running probe shown as such.
type Snapshot struct {
	Version int      `json:"list_ver"`
	Caps    []Cap    `json:"caps"`
	Results []Result `json:"results"`
	Running []string `json:"running,omitempty"`
}

func (a *Auditor) Snapshot() Snapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := Snapshot{Version: a.list.Version, Caps: append([]Cap(nil), a.list.Caps...)}
	for _, c := range a.list.Caps {
		if r, ok := a.results[c.ID]; ok {
			s.Results = append(s.Results, r)
		}
		if a.running[c.ID] {
			s.Running = append(s.Running, c.ID)
		}
	}
	return s
}

// ErrBusy is a run asked for while one is going.
var ErrBusy = errors.New("a capability run is already going")

// Run probes the named capabilities, or all of them when ids is empty, and anything they need first. Ids not in the
// declared list are refused before anything runs.
func (a *Auditor) Run(ctx context.Context, ids []string) ([]Result, error) {
	if !a.run.TryLock() {
		return nil, ErrBusy
	}
	defer a.run.Unlock()
	a.mu.Lock()
	list := a.list
	a.mu.Unlock()
	by := map[string]Cap{}
	for _, c := range list.Caps {
		by[c.ID] = c
	}
	want := map[string]bool{}
	var add func(string)
	add = func(id string) {
		if want[id] {
			return
		}
		want[id] = true
		for _, n := range by[id].Needs {
			add(n)
		}
	}
	for _, id := range ids {
		if _, ok := by[id]; !ok {
			return nil, fmt.Errorf("%s is not a declared capability", id)
		}
		add(id)
	}
	if len(ids) == 0 {
		for _, c := range list.Caps {
			want[c.ID] = true
		}
	}
	a.mu.Lock()
	for id := range want {
		a.running[id] = true
	}
	a.mu.Unlock()

	var out []Result
	done := map[string]Result{}
	for _, c := range list.Caps {
		if !want[c.ID] {
			continue
		}
		var r Result
		if bad := unmet(c, done); bad != "" {
			r = Result{Cap: c.ID, State: Unknown, Reason: "needs " + bad, At: a.stamp()}
		} else {
			r = a.Probe(ctx, c)
		}
		done[c.ID] = r
		out = append(out, r)
		a.mu.Lock()
		a.results[c.ID] = r
		delete(a.running, c.ID)
		a.mu.Unlock()
	}
	a.save()
	return out, nil
}

func unmet(c Cap, done map[string]Result) string {
	for _, n := range c.Needs {
		if done[n].State != Pass {
			return n
		}
	}
	return ""
}

func (a *Auditor) stamp() string { return a.Now().UTC().Format(time.RFC3339) }

func (a *Auditor) save() {
	if a.File == "" {
		return
	}
	s := a.Snapshot()
	b, _ := json.Marshal(s.Results)
	_ = os.MkdirAll(filepath.Dir(a.File), 0o700)
	_ = os.WriteFile(a.File, b, 0o600)
}

func expand(s, scratch, repos string) (string, bool) {
	ok := true
	out := regexp.MustCompile(`\$\{(\w+)\}`).ReplaceAllStringFunc(s, func(m string) string {
		name := m[2 : len(m)-1]
		v := scratch
		if name != "SCRATCH" {
			v = os.Getenv(name)
			if v == "" && name == "ATRIUM_REPOS" {
				v = repos
			}
		}
		if v == "" {
			ok = false
		}
		return v
	})
	return out, ok
}

// Probe runs one capability's probe and judges it. It never returns an error: a probe that cannot run is a result.
func (a *Auditor) Probe(ctx context.Context, c Cap) Result {
	start := a.Now()
	r := Result{Cap: c.ID, At: start.UTC().Format(time.RFC3339)}
	finish := func(state, reason string) Result {
		r.State, r.Reason = state, Scrub(reason)
		r.MS = a.Now().Sub(start).Milliseconds()
		return r
	}
	d, err := c.timeout()
	if err != nil {
		return finish(Fail, err.Error())
	}
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()

	if c.Pass == "http2xx" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Probe[0], nil)
		if err != nil {
			return finish(Fail, err.Error())
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return finish(Fail, err.Error())
		}
		res.Body.Close()
		if res.StatusCode/100 != 2 {
			return finish(Fail, fmt.Sprintf("status %d", res.StatusCode))
		}
		return finish(Pass, fmt.Sprintf("status %d", res.StatusCode))
	}

	argv := make([]string, len(c.Probe))
	for i, p := range c.Probe {
		v, ok := expand(p, a.Scratch, a.Repos)
		if !ok {
			return finish(Unknown, "probe names a path this room has not set")
		}
		argv[i] = v
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	if c.Dir != "" {
		dir, ok := expand(c.Dir, a.Scratch, a.Repos)
		if !ok {
			return finish(Unknown, "the repository root for this probe is not set (ATRIUM_REPOS)")
		}
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			return finish(Unknown, "not cloned on this room")
		}
		cmd.Dir = dir
	}
	if a.Scratch != "" {
		_ = os.MkdirAll(a.Scratch, 0o700)
	}
	var out capped
	cmd.Stdout, cmd.Stderr = &out, &out
	cmd.Stdin = nil
	nowindow.Hide(cmd)
	err = cmd.Run()
	text := out.b.String()
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return finish(Fail, "timeout")
	case errors.Is(err, exec.ErrNotFound):
		return finish(Fail, argv[0]+" not on PATH")
	case err != nil:
		return finish(Fail, firstLine(text, err.Error()))
	}
	if c.Match != "" && !regexp.MustCompile(c.Match).MatchString(strings.TrimSpace(text)) {
		return finish(Fail, "output did not match: "+firstLine(text, ""))
	}
	if c.Version != "" {
		if m := regexp.MustCompile(c.Version).FindStringSubmatch(text); len(m) > 1 {
			r.Version = Scrub(m[1])
		} else if len(m) == 1 {
			r.Version = Scrub(m[0])
		}
	}
	return finish(Pass, firstLine(text, "ok"))
}

func firstLine(text, fallback string) string {
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return fallback
}

// Due is the capabilities whose newest result is older than their own cadence (every, else sixHours).
func (a *Auditor) Due() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var ids []string
	now := a.Now()
	for _, c := range a.list.Caps {
		every := 6 * time.Hour
		if d, err := time.ParseDuration(c.Every); err == nil && d > 0 {
			every = d
		}
		r, ok := a.results[c.ID]
		at, _ := time.Parse(time.RFC3339, r.At)
		if !ok || now.Sub(at) >= every {
			ids = append(ids, c.ID)
		}
	}
	return ids
}
