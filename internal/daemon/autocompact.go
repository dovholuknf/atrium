package daemon

import (
	"context"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// A runner older than --autocompact treats it as an unknown option, which is a hard error
// at startup, and the claude row carries the flag on every room. So the runner is asked
// once, `--help` naming the flag, and the answer is kept for the life of the daemon. A runner
// that does not take it is started without it, a log line says so once, and the card
// details say "runner does not take autocompact".

const autocompactFlag = "--autocompact"

// NoAutocompactNote is what the card details say for a runner that does not take the flag.
const NoAutocompactNote = "runner does not take autocompact"

type autocompactProbe struct {
	mu   sync.Mutex
	seen map[string]bool
	// help runs the runner's `--help` and returns what it printed. A variable so a test
	// can stand a fake claude in for it.
	help func(exe string) (string, error)
}

func newAutocompactProbe() *autocompactProbe {
	return &autocompactProbe{seen: map[string]bool{}, help: runHelp}
}

func runHelp(exe string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, exe, "--help").CombinedOutput()
	return string(out), err
}

// takes is whether the runner at exe lists the flag, probed the first time and kept. A
// runner that could not be asked is assumed to take it and is not cached, so a transient
// failure does not turn the flag off: a runner that cannot print `--help` cannot launch either.
func (p *autocompactProbe) takes(exe string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if v, ok := p.seen[exe]; ok {
		return v
	}
	out, err := p.help(exe)
	if err != nil && strings.TrimSpace(out) == "" {
		return true
	}
	v := strings.Contains(out, autocompactFlag)
	p.seen[exe] = v
	if !v {
		log.Printf("[atrium] %s does not list %s, so cards are started without it", exe, autocompactFlag)
	}
	return v
}

// autocompactArgsFor is the row's template when its runner takes the flag, else nil.
func (d *Daemon) autocompactArgsFor(h *store.Harness) []string {
	if h == nil || len(h.AutocompactArgs) == 0 {
		return nil
	}
	if isClaude(h) && !d.acProbe.takes(h.Exe()) {
		return nil
	}
	return h.AutocompactArgs
}

// probeAutocompact asks every enabled runner that has the template, once, at start.
func (d *Daemon) probeAutocompact() {
	hs, err := d.st.Harnesses()
	if err != nil {
		return
	}
	for _, h := range hs {
		if h.Enabled {
			d.autocompactArgsFor(h)
		}
	}
}

// modelWindowK is the context window of a model in thousands of tokens, by the name the card
// was launched with: `[1m]` in it means 1M, any other named model 200k. Zero when no model
// was named, which is unknown: the runner picks its own default and atrium cannot say how
// large its window is.
func modelWindowK(model string) int {
	model = strings.ToLower(strings.TrimSpace(model))
	switch {
	case model == "":
		return 0
	case strings.Contains(model, "[1m]"):
		return 1000
	}
	return 200
}
