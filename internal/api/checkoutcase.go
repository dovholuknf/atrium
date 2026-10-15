package api

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// findProviderCheckout is where host/org/repo lives under a provider root, with the case as written tried first and
// then each of org and repo matched case-insensitively against what is on disk. A forge compares names without case,
// so a pasted link's O/R is the checkout at o/r, and on a case-sensitive disk the path has to be the folder's own
// spelling, not the link's and not a lowered one. It returns "" when nothing there is a checkout. An exact spelling
// wins, and when two folders differ only by case the first in name order wins, so the answer is stable.
func findProviderCheckout(root, org, repo string) string {
	return findProviderCheckoutIn(root, org, repo, os.ReadDir, isCheckout)
}

func findProviderCheckoutIn(root, org, repo string, readDir func(string) ([]os.DirEntry, error), isRepo func(string) bool) string {
	exact := store.ProviderPath(root, org, repo)
	if exact == "" {
		return ""
	}
	if isRepo(filepath.FromSlash(exact)) {
		return exact
	}
	org = strings.Trim(strings.TrimSpace(org), "/")
	dir := store.ProviderPath(root, "", "x")
	dir = strings.TrimSuffix(dir, "/x")
	// pick is the folder under dir named like want, ignoring case
	pick := func(dir, want string, ok func(string) bool) string {
		ents, err := readDir(filepath.FromSlash(dir))
		if err != nil {
			return ""
		}
		for _, e := range ents {
			if strings.EqualFold(e.Name(), want) && ok(dir+"/"+e.Name()) {
				return e.Name()
			}
		}
		return ""
	}
	if org != "" {
		// the org may differ by case and so may the repo, and a different org folder may be the one holding it
		ents, err := readDir(filepath.FromSlash(dir))
		if err != nil {
			return ""
		}
		for _, e := range ents {
			if !strings.EqualFold(e.Name(), org) {
				continue
			}
			sub := dir + "/" + e.Name()
			if r := pick(sub, repo, func(p string) bool { return isRepo(filepath.FromSlash(p)) }); r != "" {
				return sub + "/" + r
			}
		}
		return ""
	}
	if r := pick(dir, repo, func(p string) bool { return isRepo(filepath.FromSlash(p)) }); r != "" {
		return dir + "/" + r
	}
	return ""
}
