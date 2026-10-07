package forge

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// FactsFetch is the recogniser's built-in fetch: `forge pr` or `forge issue` reads the pull request or issue a link
// names. A room runs it through its hub, and the hub, answering a paste itself, through its own forge.
const FactsFetch = "forge"

// Facts reads what a recogniser's `forge pr` or `forge issue` fetch asks for. args are the fetch's arguments and vars
// the pattern's captures, which must hold org, repo and num. pick is the forge for a host. The facts carry gh's names,
// so a template written for `gh pr view --json title,headRefName,baseRefName` reads the same.
func Facts(ctx context.Context, args []string, vars map[string]string,
	pick func(host string) (Forge, error)) (map[string]string, error) {

	what := ""
	if len(args) > 0 {
		what = strings.TrimSpace(args[0])
	}
	if what != "pr" && what != "issue" {
		return nil, errors.New("the forge fetch takes one argument, pr or issue")
	}
	n, err := strconv.Atoi(strings.TrimSpace(vars["num"]))
	if err != nil || n <= 0 || vars["org"] == "" || vars["repo"] == "" {
		return nil, errors.New("the forge fetch needs the pattern to capture org, repo and num")
	}
	host := strings.ToLower(strings.TrimSpace(vars["host"]))
	if host == "" {
		host = "github.com"
	}
	ref := Ref{Host: host, Org: vars["org"], Repo: vars["repo"], Number: n}
	f, err := pick(host)
	if err != nil {
		return nil, err
	}
	if what == "issue" {
		ir, ok := f.(IssueReader)
		if !ok {
			return nil, fmt.Errorf("the %s forge does not read issues", f.Kind())
		}
		is, err := ir.Issue(ctx, ref)
		if err != nil {
			return nil, err
		}
		return map[string]string{"title": is.Title, "body": is.Body, "author": is.Author, "state": is.State}, nil
	}
	// A paste being recognised asks about the pull request and nothing more, so the hub fetches no head for it.
	view := f.View
	if hf, ok := f.(*Remote); ok {
		view = hf.Peek
	}
	pr, err := view(ctx, ref)
	if err != nil {
		return nil, err
	}
	return map[string]string{"title": pr.Title, "headRefName": pr.HeadRef, "baseRefName": pr.BaseRef,
		"author": pr.Author, "headRefOid": pr.Head}, nil
}
