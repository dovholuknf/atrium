package gitsync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/dovholuknf/atrium/internal/safepath"
)

// Clones in the operator's scm folder: `<git.scm_root>/<host>/<owner>/<repo>`, made by
// `atrium_git_clone {url}`. See docs/fabric/hub-forge-design.md section 5.2 and wall 2 of 5.3.
//
// WHAT IS NOT HERE. The `hub` remote's URL is the room's stable forwarder, which another item
// builds. This file reaches it through SCM.HubURL, and hubremote.go holds the one function that
// adds the remote, so the two can meet in one place.

// SettingSCMRoot is the room setting naming the scm folder. Empty means unset, and
// atrium_git_clone then refuses and says how to set it. It is set on the room at install, and
// never by the hub. It is NOT `git_root`, which is where the hub's git sync keeps its clones.
const SettingSCMRoot = "git.scm_root"

// SettingCredentialHosts lists the hosts atrium's credential helper may be asked about, comma or space
// separated. Empty means `github.com`. A clone of any other host runs with no helper at all, so a card
// that names a host of its own cannot make the room send the operator's credential there.
const SettingCredentialHosts = "git.credential_hosts"

// DefaultCredentialHosts is what an empty git.credential_hosts means.
const DefaultCredentialHosts = "github.com"

// SettingCredentialHelper is the credential helper the operator configured for atrium's clones,
// as git would write it after `credential.helper=`. Empty means none, and a public repository
// needs none. Atrium's git runs with every other helper switched off.
const SettingCredentialHelper = "git.credential_helper"

// CloneFailed is the sentence a failed clone answers, EXACTLY, whatever git said. A private
// repository atrium has no credential for and a wrong name look the same from here, and git's
// own words may carry a URL.
const CloneFailed = "that repo doesn't exist, check it, and if it is private have the operator clone it. Atrium can't."

// OriginPushURL is the push URL set on `origin` of a clone atrium made, so a push to the forge
// fails on a scheme nothing serves, whatever a script does to avoid a hook.
const OriginPushURL = "atrium-refused://origin-push"

// Config keys atrium keeps in a clone's own .git/config.
const (
	cfgMade    = "atrium.clone"   // "made": atrium made this clone
	cfgAdopted = "atrium.adopted" // "yes": the operator said yes to atrium's remotes on it
)

// ErrNoSCMRoot is what a room with no git.scm_root answers.
var ErrNoSCMRoot = errors.New("this room has no scm folder, so atrium has nowhere to clone to. " +
	"Set git.scm_root on the room (for example `atrium settings set git.scm_root ~/git`) and ask again")

// ErrAsked is what a clone the operator made answers until the operator says yes: atrium asked
// on the board and has changed nothing in the clone.
var ErrAsked = errors.New("this clone was made by the operator, so atrium asked on the board before it adds its " +
	"remotes to it. Nothing was changed. Ask again once the operator has answered")

// ErrDenied is what a clone answers when the operator said no. The clone is left as it was.
var ErrDenied = errors.New("the operator said no to atrium adding its remotes to this clone, so it was left as it was")

// SCMResult is what atrium_git_clone answers.
type SCMResult struct {
	// Path is the clone, always under the scm folder.
	Path string `json:"path"`
	// State is `cloned` (atrium made it) or `existing` (it was already there).
	State string `json:"state"`
	// Hub is the name atrium's remote went in under: `hub`, or `atrium-hub` when the clone's own
	// `hub` points elsewhere. Empty when it was not added, and Note says why.
	Hub  string `json:"hub,omitempty"`
	Note string `json:"note,omitempty"`
}

// SCM makes and finds clones in the scm folder.
type SCM struct {
	Runner *Runner
	// Root is the scm folder: the git.scm_root setting, read on every use.
	Root func() string
	// CredentialHelper is the git.credential_helper setting. Nil or empty means none.
	CredentialHelper func() string
	// CredentialHosts is the git.credential_hosts setting. Nil or empty means github.com.
	CredentialHosts func() string
	// Yes asks the operator, on the board, once for a clone atrium did not make. It answers
	// ErrAsked while nobody has answered and ErrDenied for a no. A nil Yes refuses every such clone.
	Yes func(ctx context.Context, clone string) error
	// HubURL is the stable `hub` remote URL for a repository. Nil means there is none yet and the
	// remote is not added. See hubremote.go.
	HubURL func(Ref) (string, error)

	// From, when set, is where a room attached to a hub clones from instead of the forge: the hub's copy of the
	// repository, through a loopback to the link (HubLoopback). done closes it. The clone's origin is then set to the
	// forge's https URL, so the push guard and the origin check read it as any other clone, and the room never
	// fetched from the forge. See docs/rnd/scm-forge-design.md, Built.
	From func(ctx context.Context, r Ref) (src string, done func(), err error)

	// source is the URL git clones from. It is unexported, so only this package's tests set it:
	// they point it at a local bare repository, and production always clones from the https URL.
	source func(Ref) string
	// extra is git config for the clone, `-c k=v` pairs, and is for the same tests.
	extra []string

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func (c *SCM) lock(dir string) func() {
	// Lowercased: on a case-insensitive disk Foo/bar and foo/bar are one folder.
	dir = strings.ToLower(dir)
	c.mu.Lock()
	if c.locks == nil {
		c.locks = map[string]*sync.Mutex{}
	}
	m := c.locks[dir]
	if m == nil {
		m = &sync.Mutex{}
		c.locks[dir] = m
	}
	c.mu.Unlock()
	m.Lock()
	return m.Unlock
}

func (c *SCM) runner() *Runner {
	if c.Runner != nil {
		return c.Runner
	}
	return Default
}

// httpsURL is the one URL atrium clones from, built from the checked parts and never from what
// was given: `https://<host>/<owner>/<repo>.git`, with github.com for the host `github`.
func httpsURL(r Ref) string {
	host := r.Host
	if host == DefaultHost {
		host = "github.com"
	}
	return "https://" + host + "/" + r.Owner + "/" + r.Repo + ".git"
}

// credentialArgs passes the operator's helper for THIS host only, and only when the host is in
// git.credential_hosts. The key is scoped to the https URL of the checked host, so git asks the helper
// about no other host, a redirect target included. An unscoped helper is asked about every host, and
// a server that answers 401 gets whatever it returns.
func (c *SCM) credentialArgs(r Ref) []string {
	if c.CredentialHelper == nil {
		return nil
	}
	h := strings.TrimSpace(c.CredentialHelper())
	if h == "" {
		return nil
	}
	host := strings.TrimPrefix(httpsURL(r), "https://")
	host = host[:strings.Index(host, "/")]
	list := DefaultCredentialHosts
	if c.CredentialHosts != nil {
		if v := strings.TrimSpace(c.CredentialHosts()); v != "" {
			list = v
		}
	}
	for _, allowed := range strings.FieldsFunc(list, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' }) {
		if strings.EqualFold(allowed, host) {
			return []string{"-c", "credential.https://" + host + ".helper=" + h}
		}
	}
	return nil
}

// ClonePath is where a repository's clone is, checked to stay under the scm folder after
// symlinks are resolved. The folder need not have the clone yet.
func (c *SCM) ClonePath(r Ref) (root, dest string, err error) {
	root = ""
	if c.Root != nil {
		root = strings.TrimSpace(c.Root())
	}
	if root == "" {
		return "", "", ErrNoSCMRoot
	}
	root = expandHome(root)
	if !filepath.IsAbs(root) {
		return "", "", refuse("git.scm_root has to be an absolute path, not %s", filepath.ToSlash(root))
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", "", refuse("the scm folder %s cannot be made: %v", filepath.ToSlash(root), err)
	}
	dest, err = safepath.Contained(root, filepath.Join(root, r.Host, r.Owner, r.Repo))
	if err != nil {
		return "", "", refuse("that repository would be cloned outside the scm folder, so it was refused")
	}
	return root, dest, nil
}

// ExpandHome is a git.scm_root value with a leading ~ made the home folder.
func ExpandHome(p string) string { return expandHome(p) }

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if h, err := os.UserHomeDir(); err == nil && h != "" {
			return filepath.Join(h, p[1:])
		}
	}
	return p
}

// Clone finds the clone of a repository, or makes it, and adds atrium's remotes.
func (c *SCM) Clone(ctx context.Context, rawURL string) (SCMResult, error) {
	ref, err := ParseURL(rawURL)
	if err != nil {
		return SCMResult{}, err
	}
	root, dest, err := c.ClonePath(ref)
	if err != nil {
		return SCMResult{}, err
	}
	defer c.lock(dest)()

	g := c.runner()
	if st, err := os.Stat(filepath.Join(dest, ".git")); err == nil && st != nil {
		return c.existing(ctx, ref, dest)
	}
	if ents, err := os.ReadDir(dest); err == nil && len(ents) > 0 {
		return SCMResult{}, refuse("%s is there and is not a git clone, so atrium left it alone", filepath.ToSlash(dest))
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return SCMResult{}, refuse("the clone's folder cannot be made: %v", err)
	}
	// Made, so checked again: the parent's own path may have been a link a moment ago.
	if _, err := safepath.Contained(root, dest); err != nil {
		return SCMResult{}, refuse("that repository would be cloned outside the scm folder, so it was refused")
	}

	src, args, env := httpsURL(ref), []string{}, []string{"GIT_ALLOW_PROTOCOL=https"}
	viaHub := false
	switch {
	case c.source != nil:
		src, env = c.source(ref), nil
	case c.From != nil:
		from, done, err := c.From(ctx, ref)
		if err != nil {
			return SCMResult{}, err
		}
		defer done()
		src, viaHub, env = from, true, []string{"GIT_ALLOW_PROTOCOL=http"}
		args = append(args, "-c", "protocol.allow=never", "-c", "protocol.http.allow=always")
	default:
		args = append(args, "-c", "protocol.allow=never", "-c", "protocol.https.allow=always")
	}
	args = append(args, c.extra...)
	if !viaHub {
		args = append(args, c.credentialArgs(ref)...)
	}
	// An argv slice, never a shell. `--` ends options, though the URL was built from checked
	// parts and cannot start with a dash. The output is capped, and the runner bounds the time.
	args = append(args, "clone", "--no-tags", "--", src, dest)
	if _, err := g.GitCapped(ctx, filepath.Dir(dest), env, 64<<10, args...); err != nil {
		if errors.Is(err, ErrStopped) {
			return SCMResult{}, err
		}
		_ = os.RemoveAll(dest)
		return SCMResult{}, errors.New(CloneFailed)
	}
	if viaHub {
		// The loopback is gone once this returns, and origin names the forge as any clone's does.
		if _, err := g.GitCapped(ctx, dest, nil, 4<<10, "remote", "set-url", "origin", httpsURL(ref)); err != nil {
			_ = os.RemoveAll(dest)
			return SCMResult{}, errors.New(CloneFailed)
		}
	}
	if err := c.guard(ctx, dest, true); err != nil {
		return SCMResult{}, err
	}
	res := SCMResult{Path: filepath.ToSlash(dest), State: "cloned"}
	c.hub(ctx, ref, dest, &res)
	return res, nil
}

// existing handles a clone that is already there.
func (c *SCM) existing(ctx context.Context, ref Ref, dest string) (SCMResult, error) {
	g := c.runner()
	if c.source == nil {
		if msg := originMismatch(ctx, g, dest, ref.Name()); msg != "" {
			return SCMResult{}, refuse("%s", msg)
		}
	}
	res := SCMResult{Path: filepath.ToSlash(dest), State: "existing"}
	made := c.cfg(ctx, dest, cfgMade) == "made"
	if !made && c.cfg(ctx, dest, cfgAdopted) != "yes" {
		// ONE YES, the first time for this clone. Until then nothing in it changes.
		if c.Yes == nil {
			return SCMResult{}, ErrAsked
		}
		if err := c.Yes(ctx, dest); err != nil {
			return SCMResult{}, err
		}
		if _, err := g.Git(ctx, dest, "config", cfgAdopted, "yes"); err != nil {
			return SCMResult{}, errors.New("could not record the operator's yes in the clone: " + firstLine(err))
		}
	}
	// Wall 2 goes on an operator's clone with the same yes, and on atrium's own as it was made.
	if err := c.guard(ctx, dest, false); err != nil {
		return SCMResult{}, err
	}
	c.hub(ctx, ref, dest, &res)
	return res, nil
}

func (c *SCM) cfg(ctx context.Context, dir, key string) string {
	out, err := c.runner().Git(ctx, dir, "config", "--local", "--get", key)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// guard is wall 2: origin can be fetched from and cannot be pushed to. `made` also marks the
// clone as atrium's.
func (c *SCM) guard(ctx context.Context, dir string, made bool) error {
	g := c.runner()
	if made {
		if _, err := g.Git(ctx, dir, "config", cfgMade, "made"); err != nil {
			return errors.New("could not mark the clone: " + firstLine(err))
		}
	}
	if _, err := g.Git(ctx, dir, "config", "remote.origin.pushurl", OriginPushURL); err != nil {
		return errors.New("could not set the push guard on origin: " + firstLine(err))
	}
	return nil
}
