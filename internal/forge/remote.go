package forge

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// A room attached to a hub never runs a forge CLI and never fetches from a forge host. It asks the hub, which runs
// the forge, fetches the head into its own store, and answers in the shapes below. See docs/rnd/scm-forge-design.md.

// The hub's forge routes, on the link's git kind.
const (
	HubPRPath    = "/_forge/pr"
	HubIssuePath = "/_forge/issue"
	HubRepoPath  = "/_forge/repo"
)

// HubAsk names one pull request, issue or repository. Diff asks for the diff too, and Fetch asks the hub to fetch the
// head into its store first.
type HubAsk struct {
	Host   string `json:"host"`
	Org    string `json:"org"`
	Repo   string `json:"repo"`
	Number int    `json:"number,omitempty"`
	Diff   bool   `json:"diff,omitempty"`
	Fetch  bool   `json:"fetch,omitempty"`
	// HeadOnly is the head check: the hub answers PR.Head and nothing else is asked of the forge.
	HeadOnly bool `json:"head_only,omitempty"`
}

// HubPR is the hub's answer about a pull request. Store is the repository's name in the hub's store and Ref the ref
// the head is under there, both set when Fetch was asked.
type HubPR struct {
	Kind  string `json:"kind"`
	PR    *PR    `json:"pr"`
	Diff  []byte `json:"diff,omitempty"`
	URL   string `json:"url"`
	Store string `json:"store,omitempty"`
	Ref   string `json:"ref,omitempty"`
}

// HubIssue is the hub's answer about an issue.
type HubIssue struct {
	Kind  string `json:"kind"`
	Issue *Issue `json:"issue"`
}

// HubRepo is the hub's answer to a repository it was asked to hold: its name in the store.
type HubRepo struct {
	Store string `json:"store"`
}

// The codes of a HubError.
const (
	CodeNoForge = "no_forge"
	CodeAccess  = "access"
	CodeFetch   = "fetch"
	CodeOther   = "other"
)

// HubError is a refusal from the hub's forge route. The message is the hub's sentence, which names the hub, so a room
// shows it as it is and raises nothing of its own: the hub raised the alert.
type HubError struct {
	Message string `json:"error"`
	Code    string `json:"code"`
	Tool    string `json:"tool,omitempty"`
	Host    string `json:"host,omitempty"`
}

func (e *HubError) Error() string { return e.Message }

// PRRef is where the hub keeps a pull request's head in its store, and where a room fetches it to.
func PRRef(n int) string { return "refs/atrium/pr/" + strconv.Itoa(n) }

// StoreName is the hub store's name for a repository: `<host>/<org>/<repo>` with github.com spelled `github`.
func StoreName(host, org, repo string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" || host == "github.com" {
		host = "github"
	}
	return host + "/" + org + "/" + repo
}

// Remote is the Forge of a room attached to a hub: every call is a request to the hub over the link. Call posts in
// to the path and decodes the answer into out, and a refusal comes back as a *HubError.
type Remote struct {
	Call func(ctx context.Context, path string, in, out any) error

	mu   sync.Mutex
	kind string
	seen map[Ref]HubPR
}

// NewRemote is a Remote over call.
func NewRemote(call func(ctx context.Context, path string, in, out any) error) *Remote {
	return &Remote{Call: call, seen: map[Ref]HubPR{}}
}

// Hub is the kind a Remote names before the hub has said which forge it ran.
const Hub = "hub"

func (r *Remote) Kind() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.kind == "" {
		return Hub
	}
	return r.kind
}

func ask(ref Ref) HubAsk {
	return HubAsk{Host: ref.Host, Org: ref.Org, Repo: ref.Repo, Number: ref.Number}
}

func (r *Remote) pr(ctx context.Context, ref Ref, in HubAsk) (HubPR, error) {
	var out HubPR
	if err := r.Call(ctx, HubPRPath, in, &out); err != nil {
		return out, err
	}
	if out.PR == nil {
		return out, fmt.Errorf("the hub answered nothing about pull request %d", ref.Number)
	}
	r.mu.Lock()
	if out.Kind != "" {
		r.kind = out.Kind
	}
	if !in.HeadOnly {
		r.seen[ref] = out
	}
	r.mu.Unlock()
	return out, nil
}

// View asks the hub to read the pull request and fetch its head into the hub's store, so FetchSpec names a ref
// that is there.
func (r *Remote) View(ctx context.Context, ref Ref) (*PR, error) {
	in := ask(ref)
	in.Fetch = true
	out, err := r.pr(ctx, ref, in)
	if err != nil {
		return nil, err
	}
	return out.PR, nil
}

func (r *Remote) Diff(ctx context.Context, ref Ref) ([]byte, error) {
	in := ask(ref)
	in.Diff = true
	out, err := r.pr(ctx, ref, in)
	if err != nil {
		return nil, err
	}
	return out.Diff, nil
}

func (r *Remote) Head(ctx context.Context, ref Ref) (string, error) {
	in := ask(ref)
	in.HeadOnly = true
	out, err := r.pr(ctx, ref, in)
	if err != nil {
		return "", err
	}
	return out.PR.Head, nil
}

// Issue asks the hub to read an issue.
func (r *Remote) Issue(ctx context.Context, ref Ref) (*Issue, error) {
	var out HubIssue
	if err := r.Call(ctx, HubIssuePath, ask(ref), &out); err != nil {
		return nil, err
	}
	if out.Issue == nil {
		return nil, fmt.Errorf("the hub answered nothing about issue %d", ref.Number)
	}
	return out.Issue, nil
}

// Repo asks the hub to hold the repository in its store, cloning it from the forge when it does not, and answers its
// name there.
func (r *Remote) Repo(ctx context.Context, host, org, repo string) (string, error) {
	var out HubRepo
	if err := r.Call(ctx, HubRepoPath, HubAsk{Host: host, Org: org, Repo: repo}, &out); err != nil {
		return "", err
	}
	if out.Store == "" {
		return "", fmt.Errorf("the hub did not say where it holds %s/%s", org, repo)
	}
	return out.Store, nil
}

// FetchSpec is the head in the hub's store. It runs nothing.
func (r *Remote) FetchSpec(ref Ref) FetchSpec {
	r.mu.Lock()
	seen, ok := r.seen[ref]
	r.mu.Unlock()
	if ok && seen.Store != "" && seen.Ref != "" {
		return FetchSpec{Hub: seen.Store, Refspec: seen.Ref}
	}
	return FetchSpec{Hub: StoreName(ref.Host, ref.Org, ref.Repo), Refspec: PRRef(ref.Number)}
}

// PRURL is the URL the hub gave, else the built-in shape of the host. It runs nothing.
func (r *Remote) PRURL(ref Ref) string {
	r.mu.Lock()
	seen, ok := r.seen[ref]
	r.mu.Unlock()
	if ok && seen.URL != "" {
		return seen.URL
	}
	if DefaultHosts[strings.ToLower(ref.Host)] == Bitbucket {
		return (&bitbucket{}).PRURL(ref)
	}
	return (&github{}).PRURL(ref)
}
