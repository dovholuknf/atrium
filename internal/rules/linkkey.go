package rules

import (
	"strconv"
	"strings"
)

// THE KIND AND THE KEY OF A RECOGNISED LINK, in one place so a room (internal/api/openkinds.go) and a hub placing the
// same paste (internal/link/openroute.go) read the same key off the same link. The hub claims that key for the room.

// The kinds a link opens as.
const (
	LinkPR      = "pr"
	LinkIssue   = "issue"
	LinkBranch  = "branch"
	LinkSupport = "support"
)

// LinkKind says what a recognised link is, and the parts its key is made from. Kind is "" for a link nothing opens.
type LinkKind struct {
	Kind            string
	Host, Org, Repo string
	Num             int
	Branch          string
}

// ClassifyLink reads the kind off the resolution, never off the link's text.
func ClassifyLink(got *Resolved) LinkKind {
	k := LinkKind{
		Host:   strings.ToLower(strings.TrimSpace(got.Vars["host"])),
		Org:    strings.TrimSpace(got.Vars["org"]),
		Repo:   strings.TrimSpace(got.Vars["repo"]),
		Branch: strings.TrimSpace(got.Branch),
	}
	if k.Host == "" {
		k.Host = strings.ToLower(strings.TrimSpace(got.Host))
	}
	k.Num, _ = strconv.Atoi(strings.TrimSpace(got.Vars["num"]))
	pr := false
	for _, t := range got.Tags {
		if t == PRTag {
			pr = true
		}
	}
	holed := func(s string) bool { return strings.Contains(s, "{") }
	named := k.Host != "" && k.Org != "" && k.Repo != ""
	switch {
	case pr && named && k.Num > 0:
		k.Kind = LinkPR
	case pr:
	case named && k.Num > 0 && k.Branch != "" && !holed(k.Branch):
		k.Kind = LinkIssue
	case named && k.Num <= 0 && k.Branch != "" && !holed(k.Branch):
		k.Kind = LinkBranch
	case !named && k.Host != "" && k.Num > 0 && k.Branch != "" && !holed(k.Branch):
		k.Kind = LinkSupport
	}
	return k
}

// Key is the canonical key of the link. An issue's key carries an `i`, so issue 5 and PR 5 are two keys. rowKind is
// the recogniser row's kind, which names a support ticket's key.
func (k LinkKind) Key(rowKind string) string {
	switch k.Kind {
	case LinkPR:
		return PRKey(k.Host, k.Org, k.Repo, k.Num)
	case LinkIssue:
		return k.Host + "/" + k.Org + "/" + k.Repo + "/i" + strconv.Itoa(k.Num)
	case LinkBranch:
		return k.Host + "/" + k.Org + "/" + k.Repo + "@" + k.Branch
	case LinkSupport:
		kind := strings.ToLower(strings.TrimSpace(rowKind))
		if kind == "" {
			kind = "link"
		}
		return kind + ":" + k.Host + "/" + strconv.Itoa(k.Num)
	}
	return ""
}
