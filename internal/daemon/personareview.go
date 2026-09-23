package daemon

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/persona"
	"github.com/dovholuknf/atrium/internal/store"
)

// PersonaTagPrefix marks a card as a persona's review: `persona:<id>`.
const PersonaTagPrefix = "persona:"

// personaRunsDir is where persona run directories go, beside the database,
// the one directory atrium already owns.
func (d *Daemon) personaRunsDir() string {
	return filepath.Join(filepath.Dir(d.opts.DBPath), persona.RunsDirName)
}

// PersonaReview starts a persona reviewing one card's diff.
//
// A FRESH SESSION IN A RUN DIRECTORY, through the ordinary launch path. The
// run directory holds the persona's rendered file and a TARGET.md naming the
// card's worktree, its diff range and its repo key, so the session never has
// the pack as its working directory. See docs/personas-design.md, "The run
// directory".
//
// CLAUDE IS STARTED AS THE PERSONA, not told about it. `claude --agent <name>`
// makes a subagent file the session's own agent, and a project agent in the
// run directory's .claude/agents is found ahead of the user's. That was
// measured against Claude Code 2.1.280 before this was written: a probe agent
// whose body said to answer one word answered that word, and the same prompt
// without the flag did not.
//
// THE REVIEWED CARD IS THE LAUNCHER. spawned_by names its session, so the
// review's atrium_report lands on the card whose work it read, through the
// a2a path every launched session reports on. A card with no session name
// falls back to the board.
func (d *Daemon) PersonaReview(taskID, personaID, runner string) (*store.Task, error) {
	if i := strings.IndexByte(taskID, '~'); i > 0 {
		taskID = taskID[i+1:]
	}
	pack, err := d.st.Setting(store.SettingPersonaPackPath)
	if err != nil {
		return nil, err
	}
	pack = strings.TrimSpace(pack)
	if pack == "" {
		return nil, errors.New("no persona pack is configured. set " + store.SettingPersonaPackPath)
	}
	card, err := d.st.Get(taskID)
	if err != nil {
		return nil, fmt.Errorf("no card %s: %w", taskID, err)
	}
	p, err := persona.Find(pack, personaID)
	if err != nil {
		return nil, err
	}
	runner = strings.ToLower(strings.TrimSpace(runner))
	if !p.RendersFor(runner) {
		return nil, fmt.Errorf("%s does not render for %s", p.ID, runner)
	}
	h, err := d.personaHarness(runner)
	if err != nil {
		return nil, err
	}
	tgt, err := persona.ResolveTarget(card.Worktree, persona.RepoHint{
		Host: card.Host, Org: card.Org, Repo: card.Repo,
	})
	if err != nil {
		return nil, err
	}
	run, err := persona.NewRun(d.personaRunsDir(), pack, p, runner, tgt, time.Now())
	if err != nil {
		return nil, err
	}

	by := strings.TrimSpace(card.WireName)
	if by == "" {
		by = store.HumanLauncher
	}
	return d.Launch(LaunchRequest{
		Harness:    h.ID,
		Cwd:        run.Dir,
		Title:      p.Name + ": " + card.DisplayTitle(),
		Why:        "reviewing " + tgt.Range + " in " + tgt.Worktree,
		Tags:       []string{OriginAgentTag, PersonaTagPrefix + p.ID},
		Prompt:     run.Prompt,
		SpawnedBy:  by,
		RunnerArgs: run.Args,
	})
}

// personaHarness is the runner row a persona review starts on: the enabled
// row for that runner, preferring the one whose id is the runner's name.
func (d *Daemon) personaHarness(runner string) (*store.Harness, error) {
	if runner != "claude" {
		return nil, fmt.Errorf("atrium launches personas on claude only. %s is not measured yet", runner)
	}
	all, err := d.st.Harnesses()
	if err != nil {
		return nil, err
	}
	var pick *store.Harness
	for _, h := range all {
		if !h.Enabled || !isClaude(h) {
			continue
		}
		if strings.EqualFold(h.ID, runner) {
			return h, nil
		}
		if pick == nil {
			pick = h
		}
	}
	if pick == nil {
		return nil, errors.New("no claude runner is enabled on this machine")
	}
	return pick, nil
}
