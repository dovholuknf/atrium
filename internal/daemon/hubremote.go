package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/gitsync"
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
type hubGit struct {
	// cards is the token table. TWO THINGS ARE REPLACED BY THE SECURITY DESIGN'S 2c: this and nothing else. See
	// gitsync.CardAuth.
	cards gitsync.CardAuth

	mu sync.Mutex
	// issued is the token each card was launched with, IN THE CLEAR, so the room can push for a card that asked it to
	// (atrium_git_push) with the same token. This is the one place the room holds a usable token: the forwarder's
	// table (gitsync.CardTokens) keeps only a hash. Memory only, and dropped with the card.
	issued    map[string]issuedToken
	transport func() (http.RoundTripper, error)
	// agent is `127.0.0.1:<port>` of the agent listener, set when it is bound.
	agent string
	// stashing holds the cards a close is pushing a stash for, which may push though their session ended. See
	// closestash.go.
	stashing sync.Map
}

// issuedToken is a card's token and when it was minted. A runner's exit revokes only a token minted no later than the
// runner started, so a resume's new token is not taken away by the old run's late exit.
type issuedToken struct {
	tok string
	at  time.Time
}

// SetHubGit gives the forwarder its way to the hub: the room's git transport, or an error saying why there is none
// right now. Called once the link is built, as SetRelay is.
func (d *Daemon) SetHubGit(transport func() (http.RoundTripper, error)) {
	d.hubGit.mu.Lock()
	d.hubGit.transport = transport
	d.hubGit.mu.Unlock()
}

// hubRemote is the handler mounted on the agent listener at gitsync.HubRemotePrefix.
func (d *Daemon) hubRemote() http.Handler {
	return &gitsync.HubForwarder{
		Auth: d.hubGit.cards,
		Push: d.st.GitPush,
		Transport: func() (http.RoundTripper, error) {
			d.hubGit.mu.Lock()
			t := d.hubGit.transport
			d.hubGit.mu.Unlock()
			if t == nil {
				return nil, errNoHub
			}
			return t()
		},
	}
}

type hubErr string

func (e hubErr) Error() string { return string(e) }

const errNoHub = hubErr("this room is not attached to a hub")

// cardLive says whether a card may still use its git token: it exists and has not ended. A card that finished,
// was shelved or died loses the token even before anything revokes it.
func (d *Daemon) cardLive(id string) bool {
	if _, ok := d.hubGit.stashing.Load(id); ok {
		return true
	}
	t, err := d.st.Get(id)
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
func (d *Daemon) setAgentAddr(a net.Addr) {
	host, port, err := net.SplitHostPort(a.String())
	if err != nil {
		return
	}
	// Only a wildcard bind is reached at 127.0.0.1, as StableHubURL does. A listener bound to a specific address is
	// not listening on loopback, and that address is where it is.
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	d.hubGit.mu.Lock()
	d.hubGit.agent = net.JoinHostPort(host, port)
	d.hubGit.mu.Unlock()
}

// agentAddr is where the agent listener is: the bound address once it is listening (so a port of 0 is the port that
// was given), else the configured one. gitsync.StableHubURL makes a host:port or ":port" or "0.0.0.0:port" into one
// a client can reach.
func (d *Daemon) agentAddr() string {
	d.hubGit.mu.Lock()
	agent := d.hubGit.agent
	d.hubGit.mu.Unlock()
	if agent == "" {
		agent = strings.TrimSpace(d.opts.AgentAddr)
	}
	return agent
}

// HubRemoteURL is the stable URL of a repository on this room's hub forwarder, with the host in its canonical
// spelling (`github`, not `github.com`). The one helper the scm clone and the sync of a room's clones both use.
func (d *Daemon) HubRemoteURL(name string) string {
	ref, err := gitsync.ParseName(strings.Trim(name, "/"))
	if err != nil || d.agentAddr() == "" {
		return ""
	}
	return gitsync.StableHubURL(d.agentAddr(), ref)
}

// HubRemoteBase is `http://127.0.0.1:<agent port>/git/`, what a remote's push URL has to start with to be the
// forwarder's. Empty until the agent listener is bound.
func (d *Daemon) HubRemoteBase() string {
	a := d.agentAddr()
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
func (d *Daemon) hubGitEnv(card string, outside bool, harness map[string]string, into map[string]string) {
	if outside || d.hubGit.cards == nil {
		return
	}
	base := d.HubRemoteBase()
	if base == "" {
		return
	}
	tok, err := d.hubGit.cards.Mint(card)
	if err != nil {
		return
	}
	d.hubGit.mu.Lock()
	if d.hubGit.issued == nil {
		d.hubGit.issued = map[string]issuedToken{}
	}
	d.hubGit.issued[card] = issuedToken{tok: tok, at: time.Now()}
	d.hubGit.mu.Unlock()
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
func isOutsideCode(req LaunchRequest, t *store.Task) bool {
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
func (d *Daemon) revokeHubGit(card string, since time.Time) {
	d.hubGit.mu.Lock()
	defer d.hubGit.mu.Unlock()
	if it, ok := d.hubGit.issued[card]; ok && !since.IsZero() && it.at.After(since) {
		return
	}
	if d.hubGit.cards != nil {
		d.hubGit.cards.Revoke(card)
	}
	delete(d.hubGit.issued, card)
}

// GitPush is atrium_git_push: the push `git push hub <branch>` is, run by the room in the card's own directory
// with no shell, under the same rules. A plain branch only. The clone's hub remote has to push to this room's
// forwarder after every rewrite (gitsync.PushToHub), the room's git.push has to be `hub`, and a card that runs
// outside code, or whose token this room does not hold (it was launched before the room last started), is refused.
func (d *Daemon) GitPush(taskID, branch string) (any, error) {
	t, err := d.st.Get(taskID)
	if err != nil || t == nil {
		return nil, fmt.Errorf("no card %s on this room", taskID)
	}
	if !d.cardLive(t.ID) {
		return nil, fmt.Errorf("that card is not running, so it pushes nothing")
	}
	if d.st.GitPush() != store.GitPushHub {
		return nil, fmt.Errorf("this room does not let cards push to the hub (git.push is none)")
	}
	if isOutsideCode(LaunchRequest{}, t) {
		return nil, fmt.Errorf("this card runs code that is not the room's own, so it does not push")
	}
	base := d.HubRemoteBase()
	d.hubGit.mu.Lock()
	tok := d.hubGit.issued[t.ID].tok
	d.hubGit.mu.Unlock()
	if base == "" || tok == "" {
		return nil, fmt.Errorf("this room holds no git token for that card. it was launched before the room last started, or without one. relaunch it")
	}
	if strings.TrimSpace(t.Worktree) == "" {
		return nil, fmt.Errorf("that card has no directory to push from")
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitsync.CommandBound)
	defer cancel()
	dir := filepath.FromSlash(t.Worktree)
	report, err := gitsync.PushToHub(ctx, gitsync.Default, dir, base, tok, strings.TrimSpace(branch),
		gitsync.HubNameOf(ctx, gitsync.Default, dir, d.cloneRoots()...))
	if err != nil {
		// git's own words, which carry the hub's `remote: atrium: ...` sentence.
		if strings.TrimSpace(report) != "" {
			return nil, fmt.Errorf("the push did not land:\n%s", report)
		}
		return nil, err
	}
	return map[string]any{"card": t.ID, "branch": strings.TrimSpace(branch), "report": report}, nil
}

// cloneRoots are the folders a clone made by hand is laid out under as `<host>/<owner>/<repo>`: the scm folder and
// the git root synced clones go in (~/git when unset). A clone with no hub remote is named by its place under one.
func (d *Daemon) cloneRoots() []string {
	var out []string
	for _, k := range []string{gitsync.SettingSCMRoot, gitsync.SettingGitRoot} {
		if v, err := d.st.Setting(k); err == nil && strings.TrimSpace(v) != "" {
			out = append(out, strings.TrimSpace(v))
		}
	}
	return append(out, gitsync.DefaultRoot())
}

// handleHubGitURL is GET /v1/hub/git/url: the lookup atrium_git_url gives, asked of the hub over the link
// (gitsync.AskHubLookup, which reads the hub's store alone when the hub is older). The URLs are on the link's base:
// the tool puts a card's on this room's forwarder.
func (d *Daemon) handleHubGitURL(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if strings.TrimSpace(q.Get("repo")) == "" {
		writeJSONErr(w, http.StatusBadRequest, fmt.Errorf("say which repository"))
		return
	}
	rt, err := d.hubTransport()
	if err != nil {
		writeJSONErr(w, http.StatusBadGateway, err)
		return
	}
	ans, err := gitsync.AskHubLookup(r.Context(), rt, gitsync.URLQuery{Repo: q.Get("repo"), Branch: q.Get("branch"),
		Room: q.Get("room")})
	if err != nil {
		writeJSONErr(w, http.StatusBadGateway, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ans)
}
