package forge

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/dovholuknf/atrium/internal/gitsync"
)

// The lookup a card calls for the URL to fetch code that is not in its cwd, as the board API and as a control tool.
// The answer is built in internal/gitsync/lookup.go, and both doors here call it. See
// docs/fabric/hub-forge-design.md section 4.
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
func (p *Service) ServeGitURL(w http.ResponseWriter, r *http.Request, g *gitsync.Hub, fail func(int, string)) {
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
