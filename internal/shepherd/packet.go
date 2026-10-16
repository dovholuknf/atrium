package shepherd

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
)

// Packet is the evidence a hold shows, so the decision to accept is a person reading about a page. It holds no
// model's opinion: judging that the diff does what the item asked is the reading that stays with whoever accepts.
type Packet struct {
	Item  string `json:"item"`
	Title string `json:"title"`
	// Base is main when the worker started, Main is main now, Land is the staging branch's head.
	Base    string   `json:"base"`
	Main    string   `json:"main"`
	Land    string   `json:"land"`
	Branch  string   `json:"branch"`
	Commits []string `json:"commits"`
	// Diffstat is what the land branch changes against main.
	Diffstat string `json:"diffstat"`
	// Symbols is how many places on main already name each symbol the item's body quotes in backticks. A symbol the
	// item asks to be built that main already has is the worker's "done" on work that was already there.
	Symbols map[string]int `json:"symbols,omitempty"`
	Checks  []Check        `json:"checks"`
}

var quoted = regexp.MustCompile("`([A-Za-z_][A-Za-z0-9_.]{3,40})`")

// symbolsIn is the identifiers an item body quotes, in order, at most eight.
func symbolsIn(body string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range quoted.FindAllStringSubmatch(body, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
		if len(out) == 8 {
			break
		}
	}
	return out
}

// holdStep posts the packet to the row and stops. Accepting, rejecting and correcting are the verbs of
// rnd-shepherd-accept-land, and nothing in this build can reach a land from here.
type holdStep struct{}

func (holdStep) Phase() Phase { return PhaseHold }

func (holdStep) Run(ctx context.Context, e *Env, st *State) (Outcome, error) {
	main := e.Cfg.mainBranch()
	it, err := e.Hub.Backlog(ctx, st.ItemID)
	if err != nil {
		return Outcome{}, err
	}
	p := Packet{Item: st.ItemID, Title: it.Title, Base: st.BaseSHA, Branch: st.Branch, Symbols: map[string]int{}}
	_ = json.Unmarshal([]byte(st.Packet), &p.Checks)
	if p.Main, err = git(ctx, e, e.Cfg.Repo, "rev-parse", main); err != nil {
		return Outcome{}, err
	}
	if p.Land, err = git(ctx, e, st.Stage, "rev-parse", "HEAD"); err != nil {
		return Outcome{}, err
	}
	log, err := git(ctx, e, st.Stage, "log", "--oneline", main+"..HEAD")
	if err != nil {
		return Outcome{}, err
	}
	for _, l := range strings.Split(log, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			p.Commits = append(p.Commits, l)
		}
	}
	if p.Diffstat, err = git(ctx, e, st.Stage, "diff", "--stat", main+"...HEAD"); err != nil {
		return Outcome{}, err
	}
	for _, sym := range symbolsIn(it.Body) {
		// A grep that finds nothing exits 1, which is an answer of zero and not a failure.
		r, err := e.Runner.Run(ctx, e.Cfg.Repo, "git", "grep", "-c", "-F", sym, main, "--", "internal")
		if err != nil {
			continue
		}
		n := 0
		for _, l := range strings.Split(strings.TrimSpace(r.Stdout), "\n") {
			if l != "" {
				n++
			}
		}
		p.Symbols[sym] = n
	}
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return Outcome{}, err
	}
	st.Packet = string(raw)
	e.say("held: %s on %s at %s\n%s", st.ItemID, st.Branch, p.Land, st.Packet)
	return stop(st, PhaseHeld, ""), nil
}
