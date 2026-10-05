package forge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	bbTimeout   = 2 * time.Minute
	bbJSONLimit = 4 << 20
	bbDiffLimit = 8 << 20
)

// bitbucket is the Bitbucket Cloud forge, through `bb` (datlechin/bitbucket-cli). It reads the REST API 2.0
// through `bb api`, so the CLI owns the login and atrium never sees a token.
type bitbucket struct {
	cmd string
	run Runner

	mu    sync.Mutex
	specs map[Ref]FetchSpec
}

func newBitbucket(cmd string, run Runner) *bitbucket {
	if cmd == "" {
		cmd = "bb"
	}
	return &bitbucket{cmd: cmd, run: run, specs: map[Ref]FetchSpec{}}
}

func (b *bitbucket) Kind() string { return Bitbucket }

func bbHost(ref Ref) string {
	if ref.Host == "" {
		return "bitbucket.org"
	}
	return ref.Host
}

// prPath is the REST path of the pull request, without the host.
func prPath(ref Ref) string {
	return fmt.Sprintf("/repositories/%s/%s/pullrequests/%d", url.PathEscape(ref.Org), url.PathEscape(ref.Repo),
		ref.Number)
}

func (b *bitbucket) do(ctx context.Context, ref Ref, limit int, args ...string) ([]byte, error) {
	out, err := b.run(ctx, Cmd{Name: b.cmd, Args: args, Timeout: bbTimeout, Limit: limit})
	return out, bbAccess(b.cmd, bbHost(ref), err)
}

// bbAccess is access for bb: its own login command and its own not-logged-in wording.
func bbAccess(tool, host string, err error) error {
	if err == nil {
		return nil
	}
	login := tool + " auth login"
	if errors.Is(err, exec.ErrNotFound) {
		return &AccessError{Tool: tool, Host: host, Detail: err.Error(), NotInstalled: true, Login: login}
	}
	low := strings.ToLower(err.Error())
	for _, s := range []string{"auth login", "auth setup", "not logged in", "not authenticated", "authentication required",
		"401", "unauthorized", "no credentials"} {
		if strings.Contains(low, s) {
			return &AccessError{Tool: tool, Host: host, Detail: err.Error(), Login: login}
		}
	}
	return err
}

type bbRepo struct {
	FullName string `json:"full_name"`
}

type bbEnd struct {
	Branch struct {
		Name string `json:"name"`
	} `json:"branch"`
	Commit struct {
		Hash string `json:"hash"`
	} `json:"commit"`
	Repository *bbRepo `json:"repository"`
}

type bbPR struct {
	Title  string `json:"title"`
	Author struct {
		Nickname    string `json:"nickname"`
		DisplayName string `json:"display_name"`
	} `json:"author"`
	Source      bbEnd `json:"source"`
	Destination bbEnd `json:"destination"`
}

type bbDiffstat struct {
	Values []struct {
		LinesAdded   int `json:"lines_added"`
		LinesRemoved int `json:"lines_removed"`
		New          *struct {
			Path string `json:"path"`
		} `json:"new"`
		Old *struct {
			Path string `json:"path"`
		} `json:"old"`
	} `json:"values"`
}

func (b *bitbucket) getPR(ctx context.Context, ref Ref) (*bbPR, error) {
	out, err := b.do(ctx, ref, bbJSONLimit, "api", prPath(ref), "-o", "json")
	if err != nil {
		return nil, err
	}
	var v bbPR
	if err := json.Unmarshal(out, &v); err != nil {
		return nil, fmt.Errorf("%s api printed something that is not json: %w", b.cmd, err)
	}
	if v.Source.Commit.Hash == "" {
		return nil, fmt.Errorf("%s api gave no head", b.cmd)
	}
	return &v, nil
}

// fromFork is true when the source repository is not the destination, or is gone.
func (v *bbPR) fromFork() bool {
	return v.Source.Repository == nil || !strings.EqualFold(v.Source.Repository.FullName, v.Destination.Repository.FullName)
}

func (b *bitbucket) View(ctx context.Context, ref Ref) (*PR, error) {
	v, err := b.getPR(ctx, ref)
	if err != nil {
		return nil, err
	}
	if v.Destination.Repository == nil {
		v.Destination.Repository = &bbRepo{FullName: ref.Org + "/" + ref.Repo}
	}
	out, err := b.do(ctx, ref, bbJSONLimit, "api", prPath(ref)+"/diffstat?pagelen=100", "-o", "json")
	if err != nil {
		return nil, err
	}
	var ds bbDiffstat
	if err := json.Unmarshal(out, &ds); err != nil {
		return nil, fmt.Errorf("%s api printed something that is not json: %w", b.cmd, err)
	}
	files := make([]File, 0, len(ds.Values))
	for _, f := range ds.Values {
		p := ""
		switch {
		case f.New != nil && f.New.Path != "":
			p = f.New.Path
		case f.Old != nil:
			p = f.Old.Path
		}
		files = append(files, File{Path: p, Additions: f.LinesAdded, Deletions: f.LinesRemoved})
	}
	author := v.Author.Nickname
	if author == "" {
		author = v.Author.DisplayName
	}
	pr := &PR{Title: v.Title, Author: author, Head: v.Source.Commit.Hash, HeadRef: v.Source.Branch.Name,
		BaseRef: v.Destination.Branch.Name, FromFork: v.fromFork(), Files: files}

	// Bitbucket has no pull/N/head ref. FetchSpec runs nothing, so what View learned is kept for it.
	spec := FetchSpec{Remote: bbClone(ref, ref.Org+"/"+ref.Repo), Refspec: v.Source.Branch.Name}
	if v.fromFork() && v.Source.Repository != nil {
		spec.Remote = bbClone(ref, v.Source.Repository.FullName)
	}
	b.mu.Lock()
	b.specs[ref] = spec
	b.mu.Unlock()
	return pr, nil
}

func (b *bitbucket) Diff(ctx context.Context, ref Ref) ([]byte, error) {
	return b.do(ctx, ref, bbDiffLimit, "api", prPath(ref)+"/diff")
}

func (b *bitbucket) Head(ctx context.Context, ref Ref) (string, error) {
	v, err := b.getPR(ctx, ref)
	if err != nil {
		return "", err
	}
	return v.Source.Commit.Hash, nil
}

func bbClone(ref Ref, fullName string) string {
	return fmt.Sprintf("https://%s/%s.git", bbHost(ref), fullName)
}

// FetchSpec is the source branch of the pull request, from the fork when there is one. It needs a View of the
// same ref first, since the branch and the fork are only in what Bitbucket says. Without one it names the
// destination repository and no refspec, which a caller must not fetch.
func (b *bitbucket) FetchSpec(ref Ref) FetchSpec {
	b.mu.Lock()
	defer b.mu.Unlock()
	if s, ok := b.specs[ref]; ok {
		return s
	}
	return FetchSpec{Remote: bbClone(ref, ref.Org+"/"+ref.Repo)}
}

func (b *bitbucket) PRURL(ref Ref) string {
	return fmt.Sprintf("https://%s/%s/%s/pull-requests/%d", bbHost(ref), ref.Org, ref.Repo, ref.Number)
}
