// Package forge is the small interface between atrium and a code forge's command
// line tool. See docs/rnd/scm-forge-design.md.
//
// A forge holds nothing. An implementation builds an argv for a named CLI, runs it
// through the bounded Runner it is given, and parses what the CLI prints into
// atrium's own shapes. It never holds a token: the CLI owns the login, and atrium
// holds only the NAME of the command. Every method is read only.
//
// Nothing here assumes GitHub. A second forge fills the same PR struct from
// different output.
package forge

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Kinds a provider may name. `none` is a host atrium reads no pull requests from.
const (
	GitHub    = "github"
	Bitbucket = "bitbucket"
	GitLab    = "gitlab"
	None      = "none"
)

// Ref names one pull request.
type Ref struct {
	Host   string
	Org    string
	Repo   string
	Number int
}

// File is one changed file.
type File struct {
	Path      string `json:"path"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

// PR is what a forge says about a pull request, in atrium's shape.
type PR struct {
	Title  string `json:"title"`
	Author string `json:"author"`
	// Head is the full sha of the head commit. HeadRef is the branch it is on.
	Head    string `json:"head"`
	HeadRef string `json:"head_ref"`
	BaseRef string `json:"base_ref"`
	// FromFork is true when the head lives in another repository.
	FromFork bool   `json:"from_fork"`
	Files    []File `json:"files"`
}

// FetchSpec is how git gets the head: fetch Refspec from Remote.
type FetchSpec struct {
	Remote  string
	Refspec string
}

// Cmd is one named outbound command.
type Cmd struct {
	Name    string
	Args    []string
	Dir     string
	Timeout time.Duration
	Limit   int
}

// Runner runs a Cmd with a time bound and a read bound and returns what it printed.
type Runner func(ctx context.Context, c Cmd) ([]byte, error)

// Forge is what the PR runner needs from a forge.
type Forge interface {
	// Kind is one of the constants above.
	Kind() string
	// View answers title, author, head, branches, fork flag and changed files.
	View(ctx context.Context, ref Ref) (*PR, error)
	// Diff is the unified diff, capped.
	Diff(ctx context.Context, ref Ref) ([]byte, error)
	// Head is the head sha only, for the head check.
	Head(ctx context.Context, ref Ref) (string, error)
	// FetchSpec is how git fetches the head. It runs nothing.
	FetchSpec(ref Ref) FetchSpec
	// PRURL is the pull request's web address. It runs nothing.
	PRURL(ref Ref) string
}

// DefaultHosts is the built-in forge for a host no provider names.
var DefaultHosts = map[string]string{
	"github.com":    GitHub,
	"bitbucket.org": Bitbucket,
}

// Entry is the part of a provider that picks a forge. The caller fills it from
// its enabled providers, so this package never reads a store.
type Entry struct {
	Host string
	// Forge is the provider's field, empty meaning infer from the host.
	Forge string
	// Cmd overrides the command NAME. Never a token.
	Cmd string
}

// Pick chooses the forge kind and command name for a host: an entry with that
// host and its forge, else the built-in default, else NoForgeError. cmd is empty
// when nothing overrides the kind's own command.
func Pick(host string, entries []Entry) (kind, cmd string, err error) {
	host = strings.ToLower(strings.TrimSpace(host))
	for _, e := range entries {
		if strings.ToLower(strings.TrimSpace(e.Host)) != host {
			continue
		}
		// A provider that names the host decides, even when it says none.
		kind = strings.TrimSpace(e.Forge)
		if kind == "" {
			kind = DefaultHosts[host]
		}
		if kind == "" || kind == None {
			return "", "", &NoForgeError{Host: host}
		}
		return kind, strings.TrimSpace(e.Cmd), nil
	}
	if kind = DefaultHosts[host]; kind == "" {
		return "", "", &NoForgeError{Host: host}
	}
	return kind, "", nil
}

// New builds the forge of a kind. GitHub and Bitbucket are built.
func New(kind, cmd string, run Runner) (Forge, error) {
	switch kind {
	case GitHub:
		return newGitHub(cmd, run), nil
	case Bitbucket:
		return newBitbucket(cmd, run), nil
	case GitLab:
		return nil, &NoForgeError{Kind: kind}
	}
	return nil, &NoForgeError{Kind: kind}
}

// For is Pick then New.
func For(host string, entries []Entry, run Runner) (Forge, error) {
	kind, cmd, err := Pick(host, entries)
	if err != nil {
		return nil, err
	}
	return New(kind, cmd, run)
}

// NoForgeError is a PR row's `no_forge` failure: atrium knows no forge for the host.
type NoForgeError struct {
	Host string
	// Kind is set when a forge is named but not built.
	Kind string
}

// Code is the failure code of the row.
func (e *NoForgeError) Code() string { return "no_forge" }

func (e *NoForgeError) Error() string {
	if e.Host == "" && e.Kind != "" {
		return fmt.Sprintf("no_forge: the %s forge is not built yet, so its pull requests cannot be read. "+
			"Ask for it, or set the provider's forge to github", e.Kind)
	}
	return fmt.Sprintf("no_forge: atrium does not know the forge for %s. "+
		"Add a provider with host %s and its forge set to github", e.Host, e.Host)
}

// AccessError is a forge CLI that is missing or not logged in. The sentence says
// what to run.
type AccessError struct {
	Tool   string
	Host   string
	Detail string
	// NotInstalled is true when the command is not on the PATH.
	NotInstalled bool
	// Login is the command that logs in, when it is not `<tool> auth login --hostname <host>`.
	Login string
}

func (e *AccessError) login() string {
	if e.Login != "" {
		return e.Login
	}
	return fmt.Sprintf("%s auth login --hostname %s", e.Tool, e.Host)
}

func (e *AccessError) Error() string {
	if e.NotInstalled {
		return fmt.Sprintf("%s is not installed here. Install it and run `%s`", e.Tool, e.login())
	}
	return fmt.Sprintf("%s is not logged in for %s. Run `%s`", e.Tool, e.Host, e.login())
}

// access turns a runner error into an AccessError when it is one, else returns it
// unchanged.
func access(tool, host string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		return &AccessError{Tool: tool, Host: host, Detail: err.Error(), NotInstalled: true}
	}
	low := strings.ToLower(err.Error())
	for _, s := range []string{"gh auth login", "not logged in", "authentication required", "http 401",
		"bad credentials", "set the gh_token"} {
		if strings.Contains(low, s) {
			return &AccessError{Tool: tool, Host: host, Detail: err.Error()}
		}
	}
	return err
}
