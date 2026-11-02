package home

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/small"
	"github.com/dovholuknf/atrium/internal/store"
)

// The room's stable hub remote, docs/fabric/hub-forge-design.md 5.1 and 5.3. The forwarder itself is
// gitsync.HubRemote; this file is the room's side of it: which cards hold a token, the environment that carries
// it into a card's git, and the link it forwards over.

// OutsideCodeTag marks a card that runs code that is not the room's own: a PR's tests, `prove`, any card whose cwd
// is an outside target. Such a card is launched with NO git token in its environment, so code it runs cannot read
// a token and push as that card. A tag, so it survives a reopen.
const OutsideCodeTag = "atrium:outside-code"

// hubGit is the daemon's state for the hub remote.
type HubGit struct {
	// cards is the token table. TWO THINGS ARE REPLACED BY THE SECURITY DESIGN'S 2c: this and nothing else. See
	// gitsync.CardAuth.
	Cards gitsync.CardAuth

	Mu sync.Mutex
	// issued is the token each card was launched with, IN THE CLEAR, so the room can push for a card that asked it to
	// (atrium_git_push) with the same token. This is the one place the room holds a usable token: the forwarder's
	// table (gitsync.CardTokens) keeps only a hash. Memory only, and dropped with the card.
	Issued    map[string]IssuedToken
	Transport func() (http.RoundTripper, error)
	// agent is `127.0.0.1:<port>` of the agent listener, set when it is bound.
	Agent string
	// stashing holds the cards a close is pushing a stash for, which may push though their session ended. See
	// closestash.go.
	Stashing sync.Map
}

// issuedToken is a card's token and when it was minted. A runner's exit revokes only a token minted no later than the
// runner started, so a resume's new token is not taken away by the old run's late exit.
type IssuedToken struct {
	Tok string
	At  time.Time
}

// SetHubGit gives the forwarder its way to the hub: the room's git transport, or an error saying why there is none
// right now. Called once the link is built, as SetRelay is.
func (d *Service) SetHubGit(transport func() (http.RoundTripper, error)) {
	d.HubGit.Mu.Lock()
	d.HubGit.Transport = transport
	d.HubGit.Mu.Unlock()
}

// hubRemote is the handler mounted on the agent listener at gitsync.HubRemotePrefix.
func (d *Service) HubRemote() http.Handler {
	return &gitsync.HubForwarder{
		Auth: d.HubGit.Cards,
		Push: d.St.GitPush,
		Kin:  d.PushKin,
		Transport: func() (http.RoundTripper, error) {
			d.HubGit.Mu.Lock()
			t := d.HubGit.Transport
			d.HubGit.Mu.Unlock()
			if t == nil {
				return nil, ErrNoHub
			}
			return t()
		},
	}
}

// pushKin is the branch a card was launched on and who handed it over: its launcher, then each earlier card the
// launcher launched on that branch, newest first, at most gitsync.MaxChain in all. "" when the card was not launched on
// a branch or has no launcher, so a lookup that fails sends nothing and the hub's own rule stands.
func (d *Service) PushKin(card string) (string, []string) {
	t, err := d.St.Get(card)
	if err != nil || t.LaunchBranch == "" || t.SpawnedByID == "" {
		return "", nil
	}
	kin := []string{t.SpawnedByID}
	ids, err := d.St.WorkerIDs(t.SpawnedByID)
	if err != nil {
		return t.LaunchBranch, kin
	}
	var earlier []*store.Task
	for _, id := range ids {
		if id == card {
			continue
		}
		if o, err := d.St.Get(id); err == nil && o.LaunchBranch == t.LaunchBranch && o.CreatedAt.Before(t.CreatedAt) {
			earlier = append(earlier, o)
		}
	}
	sort.Slice(earlier, func(i, j int) bool { return earlier[i].CreatedAt.After(earlier[j].CreatedAt) })
	for _, o := range earlier {
		if len(kin) >= gitsync.MaxChain {
			break
		}
		kin = append(kin, o.ID)
	}
	return t.LaunchBranch, kin
}

type HubErr string

func (e HubErr) Error() string { return string(e) }

const ErrNoHub = HubErr("this room is not attached to a hub")

// cardLive says whether a card may still use its git token: it exists and has not ended. A card that finished,
// was paused or died loses the token even before anything revokes it.
func (d *Service) CardLive(id string) bool {
	if _, ok := d.HubGit.Stashing.Load(id); ok {
		return true
	}
	// A done card woken by a say keeps that status until it acts, and its first fetch comes before that.
	if d.HasSession(id) {
		return true
	}
	t, err := d.St.Get(id)
	if err != nil || t == nil {
		return false
	}
	switch t.Status {
	case store.StatusRunning, store.StatusNeedsInput, store.StatusNeedsPermission:
		return true
	}
	return false
}

// setAgentAddr records where the agent listener really is, from the bound listener, so a port of 0 is the port
// that was given.
func (d *Service) SetAgentAddr(a net.Addr) {
	host, port, err := net.SplitHostPort(a.String())
	if err != nil {
		return
	}
	// Only a wildcard bind is reached at 127.0.0.1, as StableHubURL does. A listener bound to a specific address is
	// not listening on loopback, and that address is where it is.
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	d.HubGit.Mu.Lock()
	d.HubGit.Agent = net.JoinHostPort(host, port)
	d.HubGit.Mu.Unlock()
}

// agentAddr is where the agent listener is: the bound address once it is listening (so a port of 0 is the port that
// was given), else the configured one. gitsync.StableHubURL makes a host:port or ":port" or "0.0.0.0:port" into one
// a client can reach.
func (d *Service) AgentAddr() string {
	d.HubGit.Mu.Lock()
	agent := d.HubGit.Agent
	d.HubGit.Mu.Unlock()
	if agent == "" {
		agent = strings.TrimSpace(d.OptAgentAddr())
	}
	return agent
}

// HubRemoteURL is the stable URL of a repository on this room's hub forwarder, with the host in its canonical
// spelling (`github`, not `github.com`). The one helper the scm clone and the sync of a room's clones both use.
func (d *Service) HubRemoteURL(name string) string {
	ref, err := gitsync.ParseName(strings.Trim(name, "/"))
	if err != nil || d.AgentAddr() == "" {
		return ""
	}
	return gitsync.StableHubURL(d.AgentAddr(), ref)
}

// HubRemoteBase is `http://127.0.0.1:<agent port>/git/`, what a remote's push URL has to start with to be the
// forwarder's. Empty until the agent listener is bound.
func (d *Service) HubRemoteBase() string {
	a := d.AgentAddr()
	if a == "" {
		return ""
	}
	// Through StableHubURL, so the base is spelled as the remote URL is: `http://127.0.0.1:<port>/git/`.
	u := gitsync.StableHubURL(a, gitsync.Ref{})
	return u[:strings.Index(u, "/git/")+len("/git/")]
}

// hubGitEnv is what a launch adds to a card's environment so ITS git can use the forwarder: three GIT_CONFIG
// entries, `http.http://127.0.0.1:<agent port>/git/.extraHeader` = the card's token header.
//
//   - SCOPED TO THE FORWARDER'S URL, never a bare http.extraHeader. A bare one goes to every http remote, so a
//     fetch from origin, a dependency or a submodule would send the token to github.com.
//   - In the environment and nowhere else: not in .git/config, not in argv, not in a log.
//   - `outside` is a card that runs code that is not the room's own. It gets nothing, so no token to read.
//
// harness is the env the card is launched with, whose own GIT_CONFIG_COUNT is continued rather than replaced.
func (d *Service) HubGitEnv(card string, outside bool, harness map[string]string, into map[string]string) {
	if outside || d.HubGit.Cards == nil {
		return
	}
	base := d.HubRemoteBase()
	if base == "" {
		return
	}
	tok, err := d.HubGit.Cards.Mint(card)
	if err != nil {
		return
	}
	d.HubGit.Mu.Lock()
	if d.HubGit.Issued == nil {
		d.HubGit.Issued = map[string]IssuedToken{}
	}
	now := time.Now()
	d.HubGit.Issued[card] = IssuedToken{Tok: tok, At: now}
	d.HubGit.Mu.Unlock()
	if d.St != nil {
		_, hash, ok := gitsync.TokenHash(tok)
		if ok {
			if err := d.St.SaveGitToken(card, hash, now); err != nil {
				log.Printf("atrium: could not keep the git token for %s: %v", card, err)
			}
		}
	}
	n := 0
	for k, v := range harness {
		if strings.EqualFold(k, "GIT_CONFIG_COUNT") {
			if c, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && c > 0 {
				n = c
			}
		}
	}
	i := strconv.Itoa(n)
	into["GIT_CONFIG_COUNT"] = strconv.Itoa(n + 1)
	into["GIT_CONFIG_KEY_"+i] = "http." + base + ".extraHeader"
	into["GIT_CONFIG_VALUE_"+i] = gitsync.HeaderCardToken + ": " + tok
}

// isOutsideCode says whether a launch is of a card that runs code that is not the room's own.
func IsOutsideCode(req shared.LaunchRequest, t *store.Task) bool {
	if req.OutsideCode {
		return true
	}
	for _, tag := range req.Tags {
		if tag == OutsideCodeTag {
			return true
		}
	}
	if t != nil {
		for _, tag := range t.Tags {
			if tag == OutsideCodeTag {
				return true
			}
		}
	}
	return false
}

// revokeHubGit takes a card's git token away: the forwarder stops accepting it, and the room forgets it. `since` is
// when the run that ended started: a token minted after that belongs to a newer run of the same card (a resume
// mints before it registers the new runner) and is left alone. A zero `since` revokes whatever there is.
func (d *Service) RevokeHubGit(card string, since time.Time) {
	d.HubGit.Mu.Lock()
	defer d.HubGit.Mu.Unlock()
	if it, ok := d.HubGit.Issued[card]; ok && !since.IsZero() && it.At.After(since) {
		return
	}
	if d.HubGit.Cards != nil {
		d.HubGit.Cards.Revoke(card)
	}
	delete(d.HubGit.Issued, card)
	if d.St != nil {
		d.St.DeleteGitToken(card)
	}
}

// restoreHubGit puts back the tokens cards were launched with, so a runner the pty host kept through a room restart can
// still fetch. Only the hash is kept, so `issued` (the clear token atrium_git_push sends) is not restored: GitPush for
// such a card is refused until it is relaunched. A token whose card is gone is dropped.
func (d *Service) RestoreHubGit() {
	ct, ok := d.HubGit.Cards.(*gitsync.CardTokens)
	if !ok || d.St == nil {
		return
	}
	toks, err := d.St.GitTokens()
	if err != nil {
		log.Printf("atrium: could not read the kept git tokens: %v", err)
		return
	}
	for _, g := range toks {
		if t, err := d.St.Get(g.Card); err != nil || t == nil || !ct.RestoreHash(g.Card, g.Hash) {
			d.St.DeleteGitToken(g.Card)
		}
	}
}

// GitPush is atrium_git_push: the push `git push hub <branch>` is, run by the room in the card's own directory
// with no shell, under the same rules. A plain branch only. The clone's hub remote has to push to this room's
// forwarder after every rewrite (gitsync.PushToHub), the room's git.push has to be `hub`, and a card that runs
// outside code is refused. The push uses the room's own token for the card, so a runner kept through a restart can ask for it.
func (d *Service) GitPush(taskID, branch string) (any, error) {
	t, err := d.St.Get(taskID)
	if err != nil || t == nil {
		return nil, fmt.Errorf("no card %s on this room", taskID)
	}
	// atrium_done ends the work, not the process: a resident runner between tasks still pushes.
	if !d.CardLive(t.ID) && !(t.PID > 0 && shared.ProcessAlive(t.PID)) {
		return nil, fmt.Errorf("that card is not running, so it pushes nothing")
	}
	if d.St.GitPush() != store.GitPushHub {
		return nil, fmt.Errorf("this room does not let cards push to the hub (git.push is none)")
	}
	if IsOutsideCode(shared.LaunchRequest{}, t) {
		return nil, fmt.Errorf("this card runs code that is not the room's own, so it does not push")
	}
	base := d.HubRemoteBase()
	ct, _ := d.HubGit.Cards.(*gitsync.CardTokens)
	if base == "" || ct == nil {
		return nil, fmt.Errorf("this room has no hub remote to push to")
	}
	tok, err := ct.RoomToken(t.ID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(t.Worktree) == "" {
		return nil, fmt.Errorf("that card has no directory to push from")
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitsync.CommandBound)
	defer cancel()
	dir := filepath.FromSlash(t.Worktree)
	hubName := gitsync.HubNameOf(ctx, gitsync.Default, dir, d.CloneRoots()...)
	// A hub remote that is another room's forwarder on this machine, in a repository the launcher shares: the commit
	// is on the launcher's disk, but the hub does not hold it, and a worker on another room fetches from the hub. So
	// it is pushed by URL to THIS room's forwarder, the shared config untouched, and the launcher's view is a note.
	note := ""
	other := gitsync.PointsAtOtherForwarder(ctx, gitsync.Default, dir, base)
	if other {
		by := t.SpawnedBy
		if by == "" {
			by = d.St.ReportTo(t.ID)
		}
		if l := d.VisibleLauncher(ctx, t, t.SpawnedByID, by, t.SpawnedByDir, dir); l != nil {
			note = l.WireName + " sees this commit already on disk"
		}
		if hubName == "" {
			return nil, fmt.Errorf("this clone's hub remote is another room's forwarder, and atrium cannot tell which hub repository it is to push to this room's: it has no origin with a forge URL and is not at <scm folder>/<host>/<owner>/<repo>")
		}
	}
	var report string
	if other {
		report, err = gitsync.PushToHubByURL(ctx, gitsync.Default, dir, base, tok, strings.TrimSpace(branch), hubName)
	} else {
		report, err = gitsync.PushToHub(ctx, gitsync.Default, dir, base, tok, strings.TrimSpace(branch), hubName)
	}
	if err == nil {
		d.RecordHubBranch(t.ID, strings.TrimSpace(branch), hubName)
	}
	if err != nil {
		// git's own words, which carry the hub's `remote: atrium: ...` sentence.
		if strings.TrimSpace(report) != "" {
			return nil, fmt.Errorf("the push did not land:\n%s", report)
		}
		return nil, err
	}
	if note != "" {
		report = strings.TrimSpace(report + "\n" + note)
	}
	return map[string]any{"card": t.ID, "branch": strings.TrimSpace(branch), "report": report}, nil
}

// cloneRoots are the folders a clone made by hand is laid out under as `<host>/<owner>/<repo>`: the scm folder and
// the git root synced clones go in (~/git when unset). A clone with no hub remote is named by its place under one.
func (d *Service) CloneRoots() []string {
	var out []string
	for _, k := range []string{gitsync.SettingSCMRoot, gitsync.SettingGitRoot} {
		if v, err := d.St.Setting(k); err == nil && strings.TrimSpace(v) != "" {
			out = append(out, strings.TrimSpace(v))
		}
	}
	return append(out, gitsync.DefaultRoot())
}

// handleHubGitURL is GET /v1/hub/git/url: the lookup atrium_git_url gives, asked of the hub over the link
// (gitsync.AskHubLookup, which reads the hub's store alone when the hub is older). The URLs are on the link's base:
// the tool puts a card's on this room's forwarder.
func (d *Service) HandleHubGitURL(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if strings.TrimSpace(q.Get("repo")) == "" {
		small.WriteJSONErr(w, http.StatusBadRequest, fmt.Errorf("say which repository"))
		return
	}
	rt, err := d.HubTransport()
	if err != nil {
		small.WriteJSONErr(w, http.StatusBadGateway, err)
		return
	}
	ans, err := gitsync.AskHubLookup(r.Context(), rt, gitsync.URLQuery{Repo: q.Get("repo"), Branch: q.Get("branch"),
		Room: q.Get("room")})
	if err != nil {
		small.WriteJSONErr(w, http.StatusBadGateway, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ans)
}
