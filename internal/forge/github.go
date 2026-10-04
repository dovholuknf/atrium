package forge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	ghTimeout   = 2 * time.Minute
	ghJSONLimit = 4 << 20
	ghDiffLimit = 8 << 20
)

// github is the GitHub forge, through `gh`.
type github struct {
	cmd string
	run Runner
}

func newGitHub(cmd string, run Runner) *github {
	if cmd == "" {
		cmd = "gh"
	}
	return &github{cmd: cmd, run: run}
}

func (g *github) Kind() string { return GitHub }

// repoArg is gh's --repo value. A host other than github.com is part of it.
func repoArg(ref Ref) string {
	if ref.Host == "" || strings.EqualFold(ref.Host, "github.com") {
		return ref.Org + "/" + ref.Repo
	}
	return ref.Host + "/" + ref.Org + "/" + ref.Repo
}

func host(ref Ref) string {
	if ref.Host == "" {
		return "github.com"
	}
	return ref.Host
}

func (g *github) do(ctx context.Context, ref Ref, limit int, args ...string) ([]byte, error) {
	out, err := g.run(ctx, Cmd{Name: g.cmd, Args: args, Timeout: ghTimeout, Limit: limit})
	return out, access(g.cmd, host(ref), err)
}

type ghView struct {
	Title  string `json:"title"`
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	HeadRefOid        string `json:"headRefOid"`
	HeadRefName       string `json:"headRefName"`
	BaseRefName       string `json:"baseRefName"`
	IsCrossRepository bool   `json:"isCrossRepository"`
	Files             []File `json:"files"`
}

func (g *github) View(ctx context.Context, ref Ref) (*PR, error) {
	out, err := g.do(ctx, ref, ghJSONLimit, "pr", "view", fmt.Sprint(ref.Number), "--repo", repoArg(ref), "--json",
		"title,author,headRefOid,headRefName,baseRefName,isCrossRepository,files")
	if err != nil {
		return nil, err
	}
	var v ghView
	if err := json.Unmarshal(out, &v); err != nil {
		return nil, fmt.Errorf("%s pr view printed something that is not json: %w", g.cmd, err)
	}
	if v.HeadRefOid == "" {
		return nil, fmt.Errorf("%s pr view gave no head", g.cmd)
	}
	return &PR{Title: v.Title, Author: v.Author.Login, Head: v.HeadRefOid, HeadRef: v.HeadRefName,
		BaseRef: v.BaseRefName, FromFork: v.IsCrossRepository, Files: v.Files}, nil
}

func (g *github) Diff(ctx context.Context, ref Ref) ([]byte, error) {
	return g.do(ctx, ref, ghDiffLimit, "pr", "diff", fmt.Sprint(ref.Number), "--repo", repoArg(ref))
}

func (g *github) Head(ctx context.Context, ref Ref) (string, error) {
	out, err := g.do(ctx, ref, ghJSONLimit, "pr", "view", fmt.Sprint(ref.Number), "--repo", repoArg(ref),
		"--json", "headRefOid")
	if err != nil {
		return "", err
	}
	var v struct {
		HeadRefOid string `json:"headRefOid"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return "", fmt.Errorf("%s pr view printed something that is not json: %w", g.cmd, err)
	}
	if v.HeadRefOid == "" {
		return "", errors.New(g.cmd + " pr view gave no head")
	}
	return v.HeadRefOid, nil
}

func (g *github) FetchSpec(ref Ref) FetchSpec {
	return FetchSpec{
		Remote:  fmt.Sprintf("https://%s/%s/%s.git", host(ref), ref.Org, ref.Repo),
		Refspec: fmt.Sprintf("pull/%d/head", ref.Number),
	}
}

func (g *github) PRURL(ref Ref) string {
	return fmt.Sprintf("https://%s/%s/%s/pull/%d", host(ref), ref.Org, ref.Repo, ref.Number)
}
