package gitsync

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
)

// The names of the hub's own store: `<host>/<owner>/<repo>`, which is also the directory
// `<git.store>/<host>/<owner>/<repo>.git`. See docs/rnd/hub-forge-design.md section 3.1.

// DefaultHost is the host a short name has: `openziti/zrok` means `github/openziti/zrok`.
const DefaultHost = "github"

// Ref is one repository in the store.
type Ref struct {
	// Host is `github` for github.com, and the lowercased hostname for any other forge.
	Host, Owner, Repo string
}

// Name is `<host>/<owner>/<repo>`.
func (r Ref) Name() string { return r.Host + "/" + r.Owner + "/" + r.Repo }

// ErrRefused is what a name or URL the store will not take answers.
var ErrRefused = errors.New("refused")

// ErrConflict is what a repository that cannot be made because of what is already on disk answers.
var ErrConflict = errors.New("conflict")

// refusal is a sentence the operator reads. It never repeats the text it refused, because that
// text may be a URL with a token in it.
type refusal struct {
	kind error
	msg  string
}

func (e *refusal) Error() string { return e.msg }
func (e *refusal) Unwrap() error { return e.kind }

func refuse(format string, a ...any) error {
	return &refusal{kind: ErrRefused, msg: fmt.Sprintf(format, a...)}
}

func conflict(format string, a ...any) error {
	return &refusal{kind: ErrConflict, msg: fmt.Sprintf(format, a...)}
}

// maxURL bounds what is looked at. A URL is nowhere near this long.
const maxURL = 2048

var (
	hostRe    = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)
	segmentRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
	scpRe     = regexp.MustCompile(`^([^@/:\s]+)@([^:/\s]+):(.+)$`)
	// reserved are the names Windows will not make a file called, with or without an extension.
	reserved = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[1-9]|lpt[1-9])(\..*)?$`)
)

// canonicalHost maps a forge's hostname onto the host directory. github.com is `github`.
func canonicalHost(h string) string {
	h = strings.ToLower(h)
	if h == "github.com" {
		return DefaultHost
	}
	return h
}

func checkHost(h string) error {
	if !hostRe.MatchString(h) || strings.Contains(h, "..") || len(h) > 253 {
		return refuse("the host is not a plain hostname")
	}
	return nil
}

func checkSegment(what, s string) error {
	switch {
	case !segmentRe.MatchString(s):
		return refuse("the %s has to be letters, digits, dot, dash and underscore, up to 100 of them", what)
	case s == "." || s == "..":
		return refuse("the %s cannot be . or ..", what)
	case strings.EqualFold(s, ".git") || strings.HasSuffix(strings.ToLower(s), ".git"):
		return refuse("the %s cannot be, or end in, .git", what)
	case strings.HasSuffix(s, "."):
		return refuse("the %s cannot end in a dot, which a Windows disk drops", what)
	case reserved.MatchString(s):
		return refuse("the %s is a name Windows will not make a file called", what)
	}
	return nil
}

// ParseName reads `<host>/<owner>/<repo>` or the short `<owner>/<repo>`, with the existing name
// rules (no `..`, no backslash, no drive letter) and a strict character set on top of them.
func ParseName(name string) (Ref, error) {
	if !ValidName(name) {
		return Ref{}, refuse("that is not <host>/<owner>/<repo>: no empty parts, no `..`, no backslash and no drive letter")
	}
	parts := strings.Split(name, "/")
	switch len(parts) {
	case 2:
		parts = append([]string{DefaultHost}, parts...)
	case 3:
	default:
		return Ref{}, refuse("a repository is <owner>/<repo>, or <host>/<owner>/<repo>")
	}
	r := Ref{Host: canonicalHost(parts[0]), Owner: parts[1], Repo: strings.TrimSuffix(parts[2], ".git")}
	if err := checkHost(r.Host); err != nil {
		return Ref{}, err
	}
	if err := checkSegment("owner", r.Owner); err != nil {
		return Ref{}, err
	}
	if err := checkSegment("repository name", r.Repo); err != nil {
		return Ref{}, err
	}
	return r, nil
}

// ParseURL reads a forge URL into a Ref: https://github.com/o/r, with or without `.git` and a
// trailing slash, or the scp form git@github.com:o/r.git.
//
// A URL WITH A USERNAME, A PASSWORD OR A TOKEN IN IT IS REFUSED, and the refusal never repeats
// it. Nothing but the host, the owner and the repository is kept from what is given.
func ParseURL(raw string) (Ref, error) {
	if len(raw) > maxURL {
		return Ref{}, refuse("that is too long to be a repository URL")
	}
	// Spaces around it are a paste. A control character anywhere, a newline included, is refused.
	raw = strings.Trim(raw, " ")
	if raw == "" {
		return Ref{}, refuse("say which repository, for example https://github.com/openziti/zrok")
	}
	for _, c := range raw {
		if c < 0x20 || c == 0x7f || c == ' ' || c == '\\' || c > 0x7e {
			return Ref{}, refuse("a repository URL has no spaces, control characters or backslashes in it")
		}
	}
	var host, p string
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return Ref{}, refuse("that is not a URL")
		}
		switch {
		case u.User != nil || strings.Contains(u.Host, "@"):
			return Ref{}, refuse("that URL has a username or a token in it, and atrium will not take one. " +
				"give the plain URL, and for a private repository push main to the hub as the operator")
		case u.Scheme != "https":
			return Ref{}, refuse("only https URLs, and git@host:owner/repo.git, are taken")
		case u.Port() != "" || strings.Contains(u.Host, ":"):
			return Ref{}, refuse("a host with a port is not taken")
		case u.RawQuery != "" || u.Fragment != "" || u.ForceQuery || strings.Contains(u.EscapedPath(), "%"):
			return Ref{}, refuse("a repository URL has no query, fragment or escapes in it")
		}
		host, p = u.Hostname(), u.Path
	} else {
		m := scpRe.FindStringSubmatch(raw)
		switch {
		case m == nil:
			return Ref{}, refuse("that is not https://host/owner/repo or git@host:owner/repo.git")
		case m[1] != "git":
			return Ref{}, refuse("the only user an scp-form URL may have is git")
		}
		host, p = m[2], m[3]
		if strings.Contains(p, "%") {
			return Ref{}, refuse("a repository URL has no escapes in it")
		}
	}
	p = strings.TrimLeft(p, "/")
	p = strings.TrimRight(p, "/")
	p = strings.TrimSuffix(p, ".git")
	parts := strings.Split(p, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || path.Clean(p) != p {
		return Ref{}, refuse("the path has to be <owner>/<repo>, nothing more and nothing less")
	}
	r := Ref{Host: canonicalHost(host), Owner: parts[0], Repo: parts[1]}
	if err := checkHost(r.Host); err != nil {
		return Ref{}, err
	}
	if err := checkSegment("owner", r.Owner); err != nil {
		return Ref{}, err
	}
	if err := checkSegment("repository name", r.Repo); err != nil {
		return Ref{}, err
	}
	return r, nil
}
