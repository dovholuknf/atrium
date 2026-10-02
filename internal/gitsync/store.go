package gitsync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The hub's own git store: bare repositories at `<git.store>/<host>/<owner>/<repo>.git`, one per
// repository the operator made with `atrium rooms git init`. See docs/rnd/hub-forge-design.md 3.1.
//
// IT SITS BESIDE THE git_repos MIRRORS AND SHARES THEIR PARTS. The Runner runs every git command,
// ValidName (through ParseName) checks every name, the Hub's locks serialise every repository, and
// the Hub's audit says what happened. By default `git.store` is `<hub dir>/git`, which is also
// where the `git_repos` bare repositories live, so the store lists ONLY what carries its marker
// file. A mirror is never listed unless the operator inited it, and a mirror is never changed by
// being inited: its HEAD stays on claude/main and the mirror keeps working.
//
// NO RECEIVE-PACK, NO HOOK AND NO SERVING ROUTE HERE. A repository is made ready for them and
// nothing more: http.receivepack is not written into any config, and no hook is installed.

// Marker is the file inside a bare repository that says the store made, or was told to take, it.
const Marker = "atrium-store"

// MainRef is the branch the hub holds, whatever the forge calls its own.
const MainRef = "refs/heads/main"

// Operator sentences. The first is the one the operator is told when the repository was made empty.
const (
	PushMain  = "push main to it as the operator."
	RerunInit = "once the network is back, run init again and it will seed main."
)

// Store is the hub's git store. Get it from Hub.Store, which wires the runner, locks and audit.
type Store struct {
	h *Hub

	// Forge is the URL the one seed is fetched from. Nil is https://<host>/<owner>/<repo>.git, and a
	// test points it at a local repository.
	Forge func(Ref) string
	// Protocols is GIT_ALLOW_PROTOCOL for the seed. Empty is https alone.
	Protocols string
	// GitVersion answers `git --version`. Nil asks git.
	GitVersion func(ctx context.Context) (string, error)
	// Probe says whether this git can make a reftable repository. Nil tries one in a temp directory.
	Probe func(ctx context.Context) bool
	// MainAt is the time shown for main. It is the commit time of main's tip here. The next item
	// replaces it with the time of the last operator push, from the push log, and nothing else changes.
	MainAt func(ctx context.Context, name string, tip Tip) *time.Time
	// SeedTimeout bounds the whole seed. Zero is two minutes.
	SeedTimeout time.Duration
}

// Tip is a ref's tip: the sha and the commit time.
type Tip struct {
	SHA string
	At  time.Time
}

// Store is this hub's store, made once. Its location is the `git.store` setting, read on every
// call so a change takes effect without a restart, and `<Dir>/git` when that is not set.
func (h *Hub) Store() *Store {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.store == nil {
		h.store = &Store{h: h}
	}
	return h.store
}

// Root is where the store is.
func (s *Store) Root() string {
	if s.h.StoreRoot != nil {
		if r := strings.TrimSpace(s.h.StoreRoot()); r != "" {
			return filepath.Clean(r)
		}
	}
	return filepath.Join(s.h.Dir, "git")
}

func (s *Store) dir(ref Ref) string {
	return filepath.Join(s.Root(), ref.Host, ref.Owner, ref.Repo+".git")
}

// Path is the bare repository's directory for a name, whether or not it exists. It is for the
// hub's own code, and is never put in an answer.
func (s *Store) Path(name string) (string, error) {
	ref, err := ParseName(name)
	if err != nil {
		return "", err
	}
	return s.dir(ref), nil
}

// Exists says whether the store holds a repository by that name.
func (s *Store) Exists(name string) bool {
	d, err := s.Path(name)
	if err != nil {
		return false
	}
	return isMarked(d)
}

func isMarked(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, Marker))
	return err == nil && st.Mode().IsRegular()
}

// Entry is one repository in the store.
type Entry struct {
	Ref Ref
	Dir string
}

// List is every repository in the store, by name. A bare repository under the root that does not
// carry the marker is not in it.
func (s *Store) List() ([]Entry, error) {
	var out []Entry
	each := func(dir string, f func(name string, d os.DirEntry)) {
		ents, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range ents {
			f(e.Name(), e)
		}
	}
	root := s.Root()
	each(root, func(host string, hd os.DirEntry) {
		if !hd.IsDir() {
			return
		}
		each(filepath.Join(root, host), func(owner string, od os.DirEntry) {
			if !od.IsDir() {
				return
			}
			each(filepath.Join(root, host, owner), func(repo string, rd os.DirEntry) {
				if !rd.IsDir() || !strings.HasSuffix(repo, ".git") {
					return
				}
				dir := filepath.Join(root, host, owner, repo)
				ref, err := ParseName(host + "/" + owner + "/" + strings.TrimSuffix(repo, ".git"))
				// A directory whose name is not the canonical one is not ours to list.
				if err != nil || ref.Host != host || !isMarked(dir) {
					return
				}
				out = append(out, Entry{Ref: ref, Dir: dir})
			})
		})
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Ref.Name() < out[j].Ref.Name() })
	return out, nil
}

// collision says which existing path `ref` would share on a disk that does not tell `Foo` from
// `foo` (sg4's NTFS), when it is not exactly the same path. The host, the owner and the repository
// are each compared, because `Foo/` and `foo/` are one directory there.
func (s *Store) collision(ref Ref) string {
	dir := s.Root()
	var have []string
	differs := false
	for _, w := range []string{ref.Host, ref.Owner, ref.Repo + ".git"} {
		ents, err := os.ReadDir(dir)
		if err != nil {
			break
		}
		pick := ""
		for _, e := range ents {
			if e.IsDir() && strings.EqualFold(e.Name(), w) && (pick == "" || e.Name() == w) {
				pick = e.Name()
			}
		}
		if pick == "" {
			break
		}
		differs = differs || pick != w
		have = append(have, pick)
		dir = filepath.Join(dir, pick)
	}
	if !differs {
		return ""
	}
	return strings.TrimSuffix(strings.Join(have, "/"), ".git")
}

// scrub takes the hub's own paths out of a sentence, so no answer names a directory on the
// hub's disk.
func (s *Store) scrub(msg string) string {
	for _, p := range []string{s.Root(), s.h.Dir} {
		if p == "" || p == "." {
			continue
		}
		for _, v := range []string{p, filepath.ToSlash(p)} {
			msg = strings.ReplaceAll(msg, v, "<hub>")
		}
	}
	return msg
}

func (s *Store) env() []string {
	proto := s.Protocols
	if proto == "" {
		proto = "https"
	}
	// THE OPERATOR'S GIT CONFIG IS NOT READ: a global insteadOf could send the seed anywhere, and a
	// credential helper in it could hand a credential to the forge. GIT_TERMINAL_PROMPT=0 comes
	// from the Runner's CleanEnv, and every GIT_* variable of the hub's own is stripped there.
	return []string{
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_ASKPASS=", "GIT_ALLOW_PROTOCOL=" + proto,
	}
}

var verRe = regexp.MustCompile(`git version (\d+)\.(\d+)`)

// reftable says whether a new repository should be made with the reftable ref format: git 2.45 or
// later by `git --version`, AND a probe that makes one.
func (s *Store) reftable(ctx context.Context) bool {
	var line string
	var err error
	if s.GitVersion != nil {
		line, err = s.GitVersion(ctx)
	} else {
		line, err = s.h.runner().Git(ctx, "", "--version")
	}
	if err != nil {
		return false
	}
	m := verRe.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	maj, _ := strconv.Atoi(m[1])
	min, _ := strconv.Atoi(m[2])
	if maj < 2 || (maj == 2 && min < 45) {
		return false
	}
	if s.Probe != nil {
		return s.Probe(ctx)
	}
	tmp, err := os.MkdirTemp("", "atrium-reftable-*")
	if err != nil {
		return false
	}
	defer os.RemoveAll(tmp)
	_, err = s.h.runner().GitEnv(ctx, "", s.env(), "init", "-q", "--bare", "--ref-format=reftable",
		filepath.Join(tmp, "p.git"))
	return err == nil
}

func (s *Store) git(ctx context.Context, dir string, args ...string) (string, error) {
	return s.h.runner().GitEnv(ctx, dir, s.env(), args...)
}

// tip is main's sha, or "" for a repository with no main.
func (s *Store) tip(ctx context.Context, dir string) string {
	out, err := s.git(ctx, dir, "rev-parse", "--verify", "-q", MainRef+"^{commit}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// InitResult is what `atrium rooms git init` answers.
type InitResult struct {
	// Repo is `<host>/<owner>/<repo>`.
	Repo string `json:"repo"`
	// Created is whether this call made the repository.
	Created bool `json:"created"`
	// Seeded is whether this call fetched main from the forge.
	Seeded bool `json:"seeded"`
	// Main is main's sha, or "" for an empty repository.
	Main string `json:"main"`
	// Note is what the operator is told.
	Note string `json:"note"`
}

// Init makes the repository for a forge URL, or says it is already there.
//
// A PUBLIC repository gets the forge's default branch fetched ONCE into refs/heads/main, whatever
// the forge calls it. A second init never fetches again once main exists. A private repository, a
// fetch that fails, or an empty forge repository leaves an EMPTY repository (HEAD on main), and the
// note says to push main as the operator. When that was a network failure the note says to run init
// again, which then seeds if main is still absent.
//
// The URL and any credential are never written into the repository: the seed fetches by URL, and
// no remote is added.
func (s *Store) Init(ctx context.Context, rawURL string) (InitResult, error) {
	ref, err := ParseURL(rawURL)
	if err != nil {
		return InitResult{}, err
	}
	res, err := s.initRef(ctx, ref)
	if err != nil {
		// The kind survives the scrub, so the route can still tell a conflict from a failure.
		if r, ok := err.(*refusal); ok {
			return res, &refusal{kind: r.kind, msg: s.scrub(r.msg)}
		}
		return res, fmt.Errorf("%s", s.scrub(err.Error()))
	}
	res.Note = s.scrub(res.Note)
	s.h.audit("", "git-store-init", fmt.Sprintf("%s created=%v seeded=%v main=%s",
		res.Repo, res.Created, res.Seeded, short(res.Main)))
	return res, nil
}

func (s *Store) initRef(ctx context.Context, ref Ref) (InitResult, error) {
	res := InitResult{Repo: ref.Name()}
	// ONE LOCK PER REPOSITORY, by its lowercased name so the case variants share it.
	l := s.h.lock("store:" + strings.ToLower(ref.Name()))
	l.Lock()
	defer l.Unlock()

	if hit := s.collision(ref); hit != "" {
		return res, conflict("%s cannot be made, because %s is already in the store and a disk that ignores case "+
			"would make them one directory", ref.Name(), hit)
	}
	dir := s.dir(ref)
	_, statErr := os.Stat(dir)
	existed := statErr == nil

	switch {
	case existed && isMarked(dir):
		res.Main = s.tip(ctx, dir)
		if res.Main != "" {
			res.Note = "already there, main left alone"
			return res, nil
		}
	case existed:
		// A bare repository the store did not make, such as a git_repos mirror. TAKEN, not changed:
		// only the marker is written, so its HEAD and its refs stay as they are.
		if out, err := s.git(ctx, dir, "rev-parse", "--is-bare-repository"); err != nil || strings.TrimSpace(out) != "true" {
			return res, conflict("something that is not a bare repository is already at %s", ref.Name())
		}
		if err := writeMarker(dir); err != nil {
			return res, err
		}
		res.Main = s.tip(ctx, dir)
		res.Note = "already a bare repository here, taken into the store as it is, and nothing in it was changed"
		if res.Main == "" {
			res.Note += ". it has no main, so " + PushMain
		}
		return res, nil
	default:
		if err := s.create(ctx, dir); err != nil {
			return res, err
		}
		res.Created = true
	}

	// Seed: only a repository with no main gets here.
	sha, why := s.seed(ctx, dir, ref)
	switch {
	case sha != "":
		res.Seeded, res.Main = true, sha
		if res.Created {
			res.Note = "created, and main seeded from the forge's default branch"
		} else {
			res.Note = "already there, and main was empty, so it was seeded from the forge's default branch"
		}
	default:
		verb := "created empty"
		if !res.Created {
			verb = "already there and still empty"
		}
		res.Note = verb + ": " + why
	}
	return res, nil
}

func writeMarker(dir string) error {
	return os.WriteFile(filepath.Join(dir, Marker), []byte("made by atrium rooms git init\n"), 0o644)
}

// create makes the bare repository, with HEAD on main, no hook and no template, and removes what it
// made if any step fails.
func (s *Store) create(ctx context.Context, dir string) (err error) {
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return fmt.Errorf("could not make the store's directory: %w", err)
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(dir)
		}
	}()
	args := []string{"init", "-q", "--bare", "--template=", "--initial-branch=main"}
	if s.reftable(ctx) {
		args = append(args, "--ref-format=reftable")
	}
	if _, err = s.git(ctx, "", append(args, dir)...); err != nil {
		return err
	}
	// Belt and braces: a git that ignored --initial-branch still ends with HEAD on main.
	if cur, _ := s.git(ctx, dir, "symbolic-ref", "-q", "HEAD"); strings.TrimSpace(cur) != MainRef {
		if _, err = s.git(ctx, dir, "symbolic-ref", "HEAD", MainRef); err != nil {
			return err
		}
	}
	return writeMarker(dir)
}

func (s *Store) forgeURL(ref Ref) string {
	if s.Forge != nil {
		return s.Forge(ref)
	}
	return "https://" + forgeHost(ref) + "/" + ref.Owner + "/" + ref.Repo + ".git"
}

// forgeHost undoes canonicalHost.
func forgeHost(ref Ref) string {
	if ref.Host == DefaultHost {
		return "github.com"
	}
	return ref.Host
}

var defaultRe = regexp.MustCompile(`(?m)^ref: (refs/heads/\S+)\s+HEAD$`)

// seed fetches the forge's default branch into main once. It answers the sha, or "" and the
// sentence to tell the operator.
func (s *Store) seed(ctx context.Context, dir string, ref Ref) (string, string) {
	bound := s.SeedTimeout
	if bound <= 0 {
		bound = 2 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, bound)
	defer cancel()
	forge := s.forgeURL(ref)

	out, err := s.git(ctx, dir, "ls-remote", "--symref", "--", forge, "HEAD")
	if err != nil {
		return "", s.whyNot(ref, err)
	}
	m := defaultRe.FindStringSubmatch(out)
	if m == nil {
		return "", "the forge's repository has no default branch yet, so " + PushMain
	}
	def := m[1]
	if _, err := s.git(ctx, "", "check-ref-format", def); err != nil {
		return "", "the forge's default branch has a name atrium will not take, so " + PushMain
	}
	if _, err := s.git(ctx, dir, "fetch", "-q", "--no-tags", "--no-write-fetch-head", "--",
		forge, "+"+def+":"+MainRef); err != nil {
		return "", s.whyNot(ref, err)
	}
	sha := s.tip(ctx, dir)
	if sha == "" {
		return "", "the fetch finished and main is still empty, so " + PushMain
	}
	return sha, ""
}

// whyNot tells the operator what to do after a failed look at the forge. It tells a repository
// that is not there, or is private, from a network error only as far as git's own words do.
func (s *Store) whyNot(ref Ref, err error) string {
	text := strings.ToLower(err.Error())
	if e, ok := err.(*Error); ok {
		text = strings.ToLower(e.Stderr + " " + e.Error())
	}
	for _, w := range []string{
		"repository not found", "not found", "could not read username", "could not read password",
		"authentication failed", "terminal prompts disabled", "does not appear to be a git repository",
		"returned error: 401", "returned error: 403", "returned error: 404", "access denied",
	} {
		if strings.Contains(text, w) {
			return ref.Name() + " does not exist on the forge or is private, so " + PushMain
		}
	}
	line := err.Error()
	if e, ok := err.(*Error); ok && e.First() != "" {
		line = e.First()
	}
	return "the forge could not be reached (" + line + "), so " + PushMain + " Or " + RerunInit
}

// ── what the board lists ────────────────────────────────

// MainView is main in a repository's listing.
type MainView struct {
	SHA string  `json:"sha"`
	At  *string `json:"at"`
}

// BranchView is one pushed branch. Room, Card and Released are filled by the push log in the next
// item, and are "", "" and false until then.
type BranchView struct {
	Name     string `json:"name"`
	SHA      string `json:"sha"`
	Room     string `json:"room"`
	Card     string `json:"card"`
	At       string `json:"at"`
	Released bool   `json:"released"`
}

// RepoView is one repository as GET /_hub/git/repos answers it. NEVER A PATH ON THE HUB'S DISK:
// `path` is the URL path the next item serves it at.
type RepoView struct {
	Host     string       `json:"host"`
	Owner    string       `json:"owner"`
	Repo     string       `json:"repo"`
	URL      string       `json:"url"`
	Path     string       `json:"path"`
	Main     MainView     `json:"main"`
	Branches []BranchView `json:"branches"`
}

// CloneURL is the `insteadOf` form of a repository's URL: the host is left out for github.
func CloneURL(ref Ref) string {
	if ref.Host == DefaultHost {
		return "git@hub.atrium:" + ref.Owner + "/" + ref.Repo + ".git"
	}
	return "git@hub.atrium:" + ref.Host + "/" + ref.Owner + "/" + ref.Repo + ".git"
}

// View lists the store for the board: each repository with main and its branches, newest first.
func (s *Store) View(ctx context.Context) ([]RepoView, error) {
	ents, err := s.List()
	if err != nil {
		return nil, err
	}
	out := make([]RepoView, 0, len(ents))
	for _, e := range ents {
		v := RepoView{
			Host: e.Ref.Host, Owner: e.Ref.Owner, Repo: e.Ref.Repo, URL: CloneURL(e.Ref),
			Path: "/git/hub/" + e.Ref.Name() + ".git", Branches: []BranchView{},
		}
		heads, _ := s.heads(ctx, e.Dir)
		for _, h := range heads {
			switch h.name {
			case "main":
				v.Main.SHA = h.SHA
				if at := s.mainAt(ctx, e.Ref.Name(), Tip{SHA: h.SHA, At: h.at}); at != nil {
					str := at.UTC().Format(time.RFC3339)
					v.Main.At = &str
				}
			case "claude/main":
			default:
				v.Branches = append(v.Branches, BranchView{Name: h.name, SHA: h.SHA, At: h.at.UTC().Format(time.RFC3339)})
			}
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Store) mainAt(ctx context.Context, name string, tip Tip) *time.Time {
	if s.MainAt != nil {
		return s.MainAt(ctx, name, tip)
	}
	return &tip.At
}

type head struct {
	name string
	SHA  string
	at   time.Time
}

// heads is every refs/heads in a repository, newest first.
func (s *Store) heads(ctx context.Context, dir string) ([]head, error) {
	out, err := s.git(ctx, dir, "for-each-ref", "--sort=refname",
		"--format=%(refname)%00%(objectname)%00%(committerdate:unix)", "refs/heads")
	if err != nil {
		return nil, err
	}
	var hs []head
	for _, l := range strings.Split(out, "\n") {
		f := strings.Split(strings.TrimSpace(l), "\x00")
		if len(f) != 3 {
			continue
		}
		sec, _ := strconv.ParseInt(f[2], 10, 64)
		hs = append(hs, head{name: strings.TrimPrefix(f[0], "refs/heads/"), SHA: f[1], at: time.Unix(sec, 0)})
	}
	sort.SliceStable(hs, func(i, j int) bool { return hs[i].at.After(hs[j].at) })
	return hs, nil
}
