package daemon

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/daemon/pulls"
	"github.com/dovholuknf/atrium/internal/store"
)

// prConfig is what the pulls runner reads from the daemon: its forks, its lean command line and environment, and the
// board alert hooks.
func (d *Daemon) prConfig() pulls.Config {
	return pulls.Config{
		Fork: func(ctx context.Context, spec pulls.ForkSpec) ([]byte, error) {
			return runForkProcess(ctx, forkSpec(spec), d.hk.agentFork)
		},
		UserSettings: func() []byte { return d.hk.readUserSettings() },
		LeanArgs: func(args []string, userSettings []byte, gateway string) ([]string, error) {
			return leanArgs(args, userSettings, "", nil, leanKit{}, gateway, os.ReadFile, &d.hk)
		},
		ForkEnv: func(extra, harnessEnv map[string]string) {
			leanEnv(extra)
			if _, set := harnessEnv[keepaliveTTLVar]; !set {
				extra[keepaliveTTLVar] = "1h"
			}
		},
		ChildEnv: childEnvFrom,
		Cost:     prCost,
		OnAccess: d.ForgeAccessFrom, OnWorked: d.ForgeWorked,
		Hub: d.HubForge, HubSource: d.hubSource,
	}
}

func prPrice(rec *forkReceipt) keepalivePrice {
	for m := range rec.ModelUsage {
		if p, ok := usagePriceFor(m); ok {
			return p
		}
	}
	p, _ := usagePriceFor("claude-sonnet-5-5")
	return p
}

// prCost prices one fork from its receipt, 0 when the receipt has no usage.
func prCost(raw []byte) float64 {
	var rec forkReceipt
	if json.Unmarshal(raw, &rec) != nil || rec.Usage == nil {
		return 0
	}
	return receiptCost(&rec, prPrice(&rec))
}

// changesFor answers the endpoint for one card.
func (d *Daemon) changesFor(ctx context.Context, taskID, against string, turn time.Time) (any, error) {
	return pulls.Changes{Get: d.st.Get, Transcript: d.cardTranscript, ListMax: d.hk.listMax}.For(ctx, taskID, against, turn)
}

// cardTranscript is the transcript a card's replies are read from, or "" when atrium
// cannot read one (a runner that is not Claude, or no file). The same choice /replies makes.
func (d *Daemon) cardTranscript(t *store.Task) string {
	if d.usage == nil || !d.usage.isClaude(t.Runner) {
		return ""
	}
	session := strings.TrimSpace(t.ResumeID)
	if d.ctx != nil {
		session = d.ctx.sessionOf(t)
	}
	if session == "" {
		return ""
	}
	return d.usage.transcript(t.Worktree, session)
}

// fillEdited sets each reply's `edited`: how many files the edit calls of its turn named
// inside the worktree. From the transcript alone, no git. A failure leaves the counts 0.
func fillEdited(t *store.Task, transcript string, replies []Reply) {
	if len(replies) == 0 {
		return
	}
	x, err := pulls.TurnsOf(transcript)
	if err != nil {
		return
	}
	byTurn := map[time.Time]int{}
	for i := range replies {
		start, end := x.Span(replies[i].At)
		n, ok := byTurn[start]
		if !ok {
			rels, _ := pulls.InsideWorktreeVia(t.Worktree, x.EditsIn(start, end), pulls.ResolveInsideMemo)
			n = len(rels)
			byTurn[start] = n
		}
		replies[i].Edited = n
	}
}
