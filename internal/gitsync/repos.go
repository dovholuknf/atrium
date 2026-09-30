package gitsync

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// Repo is one repository the hub mirrors and serves: the hub setting `git_repos` is a JSON
// list of these. It is set by the operator on the hub and never by a room.
type Repo struct {
	// Name is `<host>/<owner>/<repo>`, read from the checkout's origin the way
	// room-git.ps1 does, for example `github/dovholuknf/atrium`.
	Name string `json:"name"`
	// Checkout is the hub machine's own clone, the one @merge writes.
	Checkout string `json:"checkout"`
	// Branch is the integration branch the hub mirrors. `claude/main` or `main`, nothing
	// else. See ValidateRepo.
	Branch string `json:"branch"`
}

// IntegrationBranch is what an empty branch means.
const IntegrationBranch = "claude/main"

// ValidateRepo says whether an entry may be mirrored, and why not in words the operator
// reads.
//
// THE BRANCH IS THE INTEGRATION BRANCH AND NOTHING ELSE. A room's hub-main mirrors it, so a
// department branch here would put unmerged work on every room's base. That happened once
// by hand and other departments' workers started on unmerged work. So no setting, tool,
// route or verb takes any other branch.
func ValidateRepo(r Repo) error {
	if !ValidName(r.Name) {
		return fmt.Errorf("the repository name %q is not <host>/<owner>/<repo>: no empty parts, no `..`, "+
			"no backslash and no drive letter", r.Name)
	}
	if strings.TrimSpace(r.Checkout) == "" || !filepath.IsAbs(filepath.FromSlash(r.Checkout)) {
		return fmt.Errorf("the checkout for %s must be an absolute path on the hub's machine, got %q", r.Name, r.Checkout)
	}
	if r.Branch != "claude/main" && r.Branch != "main" {
		return fmt.Errorf("the branch for %s is %q, and it must be claude/main or main. a room's hub-main "+
			"mirrors the integration branch only, never a department branch, because a department "+
			"branch is unmerged work that every room would then start from", r.Name, r.Branch)
	}
	return nil
}

// ParseRepos reads the `git_repos` setting. Empty is no repositories. An empty branch
// means claude/main. One bad entry refuses the whole list, with which one and why.
func ParseRepos(raw string) ([]Repo, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var repos []Repo
	if err := json.Unmarshal([]byte(raw), &repos); err != nil {
		return nil, fmt.Errorf("git_repos is not a JSON list of {name, checkout, branch}: %w", err)
	}
	seen := map[string]bool{}
	for i := range repos {
		if strings.TrimSpace(repos[i].Branch) == "" {
			repos[i].Branch = IntegrationBranch
		}
		if err := ValidateRepo(repos[i]); err != nil {
			return nil, err
		}
		if seen[repos[i].Name] {
			return nil, errors.New("git_repos names " + repos[i].Name + " twice")
		}
		seen[repos[i].Name] = true
	}
	return repos, nil
}
