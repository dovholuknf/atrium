package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// Issue is what a forge says about an issue, in atrium's shape. A recogniser reads it as facts, under the names gh
// prints, so a row written for `gh issue view --json title,body` keeps working.
type Issue struct {
	Title  string `json:"title"`
	Body   string `json:"body"`
	Author string `json:"author"`
	State  string `json:"state"`
	URL    string `json:"url"`
}

// IssueReader is a forge that reads issues. It is its own interface so a forge, or a test's fake, that only reads
// pull requests is still a Forge.
type IssueReader interface {
	Issue(ctx context.Context, ref Ref) (*Issue, error)
}

func (g *github) Issue(ctx context.Context, ref Ref) (*Issue, error) {
	out, err := g.do(ctx, ref, ghJSONLimit, "issue", "view", fmt.Sprint(ref.Number), "--repo", repoArg(ref), "--json",
		"title,body,author,state,url")
	if err != nil {
		return nil, err
	}
	var v struct {
		Title  string `json:"title"`
		Body   string `json:"body"`
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
		State string `json:"state"`
		URL   string `json:"url"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return nil, fmt.Errorf("%s issue view printed something that is not json: %w", g.cmd, err)
	}
	return &Issue{Title: v.Title, Body: v.Body, Author: v.Author.Login, State: v.State, URL: v.URL}, nil
}

func (b *bitbucket) Issue(ctx context.Context, ref Ref) (*Issue, error) {
	path := fmt.Sprintf("/repositories/%s/%s/issues/%d", url.PathEscape(ref.Org), url.PathEscape(ref.Repo), ref.Number)
	out, err := b.do(ctx, ref, bbJSONLimit, "api", path, "-o", "json")
	if err != nil {
		return nil, err
	}
	var v struct {
		Title   string `json:"title"`
		Content struct {
			Raw string `json:"raw"`
		} `json:"content"`
		Reporter struct {
			Nickname    string `json:"nickname"`
			DisplayName string `json:"display_name"`
		} `json:"reporter"`
		State string `json:"state"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return nil, fmt.Errorf("%s api printed something that is not json: %w", b.cmd, err)
	}
	author := v.Reporter.Nickname
	if author == "" {
		author = v.Reporter.DisplayName
	}
	return &Issue{Title: v.Title, Body: v.Content.Raw, Author: author, State: v.State,
		URL: fmt.Sprintf("https://%s/%s/%s/issues/%d", bbHost(ref), ref.Org, ref.Repo, ref.Number)}, nil
}
