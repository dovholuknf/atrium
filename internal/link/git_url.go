package link

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The lookup a card calls for the URL to fetch code that is not in its cwd, as the board API and as a control tool.
// The answer is built in internal/gitsync/lookup.go, and both doors here call it. See
// docs/rnd/hub-forge-design.md section 4.
//
//	GET /_hub/git/url?repo=<host/owner/repo | owner/repo | name>[&branch=<b>][&room=<r>]
//
// WHO MAY ASK is who may fetch: the reaches of the pass-through and the store (gitReach), so loopback on the hub's
// machine, the overlay and a zrok private share, and a zrok public share gets the 404 a path that is not there gets.
// A card reaches it through the control tool, which is loopback on the hub's machine, and which gives a card the
// hub's URLs on its own room's forwarder (gitURLHandler).
//
// THE URLS ARE BUILT ON THE HOST THE CALLER REACHED THE HUB BY (the request's Host, which the listener has already
// checked is a name the hub answers to), never on a name made up here. Nothing in the query is given to git: a
// repository is looked up in the list the hub knows, and a branch is compared with the names that were listed.

// hostForURL is a Host header that is fit to be put in a URL: a hostname or an address, and a port.
var hostForURL = regexp.MustCompile(`^(\[[0-9A-Fa-f:.]+\]|[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?)(:[0-9]{1,5})?$`)

// serveGitURL answers GET /_hub/git/url.
func (p *Proxy) serveGitURL(w http.ResponseWriter, r *http.Request, g *gitsync.Hub, fail func(int, string)) {
	if _, ok := gitReach(w, r); !ok {
		return
	}
	if r.Method != http.MethodGet {
		fail(http.StatusMethodNotAllowed, "that has to be a GET")
		return
	}
	q := r.URL.Query()
	repo := strings.TrimSpace(q.Get("repo"))
	if repo == "" {
		fail(http.StatusBadRequest, "say which repository: repo=<owner>/<repo>, <host>/<owner>/<repo> or its name")
		return
	}
	if !hostForURL.MatchString(r.Host) {
		fail(http.StatusBadRequest, "the hub cannot tell what name you reached it by")
		return
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	ans := g.Lookup(r.Context(), gitsync.URLQuery{
		Repo: repo, Branch: q.Get("branch"), Room: q.Get("room"), Base: scheme + "://" + r.Host,
	})
	_ = json.NewEncoder(w).Encode(ans)
}

const gitURLToolDesc = "Get the URL to fetch code that is not in your cwd. Call this, then `git fetch <url> <branch>`. " +
	"NEVER ASK FOR A PASTE.\n\n" +
	"`repo` is `<owner>/<repo>`, `<host>/<owner>/<repo>` or just the repository's name. `branch` is the branch " +
	"you want, and without it you get the branches the hub knows. `room` asks one room and no other.\n\n" +
	"Each branch answers where it can be fetched from: `hub` (finished work, pushed) or `room` (work in " +
	"progress on an attached room, passed through to you, with the room's `online`). A branch that is both " +
	"answers both, with each sha, and `ahead` says the room has commits the hub does not. The `state` is " +
	"`found`, `not found` (with the closest repositories and branches) or `offline` (the room that has it " +
	"is not connected, so say so and do not retry).\n\n" +
	"The URL is on your own room's forwarder, which carries your card's token, so `git fetch` it from your " +
	"checkout as it stands. A room's work in progress (`room`) has no URL for a card yet: the answer says so, " +
	"and the way to it is to ask the card on that room to `atrium_git_push` the branch, then ask again. " +
	"Nothing is copied: the hub's store is read from the hub."

type gitURLInput struct {
	Repo   string `json:"repo" jsonschema:"the repository: <owner>/<repo>, <host>/<owner>/<repo> or its name"`
	Branch string `json:"branch,omitempty" jsonschema:"the branch you want. empty lists the branches the hub knows"`
	Room   string `json:"room,omitempty" jsonschema:"ask this one room only, as named on the hub"`
}

// gitURLOutput is the endpoint's JSON and a line to read.
type gitURLOutput struct {
	gitsync.URLAnswer
	Text string `json:"text"`
}

// forwarderBase is what a room said its hub forwarder is, if it is one: `http://<loopback address>:<port>/git/` and
// nothing else, so a room cannot send a card's fetch to another host. It is "" otherwise.
func forwarderBase(base string) string {
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/git/" {
		return ""
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); (ip == nil || !ip.IsLoopback()) && host != "localhost" {
		return ""
	}
	if p, err := strconv.Atoi(u.Port()); err != nil || p < 1 || p > 65535 {
		return ""
	}
	return base
}

func (c *controlMCP) gitURLHandler(ctx context.Context, req *mcp.CallToolRequest, in gitURLInput) (
	*mcp.CallToolResult, gitURLOutput, error) {
	return c.gitDoor().URL(ctx, req, in)
}
